package compose

import (
	"context"
	"errors"
	"fmt"
)

// Migrate moves a project to another backend. It is the only way a
// project's backend changes, and the UI calls it only after the user
// confirmed the migration.
//
// Inputs: reg holds both backends; projects stores the metadata; confirm
// must be true (the user's explicit decision).
// Returns: ErrNotConfirmed without confirm (nothing is touched);
// ErrSameBackend; *ValidationError when the target rejects the project;
// otherwise the first error of the flow, joined with a rollback error.
// Side effects: current backend Down keeping volumes, target Up; on
// failure the target is brought down (volumes kept) and the old backend
// brought up again. Only after success is the new backend saved.
func Migrate(ctx context.Context, reg Registry, projects Projects, p *Project, to BackendID, confirm bool, progress Progress) error {
	if !confirm {
		return ErrNotConfirmed
	}
	if p.Backend == to {
		return ErrSameBackend
	}
	from, err := reg.For(p)
	if err != nil {
		return err
	}
	target, ok := reg[to]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownBackend, to)
	}
	av, err := target.Detect(ctx)
	if err != nil {
		return err
	}
	if !av.Available {
		return fmt.Errorf("compose: %s backend unavailable: %s", to, av.Reason)
	}
	rep, err := target.Validate(ctx, p)
	if err != nil {
		return err
	}
	if !rep.OK() {
		return &ValidationError{Report: rep}
	}
	progress.emit("", StepMigrate, "stopping the project on the %s backend (volumes are kept)", from.ID())
	if err := from.Down(ctx, p, DownOptions{}, progress); err != nil {
		return err
	}
	moved := *p
	moved.Backend = to
	progress.emit("", StepMigrate, "starting the project on the %s backend", to)
	if err := target.Up(ctx, &moved, UpOptions{}, progress); err != nil {
		progress.emit("", StepMigrate, "migration failed, rolling back to the %s backend", from.ID())
		rb := errors.Join(target.Down(ctx, &moved, DownOptions{}, progress), from.Up(ctx, p, UpOptions{}, progress))
		if rb != nil {
			return errors.Join(err, fmt.Errorf("compose: rollback: %w", rb))
		}
		return err
	}
	p.Backend = to
	return projects.Save(p)
}
