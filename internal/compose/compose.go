// Package compose runs Compose projects on WSL Containers through a
// replaceable Backend. The builtin backend translates each service into
// wslc containers, networks and volumes; the native backend is a stub until
// WSLc ships its own Compose command. See doc/en-us/compose.md for the
// contract, the project.json schema and the supported keys.
package compose

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// BackendID names a Compose backend. A project's backend is fixed in its
// metadata and changes only through Migrate.
type BackendID string

// The known backends.
const (
	// BackendBuiltin is WSLPilot's own translation onto wslc commands.
	BackendBuiltin BackendID = "builtin"
	// BackendNative is a future `wslc compose` (absent in WSLc 3.0.3.0).
	BackendNative BackendID = "native"
)

// Availability is what Detect found out about a backend.
type Availability struct {
	Available bool
	// Version is the upstream version the backend runs on, when known.
	Version string
	// Reason explains an unavailable backend in plain words.
	Reason string
}

// Severity grades an Issue.
type Severity string

// Issue severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Issue is one finding of Validate.
type Issue struct {
	// Service is the service name, or "" for a top-level key.
	Service string
	// Key is the Compose key path, e.g. "cap_add" or "networks.front.driver_opts".
	Key     string
	Message string
}

// String renders the issue for logs: "service: key: message".
func (i Issue) String() string {
	var b strings.Builder
	if i.Service != "" {
		b.WriteString(i.Service + ": ")
	}
	if i.Key != "" {
		b.WriteString(i.Key + ": ")
	}
	b.WriteString(i.Message)
	return b.String()
}

// Report is the result of validating a project for one backend.
// Notes: Up refuses to run while Errors is non-empty; Warnings never block.
type Report struct {
	Errors   []Issue
	Warnings []Issue
}

// OK reports whether the report has no errors.
func (r Report) OK() bool { return len(r.Errors) == 0 }

// errorf appends an error issue.
func (r *Report) errorf(service, key, format string, a ...any) {
	r.Errors = append(r.Errors, Issue{Service: service, Key: key, Message: fmt.Sprintf(format, a...)})
}

// warnf appends a warning issue.
func (r *Report) warnf(service, key, format string, a ...any) {
	r.Warnings = append(r.Warnings, Issue{Service: service, Key: key, Message: fmt.Sprintf(format, a...)})
}

// ValidationError is returned by Up (and Migrate) when the project has
// validation errors; nothing was changed.
type ValidationError struct{ Report Report }

// Error lists the first few errors.
func (e *ValidationError) Error() string {
	parts := make([]string, 0, 3)
	for i, is := range e.Report.Errors {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("and %d more", len(e.Report.Errors)-3))
			break
		}
		parts = append(parts, is.String())
	}
	return "compose: project is not supported: " + strings.Join(parts, "; ")
}

// Errors returned by backends and Migrate.
var (
	// ErrNativeUnavailable is returned by every native-backend operation
	// while WSLc has no Compose command.
	ErrNativeUnavailable = errors.New("compose: the native Compose backend is not available")
	// ErrNotConfirmed means Migrate was called without user confirmation.
	ErrNotConfirmed = errors.New("compose: migration needs the user's confirmation")
	// ErrSameBackend means Migrate was asked to migrate to the current backend.
	ErrSameBackend = errors.New("compose: the project already uses that backend")
	// ErrUnknownBackend means no Backend is registered for an ID.
	ErrUnknownBackend = errors.New("compose: unknown backend")
)

// Step names a phase reported through Progress.
type Step string

// Progress steps.
const (
	StepLoad     Step = "load"
	StepNetwork  Step = "network"
	StepVolume   Step = "volume"
	StepPull     Step = "pull"
	StepBuild    Step = "build"
	StepCreate   Step = "create"
	StepRecreate Step = "recreate"
	StepKeep     Step = "keep"
	StepStart    Step = "start"
	StepWait     Step = "wait"
	StepStop     Step = "stop"
	StepRestart  Step = "restart"
	StepRemove   Step = "remove"
	StepOrphan   Step = "orphan"
	StepMigrate  Step = "migrate"
)

// Event is one progress report.
type Event struct {
	// Service is the service concerned, or "" for project-level steps.
	Service string
	Step    Step
	Message string
}

// Progress receives events; a nil Progress discards them.
type Progress func(Event)

// emit calls p when it is set.
func (p Progress) emit(service string, step Step, format string, a ...any) {
	if p != nil {
		p(Event{Service: service, Step: step, Message: fmt.Sprintf(format, a...)})
	}
}

// UpOptions tune Up.
type UpOptions struct {
	// RemoveOrphans removes containers of services no longer in the files.
	RemoveOrphans bool
	// Build rebuilds images of services with a build section.
	Build bool
	// ForceRecreate recreates containers whose config did not change.
	ForceRecreate bool
	// WaitTimeout bounds each depends_on wait; 0 means two minutes.
	WaitTimeout int // seconds
	// Log receives pull and build output; nil discards it.
	Log io.Writer
}

// DownOptions tune Down.
type DownOptions struct {
	// RemoveVolumes also deletes the project's named volumes and their data.
	// Callers confirm first (behavior contract: never destroy data silently).
	RemoveVolumes bool
}

// LogsOptions select the log lines Logs streams.
type LogsOptions struct {
	Services   []string // empty = all
	Follow     bool
	Tail       int // 0 = all
	Timestamps bool
}

// ContainerStatus is one project container as read back from wslc.
type ContainerStatus struct {
	Service string
	Name    string
	ID      string
	Number  int
	Image   string
	State   string // created, running, exited, …
	Health  string // healthy, unhealthy, starting, or ""
	Exit    int
	Backend string // value of io.wslpilot.backend
}

// Backend runs Compose projects. Every method reads state back from
// upstream; a backend keeps no cache of containers.
type Backend interface {
	// ID returns the backend's identifier.
	ID() BackendID
	// Detect reports whether the backend can run on this machine.
	Detect(ctx context.Context) (Availability, error)
	// Validate loads the project and reports every key the backend cannot
	// honor. The error is for load or upstream failures, not findings.
	Validate(ctx context.Context, p *Project) (Report, error)
	// Up creates or updates the project's resources; it is idempotent.
	Up(ctx context.Context, p *Project, o UpOptions, progress Progress) error
	// Down stops and removes the project's containers and networks.
	Down(ctx context.Context, p *Project, o DownOptions, progress Progress) error
	// Start starts the given services' containers (all when empty).
	Start(ctx context.Context, p *Project, services []string, progress Progress) error
	// Stop stops the given services' containers (all when empty).
	Stop(ctx context.Context, p *Project, services []string, progress Progress) error
	// Restart restarts the given services' containers (all when empty).
	Restart(ctx context.Context, p *Project, services []string, progress Progress) error
	// Ps lists the project's containers.
	Ps(ctx context.Context, p *Project) ([]ContainerStatus, error)
	// Logs streams the services' logs to w, each line prefixed by the
	// container name.
	Logs(ctx context.Context, p *Project, o LogsOptions, w io.Writer) error
}

// Registry maps backend IDs to implementations.
type Registry map[BackendID]Backend

// For returns the backend a project is fixed to.
func (r Registry) For(p *Project) (Backend, error) {
	b, ok := r[p.Backend]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownBackend, p.Backend)
	}
	return b, nil
}
