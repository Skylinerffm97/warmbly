package updater

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Config is everything the runner reads from the environment.
type Config struct {
	Mode    Mode
	RepoDir string
	Remote  string
	// Command runs in ModeCommand after the checkout moved. It is passed to
	// sh -c and has to build and restart everything itself.
	Command string
	// ComposeProject is the -p the stack was started with (the Makefile pins
	// "warmbly"), ComposeProfiles the extra profiles to activate on top of the
	// checkout's .env so the updater's own image is rebuilt too.
	ComposeProject  string
	ComposeProfiles string
	// BackendHealthURL is polled after the restart until it answers 200.
	BackendHealthURL string
	StateDir         string
	FetchInterval    time.Duration
	Prune            bool
	AllowDirty       bool
	Version          string
}

// maxLogLines caps a job's retained log so the status answer stays small.
const maxLogLines = 1500

// healthWait bounds how long the restart step waits for the backend.
const healthWait = 6 * time.Minute

// Runner owns the checkout, the one job at a time, and the persisted history.
type Runner struct {
	cfg Config
	git git

	mu       sync.Mutex
	checkout *Checkout
	job      *Job
	lastJob  *Job
	cancel   context.CancelFunc
}

// ErrJobRunning is returned when an update is asked for while one runs.
var ErrJobRunning = errors.New("an update is already running")

func NewRunner(cfg Config) (*Runner, error) {
	if cfg.RepoDir == "" {
		return nil, errors.New("UPDATER_REPO_DIR is required")
	}
	if cfg.Mode == ModeImage {
		// The clone-free install has no checkout to inspect; what it must have
		// is the compose file the pull and the recreate address.
		if _, ok := composeFile(cfg.RepoDir); !ok {
			return nil, fmt.Errorf("%s holds no compose file; UPDATER_MODE=image needs the install directory", cfg.RepoDir)
		}
	} else if _, err := os.Stat(filepath.Join(cfg.RepoDir, ".git")); err != nil {
		return nil, fmt.Errorf("%s is not a git checkout: %w", cfg.RepoDir, err)
	}
	if cfg.Mode == ModeCommand && strings.TrimSpace(cfg.Command) == "" {
		return nil, errors.New("UPDATER_MODE=command needs UPDATER_COMMAND")
	}
	if cfg.Remote == "" {
		cfg.Remote = "origin"
	}
	if cfg.ComposeProject == "" {
		cfg.ComposeProject = "warmbly"
	}
	if cfg.FetchInterval <= 0 {
		cfg.FetchInterval = 30 * time.Minute
	}
	r := &Runner{cfg: cfg, git: git{dir: cfg.RepoDir, remote: cfg.Remote}}
	r.loadState()
	return r, nil
}

// Start refreshes the checkout state now and then on the fetch interval.
func (r *Runner) Start(ctx context.Context) {
	go func() {
		r.Refresh(ctx)
		t := time.NewTicker(r.cfg.FetchInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				r.Refresh(ctx)
			}
		}
	}()
}

// Refresh fetches the remote and re-reads the checkout. In image mode there is
// nothing to fetch: what a release exists is the backend's GitHub check, and
// what is installed is one line of .env, read live in Status.
func (r *Runner) Refresh(ctx context.Context) *Checkout {
	if r.cfg.Mode == ModeImage {
		return nil
	}
	fetchErr := r.git.fetch(ctx)
	c, err := r.git.inspect(ctx)
	if err != nil {
		log.Printf("updater: inspect checkout: %v", err)
		c = &Checkout{}
	}
	c.FetchedAt = time.Now()
	if fetchErr != nil {
		c.FetchError = fetchErr.Error()
	}
	r.mu.Lock()
	r.checkout = c
	r.mu.Unlock()
	return c
}

// Status is the current snapshot. The checkout is re-read without a fetch so
// a job that just moved HEAD is reflected immediately.
func (r *Runner) Status(ctx context.Context) Status {
	r.mu.Lock()
	prev := r.checkout
	job := cloneJob(r.job)
	last := cloneJob(r.lastJob)
	r.mu.Unlock()

	if r.cfg.Mode == ModeImage {
		return Status{
			Mode:    r.cfg.Mode,
			RepoDir: r.cfg.RepoDir,
			Version: r.cfg.Version,
			Release: r.releaseState(),
			Job:     job,
			LastJob: last,
		}
	}

	c, err := r.git.inspect(ctx)
	if err == nil && prev != nil {
		c.FetchedAt = prev.FetchedAt
		c.FetchError = prev.FetchError
	} else if err != nil {
		c = prev
	}
	return Status{
		Mode:     r.cfg.Mode,
		RepoDir:  r.cfg.RepoDir,
		Version:  r.cfg.Version,
		Checkout: c,
		Job:      job,
		LastJob:  last,
	}
}

