// Package updater is the host-side agent that applies an update to a
// self-hosted Warmbly: pull the checkout, rebuild, restart, and wait for the
// backend to answer again. It runs next to the stack (a compose sidecar that
// holds the docker socket, or a systemd unit on a bare-metal host) and the
// backend drives it over a token-authenticated HTTP API that is never exposed
// publicly. This file is the wire contract both sides compile against.
package updater

import "time"

// Mode selects how the runner rebuilds and restarts after the checkout moved.
type Mode string

const (
	// ModeCompose rebuilds the images and recreates the containers of the
	// compose project the checkout belongs to.
	ModeCompose Mode = "compose"
	// ModeCommand runs an operator-provided command (a build-and-restart
	// script) and leaves the rest to it.
	ModeCommand Mode = "command"
	// ModeImage is the clone-free install: no git, no build. The release tag
	// is pinned in the install's .env, images are pulled from the registry and
	// the containers whose image changed are recreated.
	ModeImage Mode = "image"
)

// JobStatus is the lifecycle of one update run.
type JobStatus string

const (
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
)

// Checkout describes the git checkout the updater manages.
type Checkout struct {
	Branch       string    `json:"branch"`
	Detached     bool      `json:"detached"`
	Commit       string    `json:"commit"`
	Describe     string    `json:"describe"`
	RemoteCommit string    `json:"remote_commit"`
	Behind       int       `json:"behind"`
	Dirty        bool      `json:"dirty"`
	FetchedAt    time.Time `json:"fetched_at"`
	FetchError   string    `json:"fetch_error,omitempty"`
}

// Job is one update run, with the tail of its log.
type Job struct {
	ID         string     `json:"id"`
	Status     JobStatus  `json:"status"`
	Target     string     `json:"target"`
	Step       string     `json:"step"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Error      string     `json:"error,omitempty"`
	FromCommit string     `json:"from_commit"`
	ToCommit   string     `json:"to_commit,omitempty"`
	Log        []string   `json:"log"`
}

// Release describes an image-mode install: which tag its compose file
// resolves and where the images come from. It is the image-mode counterpart
// of Checkout, and exactly one of the two is ever set.
type Release struct {
	// Tag is the value of WARMBLY_TAG in the install's .env, or the compose
	// default when it names none.
	Tag string `json:"tag"`
	// Prefix is the registry namespace every image is pulled from.
	Prefix string `json:"prefix"`
	// Pinned is whether Tag is a fixed version rather than a moving channel
	// tag. A moving tag republishes under the same name, so re-pulling it is
	// a real update; a pinned one only moves when a release is chosen.
	Pinned bool `json:"pinned"`
}

// Status is the updater's answer to GET /status.
type Status struct {
	Mode    Mode   `json:"mode"`
	RepoDir string `json:"repo_dir"`
	Version string `json:"version"`
	// Checkout is set in compose and command mode, Release in image mode.
	Checkout *Checkout `json:"checkout,omitempty"`
	Release  *Release  `json:"release,omitempty"`
	Job      *Job      `json:"job,omitempty"`
	LastJob  *Job      `json:"last_job,omitempty"`
}

// UpdateRequest is the body of POST /update. An empty Tag means: pull the
// tracked branch when the checkout is on one, otherwise refuse. In image mode
// it means re-pull whatever tag the install is already pinned to.
type UpdateRequest struct {
	Tag string `json:"tag"`
}
