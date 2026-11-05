package compose

import (
	"context"
	"io"
)

// Native is the placeholder for an upstream `wslc compose`. WSLc 3.0.3.0
// has no such command, so every operation returns ErrNativeUnavailable;
// the type keeps the Backend interface and the migration flow honest.
type Native struct {
	// HasCompose is the capability probe's result (wslc.HasNativeCompose).
	// It only informs the user; it never switches a project's backend.
	HasCompose bool
}

var _ Backend = (*Native)(nil)

// ID returns BackendNative.
func (n *Native) ID() BackendID { return BackendNative }

// Detect reports whether upstream has a Compose command. Even when it does,
// WSLPilot has no contract-tested support for it yet.
func (n *Native) Detect(context.Context) (Availability, error) {
	if !n.HasCompose {
		return Availability{Reason: "this WSL Containers version has no Compose command"}, nil
	}
	return Availability{Reason: "WSLPilot does not support the native Compose command yet"}, nil
}

// Validate returns ErrNativeUnavailable.
func (n *Native) Validate(context.Context, *Project) (Report, error) {
	return Report{}, ErrNativeUnavailable
}

// Up returns ErrNativeUnavailable.
func (n *Native) Up(context.Context, *Project, UpOptions, Progress) error {
	return ErrNativeUnavailable
}

// Down returns ErrNativeUnavailable.
func (n *Native) Down(context.Context, *Project, DownOptions, Progress) error {
	return ErrNativeUnavailable
}

// Start returns ErrNativeUnavailable.
func (n *Native) Start(context.Context, *Project, []string, Progress) error {
	return ErrNativeUnavailable
}

// Stop returns ErrNativeUnavailable.
func (n *Native) Stop(context.Context, *Project, []string, Progress) error {
	return ErrNativeUnavailable
}

// Restart returns ErrNativeUnavailable.
func (n *Native) Restart(context.Context, *Project, []string, Progress) error {
	return ErrNativeUnavailable
}

// Ps returns ErrNativeUnavailable.
func (n *Native) Ps(context.Context, *Project) ([]ContainerStatus, error) {
	return nil, ErrNativeUnavailable
}

// Logs returns ErrNativeUnavailable.
func (n *Native) Logs(context.Context, *Project, LogsOptions, io.Writer) error {
	return ErrNativeUnavailable
}