// StartUpdate begins a job and returns immediately. Progress is read from
// Status; the log is appended as the steps run.
func (r *Runner) StartUpdate(req UpdateRequest) (*Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.job != nil && r.job.Status == JobRunning {
		return nil, ErrJobRunning
	}
	target := strings.TrimSpace(req.Tag)
	if target == "" {
		// What "no tag" means differs per mode, and the job label is what the
		// admin panel shows while it runs.
		target = "branch"
		if r.cfg.Mode == ModeImage {
			target = r.readTag()
		}
	}
	job := &Job{
		ID:        uuid.NewString(),
		Status:    JobRunning,
		Target:    target,
		Step:      "starting",
		StartedAt: time.Now(),
	}
	r.job = job
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go r.execute(ctx, job, req)
	return cloneJob(job), nil
}

// Stop aborts a running job, used on process shutdown.
func (r *Runner) Stop() {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *Runner) execute(ctx context.Context, job *Job, req UpdateRequest) {
	err := r.runSteps(ctx, job, req)
	r.mu.Lock()
	now := time.Now()
	job.FinishedAt = &now
	if err != nil {
		job.Status = JobFailed
		job.Error = err.Error()
		r.appendLocked(job, fmt.Sprintf("update failed: %v", err))
	} else {
		job.Status = JobSucceeded
		r.appendLocked(job, "update finished")
	}
	r.lastJob = job
	r.job = nil
	r.cancel = nil
	r.mu.Unlock()
	r.saveState()

	if err == nil && (r.cfg.Mode == ModeCompose || r.cfg.Mode == ModeImage) {
		// Last, because it may replace this very process: the outcome above is
		// already on disk for the successor to report.
		r.recreateSelf(ctx, job)
	}
}

