package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Image mode is the updater for an install that never had a checkout: the one
// `curl warmbly.com/install.sh | sh` creates, which holds a generated compose
// file and a .env and runs the published release images. There is nothing to
// pull with git and nothing to build, so an update is: pin the tag in .env,
// `docker compose pull`, recreate what changed.

// tagVar is the .env key the generated compose file reads every image tag
// from, so pinning a release is one line the operator can also edit by hand.
const tagVar = "WARMBLY_TAG"

// envFileName is the environment file next to the compose file. Compose reads
// it automatically, so writing the tag there is all it takes to move.
const envFileName = ".env"

// composeFileNames are the compose files an image install may carry, in the
// order docker compose itself resolves them.
var composeFileNames = []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}

// composeFile finds the install's compose file, or reports that there is none.
func composeFile(dir string) (string, bool) {
	for _, name := range composeFileNames {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// readTag returns the tag the install is pinned to, or "prod" when .env does
// not name one (the compose default).
func (r *Runner) readTag() string {
	if v := readEnvVar(filepath.Join(r.cfg.RepoDir, envFileName), tagVar); v != "" {
		return v
	}
	return "prod"
}

// releaseState is the image-mode answer to "what is this install running".
func (r *Runner) releaseState() *Release {
	tag := r.readTag()
	return &Release{
		Tag:    tag,
		Prefix: r.imagePrefix(),
		// A moving tag republishes under the same name, so "pull again" is a
		// real update for it and a no-op for a pinned version.
		Pinned: tag != "prod" && tag != "dev" && tag != "latest",
	}
}

func (r *Runner) imagePrefix() string {
	if v := readEnvVar(filepath.Join(r.cfg.RepoDir, envFileName), "WARMBLY_IMAGE_PREFIX"); v != "" {
		return v
	}
	return "ghcr.io/warmbly/warmbly"
}

// imageUpdate is the image-mode counterpart of composeUpdate: no git, no
// build. The tag is written to .env FIRST so the pull, the recreate and every
// later `docker compose up` by hand all resolve the same images.
func (r *Runner) imageUpdate(ctx context.Context, job *Job, tag string) error {
	r.step(job, "resolve")
	if tag == "" {
		tag = r.readTag()
		r.logf(job, "no release named; re-pulling the pinned tag %s", tag)
	}
	from := r.readTag()
	envPath := filepath.Join(r.cfg.RepoDir, envFileName)
	if err := setEnvVar(envPath, tagVar, tag); err != nil {
		return fmt.Errorf("could not pin %s=%s in %s: %w", tagVar, tag, envPath, err)
	}
	if from == tag {
		r.logf(job, "already pinned to %s; pulling it again", tag)
	} else {
		r.logf(job, "moved %s -> %s in %s", from, tag, envFileName)
	}

	r.step(job, "pull")
	r.logf(job, "pulling %s/*:%s", r.imagePrefix(), tag)
	if err := r.exec(ctx, job, r.cfg.RepoDir, nil, "docker", r.composeArgs("pull")...); err != nil {
		// A pull that fails leaves .env pointing at a tag with no images, and
		// the next `docker compose up` by hand would then fail the same way.
		if rerr := setEnvVar(envPath, tagVar, from); rerr == nil {
			r.logf(job, "pull failed; %s put back to %s", tagVar, from)
		}
		return err
	}

	r.step(job, "restart")
	services, err := r.servicesToRecreate(ctx)
	if err != nil {
		return err
	}
	r.logf(job, "recreating %s", strings.Join(services, ", "))
	args := append(r.composeArgs("up", "-d", "--no-build", "--remove-orphans"), services...)
	if err := r.exec(ctx, job, r.cfg.RepoDir, nil, "docker", args...); err != nil {
		return err
	}

	if r.cfg.Prune {
		r.step(job, "prune")
		if err := r.exec(ctx, job, r.cfg.RepoDir, nil, "docker", "image", "prune", "-f"); err != nil {
			r.logf(job, "image prune failed (ignored): %v", err)
		}
	}
	return nil
}

// env file editing
//
// The file is read and rewritten whole rather than appended to, so a second
// update does not leave two WARMBLY_TAG lines with the loser still readable.

// readEnvVar returns the last assignment of key in a KEY=VALUE file, or "".
// Last wins because that is what docker compose itself does.
func readEnvVar(path, key string) string {
	b, err := os.ReadFile(path) //nolint:gosec // operator-owned install directory
	if err != nil {
		return ""
	}
	out := ""
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := splitEnvLine(line)
		if ok && k == key {
			out = v
		}
	}
	return out
}

// setEnvVar rewrites key in place, keeping every other line and the file's
// position, and appends it when it is absent. The write is atomic and 0600:
// this file holds every secret the instance has.
func setEnvVar(path, key, value string) error {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-owned install directory
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := []string{}
	if len(raw) > 0 {
		lines = strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	}
	replaced := false
	for i, line := range lines {
		k, _, ok := splitEnvLine(line)
		if !ok || k != key {
			continue
		}
		if replaced {
			// A duplicate earlier in the file would win nothing but confuse
			// the next person to read it.
			lines[i] = "# " + line + "   # superseded by the updater"
			continue
		}
		lines[i] = key + "=" + value
		replaced = true
	}
	if !replaced {
		lines = append(lines, key+"="+value)
	}
	body := strings.Join(lines, "\n") + "\n"

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// splitEnvLine parses one KEY=VALUE line, skipping comments and blanks and
// unwrapping the quotes compose accepts.
func splitEnvLine(line string) (string, string, bool) {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") {
		return "", "", false
	}
	s = strings.TrimPrefix(s, "export ")
	key, value, ok := strings.Cut(s, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}
	return key, value, true
}
