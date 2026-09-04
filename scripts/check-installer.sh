#!/usr/bin/env bash
#
# Checks the one-command installer served at https://warmbly.com/install.sh.
#
# It is served verbatim out of site/public, so this runs against the exact
# bytes a `curl -fsSL https://warmbly.com/install.sh | sh` executes:
#
#   * it parses as POSIX sh, in dash and not only in bash
#   * shellcheck has nothing to say about it
#   * --help, --print-env and --dry-run work without a terminal, a docker or a
#     network, because that is how someone reads it before trusting it
#   * every compose file it can generate is one docker compose accepts
#   * the published checksum matches, so the documented
#     "download, verify, read, run" path actually verifies
set -euo pipefail

cd "$(dirname "$0")/.."
SCRIPT=site/public/install.sh
SUMFILE=site/public/install.sh.sha256

fail() { printf '\n\033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }
pass() { printf '\033[32m✓\033[0m %s\n' "$*"; }

[[ -f $SCRIPT ]] || fail "$SCRIPT is missing"

# The script is executed by whatever /bin/sh is on the operator's box, which on
# Debian and Ubuntu is dash. Checking it with bash alone would let a bashism
# through to exactly the hosts this is aimed at.
if command -v dash >/dev/null 2>&1; then
  dash -n "$SCRIPT" || fail "the installer is not valid POSIX sh (dash -n)"
  pass "parses as POSIX sh"
else
  sh -n "$SCRIPT" || fail "the installer does not parse"
  pass "parses (dash not installed; POSIX check was approximate)"
fi

if command -v shellcheck >/dev/null 2>&1; then
  shellcheck -s sh "$SCRIPT" || fail "shellcheck found problems in the installer"
  pass "shellcheck clean"
else
  echo "· shellcheck not installed; skipped"
fi

# --help must work before anything is set up, which is where an unbound
# variable under set -u would otherwise hide.
sh "$SCRIPT" --help >/dev/null || fail "--help failed"
pass "--help works"

# --demo has to reach its end with no terminal, no Docker and no network, and
# above all it must not create the install directory it talks about.
demo_dir=$(mktemp -d)/opt-warmbly
sh "$SCRIPT" --demo --no-color --dir "$demo_dir" >/dev/null 2>&1 || fail "--demo failed"
[[ ! -e $demo_dir ]] || fail "--demo created $demo_dir; it must write nothing"
pass "--demo runs and writes nothing"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

sh "$SCRIPT" --print-env --no-color --dir "$work/inst" --version v0.0.0-test >"$work/env" ||
  fail "--print-env failed"
for key in WARMBLY_TAG AUTH_SECRET CREDENTIALS_ENCRYPTION_KEY KMS_LOCAL_MASTER_KEY \
           INTERNAL_API_TOKEN SECRET_KEY_BASE PRIMARY_DB WARMBLY_SETTINGS_BOOTSTRAP; do
  grep -q "^${key}=." "$work/env" || fail "--print-env wrote no $key"
done
grep -q '^WARMBLY_TAG=v0.0.0-test$' "$work/env" || fail "--version was not pinned into .env"
pass "--print-env writes a complete .env"

# Every shape of answer has to produce a compose file compose accepts. These
# are the four that change the file's structure rather than its values.
check_shape() {
  local label=$1; shift
  local dir="$work/shape"
  rm -rf "$dir"; mkdir -p "$dir"
  sh "$SCRIPT" --dry-run --no-color --dir "$dir/inst" --version v0.0.0-test "$@" >"$dir/out" ||
    fail "--dry-run failed for: $label"
  python3 - "$dir" <<'PY'
import sys, os, re
d = sys.argv[1]
lines = open(os.path.join(d, "out")).read().split("\n")
def extract(marker, out):
    idx = [i for i, l in enumerate(lines) if l.strip().startswith("── ") and marker in l]
    if not idx:
        return
    body = []
    for l in lines[idx[0] + 1:]:
        if l.strip().startswith("── ") and "(mode" in l:
            break
        body.append(l[2:] if l.startswith("  ") else l)
    while body and (body[-1].strip() == "" or body[-1][:1] in "╭│╰"):
        body.pop()
    open(os.path.join(d, out), "w").write("\n".join(body) + "\n")
extract("/.env", ".env")
extract("docker-compose.yml", "docker-compose.yml")
PY
  [[ -f "$dir/docker-compose.yml" ]] || fail "--dry-run printed no compose file for: $label"
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    ( cd "$dir" && docker compose config >/dev/null ) || fail "invalid compose file for: $label"
  fi
  pass "generates a valid stack: $label"
}

check_shape "defaults"
check_shape "bundled TLS" --tls caddy --host warmbly.example.com
check_shape "core only" --components core
check_shape "named volumes" --data-root volumes
check_shape "external stores" --database-url postgres://u:p@db:5432/w --redis-url redis://cache:6379 --blobs s3

# Nothing drawn inside a redraw loop may be wider than the terminal. A wrapped
# line is two physical rows, every cursor-up counts logical ones, and the menu
# then draws over itself and over whatever was on screen before it. This is the
# regression that check exists for.
if command -v python3 >/dev/null 2>&1; then
  python3 - "$SCRIPT" <<'PYEOF' || fail "the installer drew past the terminal width"
import fcntl, os, pty, re, select, struct, sys, termios, time

script = sys.argv[1]
failures = []
for cols in (80, 100):
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 45, cols, 0, 0))
    pid = os.fork()
    if pid == 0:
        os.setsid()
        fcntl.ioctl(slave, termios.TIOCSCTTY, 0)
        os.dup2(slave, 0); os.dup2(slave, 1); os.dup2(slave, 2)
        os.close(master); os.close(slave)
        os.environ["TERM"] = "xterm-256color"
        os.environ["WARMBLY_DEMO_FAST"] = "1"
        os.execvp("sh", ["sh", script, "--demo"])
        os._exit(1)
    os.close(slave)
    buf = b""
    start = last = time.time()
    sent = 0
    while time.time() - start < 90:
        r, _, _ = select.select([master], [], [], 0.25)
        if r:
            try:
                chunk = os.read(master, 65536)
            except OSError:
                break
            if not chunk:
                break
            buf += chunk
            last = time.time()
        elif time.time() - last > 0.4 and sent < 80:
            tail = re.sub(r"\x1b\[[0-9;?]*[a-zA-Z]", "", buf.decode("utf-8", "replace"))[-400:]
            os.write(master, b"copied\r" if "Type 'copied'" in tail else b"\r")
            sent += 1
            last = time.time()
        if b"That was the demo" in buf:
            break
    for fn in (lambda: os.close(master), lambda: os.waitpid(pid, 0)):
        try:
            fn()
        except OSError:
            pass
    text = buf.decode("utf-8", "replace")
    if "command not found" in text or "syntax error" in text:
        failures.append(f"{cols} columns: the run produced shell errors")
    if "That was the demo" not in text:
        failures.append(f"{cols} columns: the demo did not reach its end")
    plain = re.sub(r"\x1b\[[0-9;?]*[a-zA-Z]", "", text)
    over = [l for l in plain.replace("\r", "\n").split("\n") if len(l) > cols]
    if over:
        failures.append(f"{cols} columns: {len(over)} line(s) too wide, first: {over[0][:cols + 20]!r}")

for f in failures:
    print("   " + f, file=sys.stderr)
sys.exit(1 if failures else 0)
PYEOF
  pass "draws inside the terminal at 80 and 100 columns"
else
  echo "· python3 not installed; the width check was skipped"
fi

# The checksum is the whole answer to "why would I pipe this into a shell", so
# a stale one is a failure, not a warning.
if [[ ! -f $SUMFILE ]]; then
  fail "$SUMFILE is missing. Regenerate it with: make installer-sha"
fi
expected=$(awk '{print $1}' "$SUMFILE")
actual=$(sha256sum "$SCRIPT" | awk '{print $1}')
if [[ $expected != "$actual" ]]; then
  fail "$SUMFILE is stale.
    published $expected
    actual    $actual
  Regenerate it with: make installer-sha"
fi
pass "published checksum matches"

printf '\n\033[32mThe installer is good.\033[0m\n'