func (r *Runner) runSteps(ctx context.Context, job *Job, req UpdateRequest) error {
	if r.cfg.Mode == ModeImage {
		if err := r.imageUpdate(ctx, job, strings.TrimSpace(req.Tag)); err != nil {
			return err
		}
		return r.waitForBackend(ctx, job)
	}

	from, err := r.git.head(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	job.FromCommit = from
	r.mu.Unlock()

	r.step(job, "fetch")
	r.logf(job, "fetching %s", r.cfg.Remote)
	if err := r.git.fetch(ctx); err != nil {
		return err
	}

	r.step(job, "checkout")
	if dirty, err := r.git.dirty(ctx); err == nil && dirty && !r.cfg.AllowDirty {
		return errors.New("the checkout has local modifications; commit or stash them, or set UPDATER_ALLOW_DIRTY=true")
	}
	tag := strings.TrimSpace(req.Tag)
	switch {
	case tag != "":
		if !r.git.tagExists(ctx, tag) {
			return fmt.Errorf("tag %s does not exist on %s", tag, r.cfg.Remote)
		}
		r.logf(job, "checking out %s", tag)
		if err := r.git.checkoutTag(ctx, tag); err != nil {
			return err
		}
	default:
		branch, err := r.git.branch(ctx)
		if err != nil {
			return err
		}
		if branch == "" {
			return errors.New("the checkout is detached (pinned to a tag); choose a release to move to")
		}
		r.logf(job, "fast-forwarding %s to %s/%s", branch, r.cfg.Remote, branch)
		if err := r.git.pull(ctx, branch); err != nil {
			return err
		}
	}
	to, err := r.git.head(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	job.ToCommit = to
	r.mu.Unlock()
	if to == from {
		r.logf(job, "already at %s; rebuilding anyway", short(to))
	} else {
		r.logf(job, "moved %s -> %s", short(from), short(to))
	}
	r.restoreOwnership(ctx, job)

	switch r.cfg.Mode {
	case ModeCommand:
		r.step(job, "command")
		r.logf(job, "running UPDATER_COMMAND")
		if err := r.exec(ctx, job, r.cfg.RepoDir, nil, "sh", "-c", r.cfg.Command); err != nil {
			return err
		}
	default:
		if err := r.composeUpdate(ctx, job); err != nil {
			return err
		}
	}

	return r.waitForBackend(ctx, job)
}

// waitForBackend is the last step of every mode: the update is only finished
// once the API answers again.
func (r *Runner) waitForBackend(ctx context.Context, job *Job) error {
	if r.cfg.BackendHealthURL == "" {
		return nil
	}
	r.step(job, "wait")
	r.logf(job, "waiting for the backend at %s", r.cfg.BackendHealthURL)
	if err := waitHealthy(ctx, r.cfg.BackendHealthURL, healthWait); err != nil {
		return err
	}
	r.logf(job, "backend is answering")
	return nil
}

// restoreOwnership gives files git wrote as root back to the checkout's owner,
// so the operator's own git pull keeps working after the sidecar moved HEAD.
func (r *Runner) restoreOwnership(ctx context.Context, job *Job) {
	if os.Geteuid() != 0 {
		return
	}
	uid, gid, ok := ownerOf(r.cfg.RepoDir)
	if !ok || uid == 0 {
		return
	}
	spec := fmt.Sprintf("%d:%d", uid, gid)
	if err := r.exec(ctx, job, r.cfg.RepoDir, nil, "chown", "-R", spec, filepath.Join(r.cfg.RepoDir, ".git")); err != nil {
		r.logf(job, "could not restore ownership of .git: %v", err)
	}
	// Only files git touched need it; a full recursive chown over node_modules
	// would take longer than the build.
	out, err := r.git.run(ctx, "diff", "--name-only", job.FromCommit, "HEAD")
	if err != nil || out == "" {
		return
	}
	args := []string{spec}
	for _, f := range strings.Split(out, "\n") {
		if f = strings.TrimSpace(f); f != "" {
			args = append(args, filepath.Join(r.cfg.RepoDir, f))
		}
	}
	_ = exec.CommandContext(ctx, "chown", args...).Run()
}

// exec runs a command with its output streamed into the job log.
func (r *Runner) exec(ctx context.Context, job *Job, dir string, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r")
			if strings.TrimSpace(line) == "" {
				continue
			}
			r.logf(job, "%s", line)
		}
	}()
	err := cmd.Run()
	_ = pw.Close()
	<-done
	if err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func (r *Runner) step(job *Job, name string) {
	r.mu.Lock()
	job.Step = name
	r.mu.Unlock()
	r.saveState()
}

func (r *Runner) logf(job *Job, format string, args ...any) {
	r.mu.Lock()
	r.appendLocked(job, fmt.Sprintf(format, args...))
	r.mu.Unlock()
}

// appendLocked records one log line. The caller holds r.mu; the mutex is not
// reentrant, so the completion path in execute must use this, never logf.
func (r *Runner) appendLocked(job *Job, msg string) {
	line := time.Now().UTC().Format("15:04:05") + " " + msg
	job.Log = append(job.Log, line)
	if len(job.Log) > maxLogLines {
		job.Log = job.Log[len(job.Log)-maxLogLines:]
	}
	log.Printf("updater: %s", line)
}

// persisted state

type stateFile struct {
	LastJob *Job `json:"last_job,omitempty"`
}

func (r *Runner) statePath() string {
	if r.cfg.StateDir == "" {
		return ""
	}
	return filepath.Join(r.cfg.StateDir, "state.json")
}

func (r *Runner) loadState() {
	p := r.statePath()
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var st stateFile
	if err := json.Unmarshal(b, &st); err != nil {
		return
	}
	if st.LastJob != nil && st.LastJob.Status == JobRunning {
		// The process died mid-job (most likely it recreated itself after a
		// successful compose run, or the host rebooted). Say so rather than
		// showing a job that runs forever.
		now := time.Now()
		st.LastJob.Status = JobFailed
		st.LastJob.FinishedAt = &now
		st.LastJob.Error = "the updater restarted before the job finished"
	}
	r.lastJob = st.LastJob
}

func (r *Runner) saveState() {
	p := r.statePath()
	if p == "" {
		return
	}
	r.mu.Lock()
	st := stateFile{LastJob: cloneJob(r.lastJob)}
	if r.job != nil {
		st.LastJob = cloneJob(r.job)
	}
	r.mu.Unlock()
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return
	}
	_ = os.Rename(tmp, p)
}

// helpers

func cloneJob(j *Job) *Job {
	if j == nil {
		return nil
	}
	c := *j
	c.Log = append([]string(nil), j.Log...)
	return &c
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func waitHealthy(ctx context.Context, url string, max time.Duration) error {
	deadline := time.Now().Add(max)
	client := &http.Client{Timeout: 3 * time.Second}
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the backend did not answer at %s within %s; check its logs", url, max)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}
