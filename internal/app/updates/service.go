// Package updates answers "is there a newer Warmbly than the one running, and
// apply it". The check side polls GitHub Releases and the host-side updater on
// an interval, so the admin panel's top bar can show an update the moment one
// exists. The apply side hands the job to the updater (internal/updater) and
// relays its progress; the backend itself never touches git or docker.
package updates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/warmbly/warmbly/internal/app/releases"
	"github.com/warmbly/warmbly/internal/updater"
	"github.com/warmbly/warmbly/internal/version"
)

// Config is read from the environment by cmd/backend.
type Config struct {
	// Enabled turns the periodic GitHub check on. The updater status is read
	// regardless, because a running job must be visible even with checks off.
	Enabled  bool
	Interval time.Duration
	// Channel is stable (releases) or dev (prereleases included).
	Channel     string
	GithubRepo  string
	GithubToken string
	// UpdaterURL is the host-side updater; empty means updates are applied by
	// hand and the panel only reports.
	UpdaterURL   string
	UpdaterToken string
	HTTPClient   *http.Client
}

// Latest is the newest release on the configured channel.
type Latest struct {
	Tag         string    `json:"tag"`
	Name        string    `json:"name,omitempty"`
	HTMLURL     string    `json:"html_url,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	Channel     string    `json:"channel"`
}

// UpdaterView is what the panel knows about the host-side agent.
type UpdaterView struct {
	// Configured is whether UPDATER_URL is set at all.
	Configured bool `json:"configured"`
	// Status is off (not configured), ok, or unreachable.
	Status  string       `json:"status"`
	Error   string       `json:"error,omitempty"`
	Mode    updater.Mode `json:"mode,omitempty"`
	RepoDir string       `json:"repo_dir,omitempty"`
	// Exactly one of these is set: a checkout in compose and command mode, a
	// release in image mode (the clone-free install).
	Checkout *updater.Checkout `json:"checkout,omitempty"`
	Release  *updater.Release  `json:"release,omitempty"`
	Job      *updater.Job      `json:"job,omitempty"`
	LastJob  *updater.Job      `json:"last_job,omitempty"`
}

// State is the whole answer to GET /admin/instance/update.
type State struct {
	Running         version.Info `json:"running"`
	Latest          *Latest      `json:"latest,omitempty"`
	UpdateAvailable bool         `json:"update_available"`
	// Reason is release (a newer tag exists) or commits (the checkout is
	// behind its branch); empty when nothing is pending.
	Reason     string      `json:"reason,omitempty"`
	CheckedAt  time.Time   `json:"checked_at,omitempty"`
	CheckError string      `json:"check_error,omitempty"`
	Enabled    bool        `json:"enabled"`
	Interval   string      `json:"interval"`
	Channel    string      `json:"channel"`
	Repo       string      `json:"repo"`
	Updater    UpdaterView `json:"updater"`
}

var (
	ErrUpdaterNotConfigured = errors.New("no updater is configured on this instance")
	ErrNothingToApply       = errors.New("this install is pinned to a version and no release is known to move to; run a check first")
)

type Service struct {
	cfg  Config
	http *http.Client

	mu        sync.Mutex
	latest    *Latest
	checkedAt time.Time
	checkErr  string

	// The updater view is cached briefly so the member-facing version pill,
	// the health checks and the admin poll share one read instead of each
	// dialling the updater, and a stalled updater cannot slow every caller.
	viewMu    sync.Mutex
	view      UpdaterView
	viewUntil time.Time
}

// viewTTL is how long a good updater read is served from cache; viewFailTTL
// how long a failed one is, so an absent updater is dialled rarely.
const (
	viewTTL     = 2 * time.Second
	viewFailTTL = 30 * time.Second
)

func New(cfg Config) *Service {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.Interval < 5*time.Minute {
		cfg.Interval = 30 * time.Minute
	}
	if cfg.Channel != "dev" {
		cfg.Channel = "stable"
	}
	cfg.UpdaterURL = strings.TrimRight(strings.TrimSpace(cfg.UpdaterURL), "/")
	// Compose substitutes its default for an empty value, so "none" is how a
	// .env turns the updater off.
	switch strings.ToLower(cfg.UpdaterURL) {
	case "none", "off", "false":
		cfg.UpdaterURL = ""
	}
	return &Service{cfg: cfg, http: cfg.HTTPClient}
}

// Start runs the release check now and on every interval until ctx ends.
func (s *Service) Start(ctx context.Context) {
	if !s.cfg.Enabled {
		log.Printf("updates: release check disabled (UPDATE_CHECK_ENABLED=false)")
		return
	}
	go func() {
		s.checkGitHub(ctx)
		t := time.NewTicker(s.cfg.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.checkGitHub(ctx)
			}
		}
	}()
}

// Check refreshes both sources now and returns the result.
func (s *Service) Check(ctx context.Context) State {
	s.checkGitHub(ctx)
	view := s.updaterStatus(ctx, http.MethodPost, "/check")
	s.storeView(view)
	return s.compose(view, false)
}

// State returns the cached release check plus the updater's state, read live
// at most every viewTTL. withLog keeps job logs; the top-bar poll drops them.
func (s *Service) State(ctx context.Context, withLog bool) State {
	return s.compose(s.cachedView(ctx), !withLog)
}

func (s *Service) cachedView(ctx context.Context) UpdaterView {
	s.viewMu.Lock()
	if time.Now().Before(s.viewUntil) {
		v := s.view
		s.viewMu.Unlock()
		return v
	}
	s.viewMu.Unlock()
	view := s.updaterStatus(ctx, http.MethodGet, "/status")
	s.storeView(view)
	return view
}

func (s *Service) storeView(view UpdaterView) {
	ttl := viewTTL
	if view.Status == "unreachable" || (view.Status == "off" && view.Configured) {
		ttl = viewFailTTL
	}
	s.viewMu.Lock()
	s.view = view
	s.viewUntil = time.Now().Add(ttl)
	s.viewMu.Unlock()
}

// Apply asks the updater to move to target: "latest" picks the tracked branch
// or, on a pinned checkout, the newest release; anything else is a tag.
func (s *Service) Apply(ctx context.Context, target string) (*updater.Job, error) {
	if s.cfg.UpdaterURL == "" {
		return nil, ErrUpdaterNotConfigured
	}
	// Every target goes through the same availability check, so a missing
	// updater is one clear answer rather than a transport error.
	view := s.updaterStatus(ctx, http.MethodGet, "/status")
	s.storeView(view)
	if view.Status != "ok" {
		if view.Error == "" {
			return nil, ErrUpdaterNotConfigured
		}
		return nil, errors.New(view.Error)
	}
	req := updater.UpdateRequest{}
	switch strings.TrimSpace(target) {
	case "", "latest", "branch":
		// A pinned install has nothing to move to on its own: an image install
		// reads its tag from .env and a detached checkout is on a tag, so both
		// need the release the check found naming the destination.
		pinned := (view.Checkout != nil && view.Checkout.Detached) ||
			(view.Mode == updater.ModeImage && (view.Release == nil || view.Release.Pinned))
		if pinned {
			s.mu.Lock()
			latest := s.latest
			s.mu.Unlock()
			if latest == nil {
				return nil, ErrNothingToApply
			}
			req.Tag = latest.Tag
		}
	default:
		req.Tag = strings.TrimSpace(target)
	}

	body, _ := json.Marshal(req)
	resp, err := s.call(ctx, http.MethodPost, "/update", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusAccepted {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = fmt.Sprintf("updater answered %d", resp.StatusCode)
		}
		return nil, errors.New(e.Error)
	}
	var job updater.Job
	if err := json.Unmarshal(raw, &job); err != nil {
		return nil, fmt.Errorf("decode updater answer: %w", err)
	}
	return &job, nil
}

// internals

func (s *Service) checkGitHub(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rels, err := releases.FetchReleases(cctx, s.http, s.cfg.GithubRepo, s.cfg.GithubToken)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkedAt = time.Now()
	if err != nil {
		s.checkErr = err.Error()
		log.Printf("updates: release check failed: %v", err)
		return
	}
	s.checkErr = ""
	stable, dev := releases.PickChannelHeads(rels)
	head := stable
	if s.cfg.Channel == "dev" && dev != nil {
		head = dev
	}
	if head == nil {
		s.latest = nil
		return
	}
	s.latest = &Latest{
		Tag: head.TagName, Name: head.Name, HTMLURL: head.HTMLURL,
		PublishedAt: head.PublishedAt, Channel: s.cfg.Channel,
	}
}

func (s *Service) compose(view UpdaterView, dropLogs bool) State {
	s.mu.Lock()
	st := State{
		Running:    version.Current(),
		Latest:     s.latest,
		CheckedAt:  s.checkedAt,
		CheckError: s.checkErr,
		Enabled:    s.cfg.Enabled,
		Interval:   s.cfg.Interval.String(),
		Channel:    s.cfg.Channel,
		Repo:       s.cfg.GithubRepo,
		Updater:    view,
	}
	s.mu.Unlock()

	if st.Latest != nil {
		if isNewer, ok := newer(st.Latest.Tag, st.Running.Version); ok && isNewer {
			st.UpdateAvailable = true
			st.Reason = "release"
		}
	}
	if !st.UpdateAvailable && view.Checkout != nil && !view.Checkout.Detached && view.Checkout.Behind > 0 {
		st.UpdateAvailable = true
		st.Reason = "commits"
	}
	if dropLogs {
		if st.Updater.Job != nil {
			st.Updater.Job = withoutLog(st.Updater.Job)
		}
		if st.Updater.LastJob != nil {
			st.Updater.LastJob = withoutLog(st.Updater.LastJob)
		}
	}
	return st
}

func withoutLog(j *updater.Job) *updater.Job {
	c := *j
	c.Log = nil
	return &c
}

func (s *Service) updaterStatus(ctx context.Context, method, path string) UpdaterView {
	if s.cfg.UpdaterURL == "" {
		return UpdaterView{Status: "off"}
	}
	view := UpdaterView{Configured: true}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	resp, err := s.call(cctx, method, path, nil)
	if err != nil {
		// Under compose the backend always gets UPDATER_URL=http://updater:8095,
		// profile or not. The compose service name not resolving is the
		// profile being off, which is report-only by choice, not a broken
		// updater. Any other host that fails is reported as unreachable.
		var dns *net.DNSError
		if errors.As(err, &dns) && s.composeServiceHost() {
			view.Status = "off"
			view.Error = "the updater is not running; enable the updater compose profile (make up does) to update from here"
			return view
		}
		view.Status = "unreachable"
		view.Error = describeDialError(err)
		return view
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		view.Status = "unreachable"
		view.Error = fmt.Sprintf("updater answered %d; check UPDATER_TOKEN matches on both sides", resp.StatusCode)
		return view
	}
	var st updater.Status
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&st); err != nil {
		view.Status = "unreachable"
		view.Error = "could not decode the updater's answer"
		return view
	}
	view.Status = "ok"
	view.Mode = st.Mode
	view.RepoDir = st.RepoDir
	view.Checkout = st.Checkout
	view.Release = st.Release
	view.Job = st.Job
	view.LastJob = st.LastJob
	return view
}

func (s *Service) call(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.UpdaterURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.UpdaterToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return s.http.Do(req)
}

// composeServiceHost reports whether UPDATER_URL names the compose service.
func (s *Service) composeServiceHost() bool {
	u, err := url.Parse(s.cfg.UpdaterURL)
	return err == nil && u.Hostname() == composeUpdaterHost
}

// composeUpdaterHost is the updater service's name in docker-compose.yml.
const composeUpdaterHost = "updater"

// describeDialError turns the usual "not running" failures into the sentence
// the panel shows, instead of a raw dial string.
func describeDialError(err error) string {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "the updater host does not resolve; check UPDATER_URL"
	}
	if strings.Contains(err.Error(), "connection refused") {
		return "the updater is not accepting connections; it is not running or UPDATER_URL points at the wrong port"
	}
	return err.Error()
}
