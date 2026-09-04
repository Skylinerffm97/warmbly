package updater

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// selfService is the compose service the updater itself runs as.
const selfService = "updater"

// composeArgs prefixes every compose invocation with the project and the
// profiles, so the sidecar addresses the same stack the operator started.
func (r *Runner) composeArgs(args ...string) []string {
	out := []string{"compose", "-p", r.cfg.ComposeProject}
	for _, p := range strings.Split(r.cfg.ComposeProfiles, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, "--profile", p)
		}
	}
	return append(out, args...)
}

func (r *Runner) composeOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", r.composeArgs(args...)...)
	cmd.Dir = r.cfg.RepoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker compose %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// composeUpdate rebuilds every image and recreates the containers whose image
// or configuration changed. Postgres, Redis and NATS keep running: compose
// leaves a container alone when nothing about it changed.
func (r *Runner) composeUpdate(ctx context.Context, job *Job) error {
	r.step(job, "build")
	describe := r.git.describe(ctx)
	env := []string{
		"WARMBLY_BUILD_VERSION=" + describe,
		"WARMBLY_BUILD_COMMIT=" + job.ToCommit,
		"DOCKER_CLI_HINTS=false",
	}
	r.logf(job, "building images (%s)", describe)
	if err := r.exec(ctx, job, r.cfg.RepoDir, env, "docker", r.composeArgs("build")...); err != nil {
		return err
	}

	r.step(job, "restart")
	services, err := r.servicesToRecreate(ctx)
	if err != nil {
		return err
	}
	r.logf(job, "recreating %s", strings.Join(services, ", "))
	args := append(r.composeArgs("up", "-d", "--no-build"), services...)
	if err := r.exec(ctx, job, r.cfg.RepoDir, env, "docker", args...); err != nil {
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

// servicesToRecreate is every service the checkout's profiles define plus
// everything currently running, minus the updater itself. The union covers a
// service the new version added and one the operator started by hand.
func (r *Runner) servicesToRecreate(ctx context.Context) ([]string, error) {
	defined, err := r.composeOutput(ctx, "config", "--services")
	if err != nil {
		return nil, err
	}
	running, _ := r.composeOutput(ctx, "ps", "--services", "--status", "running")
	set := map[string]bool{}
	for _, chunk := range []string{defined, running} {
		for _, s := range strings.Split(chunk, "\n") {
			if s = strings.TrimSpace(s); s != "" && s != selfService {
				set[s] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no compose services found in %s", r.cfg.RepoDir)
	}
	return out, nil
}

// selfImageID is the image id the compose file resolves for the updater
// service, whether that image was just built or just pulled. It is asked of
// compose rather than assembled by hand, because the service carries an
// image: key now and `project-updater` is no longer its tag.
func (r *Runner) selfImageID(ctx context.Context) string {
	ref, err := r.composeOutput(ctx, "config", "--images", selfService)
	if err != nil || ref == "" {
		// Older compose, or a file with neither key: fall back to the name a
		// build without an image: key produces.
		ref = r.cfg.ComposeProject + "-" + selfService
	}
	// --images answers one line per service; only one service was asked for.
	if i := strings.IndexByte(ref, '\n'); i >= 0 {
		ref = ref[:i]
	}
	out, err := exec.CommandContext(ctx, "docker", "image", "inspect", "-f", "{{.Id}}", strings.TrimSpace(ref)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// recreateSelf moves the updater onto the image it just built or pulled. It
// cannot run `compose up updater` in-process (that stops this container half
// way), so a detached one-off container from the new image does it a few
// seconds later.
func (r *Runner) recreateSelf(ctx context.Context, job *Job) {
	running, err := r.composeOutput(ctx, "ps", "-q", selfService)
	if err != nil || running == "" {
		return
	}
	currentImage, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.Image}}", running).Output()
	if err != nil {
		return
	}
	nextImage := r.selfImageID(ctx)
	if nextImage == "" || strings.TrimSpace(string(currentImage)) == nextImage {
		return
	}
	r.logf(job, "updater image changed; recreating the updater itself")
	inner := fmt.Sprintf("sleep 3; docker %s", strings.Join(r.composeArgs("up", "-d", "--no-build", "--no-deps", selfService), " "))
	args := r.composeArgs("run", "-d", "--rm", "--no-deps", "--entrypoint", "sh", selfService, "-c", inner)
	cmd := exec.Command("docker", args...)
	cmd.Dir = r.cfg.RepoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		r.logf(job, "could not schedule the updater's own recreate (ignored): %s", strings.TrimSpace(string(out)))
	}
	r.saveState()
}
