// The updater is the host-side agent behind "Update and restart" in the admin
// panel. It moves the install forward and restarts the stack, then reports
// progress to the backend. UPDATER_MODE picks how: compose pulls a git
// checkout and rebuilds it, image pins a release tag in the install's .env and
// pulls the published images (the clone-free install.sh install), command
// hands the rebuild to a script of your own.
// See docs/content/docs/development/updates.mdx.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/warmbly/warmbly/internal/updater"
	"github.com/warmbly/warmbly/internal/version"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	repoDir := getenv("UPDATER_REPO_DIR", "")
	if repoDir == "" {
		if wd, err := os.Getwd(); err == nil {
			repoDir = wd
		}
	}
	cfg := updater.Config{
		Mode:             updater.Mode(getenv("UPDATER_MODE", string(updater.ModeCompose))),
		RepoDir:          repoDir,
		Remote:           getenv("UPDATER_REMOTE", "origin"),
		Command:          os.Getenv("UPDATER_COMMAND"),
		ComposeProject:   getenv("UPDATER_COMPOSE_PROJECT", "warmbly"),
		ComposeProfiles:  getenv("UPDATER_COMPOSE_PROFILES", "updater"),
		BackendHealthURL: getenv("UPDATER_BACKEND_HEALTH_URL", "http://backend:8080/health"),
		StateDir:         getenv("UPDATER_STATE_DIR", "/var/lib/warmbly-updater"),
		FetchInterval:    duration("UPDATER_FETCH_INTERVAL", 30*time.Minute),
		Prune:            boolean("UPDATER_PRUNE", true),
		AllowDirty:       boolean("UPDATER_ALLOW_DIRTY", false),
		Version:          version.String(),
	}
	switch cfg.Mode {
	case updater.ModeCompose, updater.ModeCommand, updater.ModeImage:
	default:
		log.Fatalf("updater: UPDATER_MODE must be compose, image or command, got %q", cfg.Mode)
	}

	runner, err := updater.NewRunner(cfg)
	if err != nil {
		log.Fatalf("updater: %v", err)
	}
	// The updater shares the backend's internal token unless given its own.
	token := getenv("UPDATER_TOKEN", os.Getenv("INTERNAL_API_TOKEN"))
	server, err := updater.NewServer(runner, token)
	if err != nil {
		log.Fatalf("updater: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runner.Start(ctx)

	addr := getenv("UPDATER_ADDR", ":8095")
	srv := &http.Server{Addr: addr, Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		runner.Stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("updater: %s mode, install %s, listening on %s (version %s)", cfg.Mode, cfg.RepoDir, addr, cfg.Version)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("updater: %v", err)
	}
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func duration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < time.Minute {
		return def
	}
	return d
}

func boolean(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
