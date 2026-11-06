package backup

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/version"
	"github.com/YuanZhe-99/WSLPilot/internal/volume"
	"github.com/YuanZhe-99/WSLPilot/internal/wsl"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

// Runner backs up and restores live objects.
type Runner struct {
	WSL  *wsl.Client
	WSLc *wslc.Client
	Vol  *volume.Pipeline
	// Host is recorded in sources (the computer name).
	Host string
	// Log receives progress text; may be nil.
	Log func(string)
}

// Options control one backup.
type Options struct {
	// Live leaves a running source running: the copy may be inconsistent
	// and is labeled so. The default stops it first and restarts it after.
	Live bool
	Plan string
}

func (r *Runner) logf(format string, a ...any) {
	if r.Log != nil {
		r.Log(fmt.Sprintf(format, a...))
	}
}

// BackupDistro exports a distribution into w.
// Side effects: terminates the distribution when it runs (unless Live)
// and starts it again afterwards.
func (r *Runner) BackupDistro(ctx context.Context, name string, w Writer, o Options) (Item, error) {
	d, err := r.WSL.Get(ctx, name)
	if err != nil {
		return Item{}, err
	}
	consistent := true
	if d.Running {
		if o.Live {
			consistent = false
		} else {
			r.logf("Stopping %s for a consistent copy", name)
			if err := r.WSL.Terminate(ctx, name); err != nil {
				return Item{}, err
			}
			defer func() {
				r.logf("Starting %s again", name)
				r.WSL.Start(context.Background(), name)
			}()
		}
	}
	m := Meta{Source: Source{Kind: SourceDistro, Name: name, Host: r.Host}, Time: time.Now(), Consistent: consistent, Plan: o.Plan, Tool: tool()}
	return r.pipe(ctx, w, m, func(pw io.Writer) error { return r.WSL.Export(ctx, name, "-", wsl.FormatTar, pw) })
}

// BackupVolume exports a WSLc volume into w.
// Side effects: stops running containers that use it (unless Live) and
// starts them again afterwards.
func (r *Runner) BackupVolume(ctx context.Context, name string, w Writer, o Options) (Item, error) {
	consistent := true
	using, err := r.Vol.UsingContainers(ctx, name)
	if err != nil {
		return Item{}, err
	}
	var stopped []string
	for _, c := range using {
		if c.Running() {
			stopped = append(stopped, c.ID)
		}
	}
	if len(stopped) > 0 {
		if o.Live {
			consistent = false
		} else {
			r.logf("Stopping %d container(s) that use %s", len(stopped), name)
			if err := r.WSLc.Stop(ctx, -1, stopped...); err != nil {
				return Item{}, err
			}
			defer func() {
				for _, id := range stopped {
					r.WSLc.Start(context.Background(), id)
				}
			}()
		}
	}
	m := Meta{Source: Source{Kind: SourceVolume, Name: name, Host: r.Host}, Time: time.Now(), Consistent: consistent, Plan: o.Plan, Tool: tool()}
	return r.pipe(ctx, w, m, func(pw io.Writer) error { _, err := r.Vol.Export(ctx, name, pw); return err })
}

// pipe connects a producer of a tar stream to a Writer.
func (r *Runner) pipe(ctx context.Context, w Writer, m Meta, produce func(io.Writer) error) (Item, error) {
	pr, pw := io.Pipe()
	errc := make(chan error, 1)
	go func() {
		err := produce(pw)
		pw.CloseWithError(err)
		errc <- err
	}()
	it, werr := w.Write(ctx, pr, m)
	if werr == nil {
		// tar readers stop at the end marker; the producer may still write
		// record padding, which must be drained for it to exit cleanly.
		io.Copy(io.Discard, pr)
	}
	pr.CloseWithError(errors.New("backup: writer stopped"))
	perr := <-errc
	if perr != nil {
		return Item{}, perr
	}
	return it, werr
}

// RestoreAsDistro registers the item as a new distribution in location.
// Notes: a VHDX item is imported with --vhd (WSL copies it); every other
// kind is streamed as tar into `wsl --import <name> <location> -`.
func (r *Runner) RestoreAsDistro(ctx context.Context, it Item, name, location string) error {
	if _, err := r.WSL.Get(ctx, name); err == nil {
		return fmt.Errorf("a distribution named %s already exists", name)
	}
	log := logWriter{r}
	if it.Kind == KindVHDX {
		return r.WSL.Import(ctx, name, location, it.Path, wsl.ImportOptions{VHD: true}, nil, log)
	}
	rc, err := Open(ctx, it)
	if err != nil {
		return err
	}
	defer rc.Close()
	return r.WSL.Import(ctx, name, location, "-", wsl.ImportOptions{}, rc, log)
}

// ReplaceDistro restores over an existing distribution: it first backs the
// current one up into safety, then unregisters it and imports the item
// under the same name, at the same place.
// Notes: the user has confirmed; without a safety Writer it refuses.
func (r *Runner) ReplaceDistro(ctx context.Context, it Item, name string, safety Writer) (Item, error) {
	if safety == nil {
		return Item{}, errors.New("backup: replacing a distribution needs a safety backup")
	}
	d, err := r.WSL.Get(ctx, name)
	if err != nil {
		return Item{}, err
	}
	saved, err := r.BackupDistro(ctx, name, safety, Options{})
	if err != nil {
		return Item{}, fmt.Errorf("safety backup failed, nothing was changed: %w", err)
	}
	if err := r.WSL.Unregister(ctx, name); err != nil {
		return saved, err
	}
	location := d.BasePath
	if location == "" {
		return saved, errors.New("backup: the distribution's folder is unknown")
	}
	return saved, r.RestoreAsDistro(ctx, it, name, location)
}

// RestoreIntoDistro unpacks the item into a folder of an existing
// distribution as root (partial restore), keeping owners and modes.
// Inputs: filter selects entries by name (nil: all).
func (r *Runner) RestoreIntoDistro(ctx context.Context, it Item, name, dir string, filter func(string) bool) error {
	rc, err := Open(ctx, it)
	if err != nil {
		return err
	}
	defer rc.Close()
	src := io.Reader(rc)
	if filter != nil {
		src = filterTar(rc, filter)
	}
	return r.WSL.ExecStream(ctx, name, "root", src, nil, "tar", "-xpf", "-", "--numeric-owner", "-C", dir)
}

// RestoreToVolume unpacks the item into a volume (new, or replaced after
// the user confirmed and a safety backup was made by the caller).
func (r *Runner) RestoreToVolume(ctx context.Context, it Item, name string, o volume.ImportOptions) error {
	rc, err := Open(ctx, it)
	if err != nil {
		return err
	}
	defer rc.Close()
	_, _, err = r.Vol.Import(ctx, name, rc, o)
	return err
}

// RestoreProjectVolume unpacks one volume of a project backup into a
// volume (new, or replaced after the user confirmed and the caller made a
// safety backup).
func (r *Runner) RestoreProjectVolume(ctx context.Context, it Item, volumeName, into string, o volume.ImportOptions) error {
	rc, err := ProjectVolumeStream(ctx, it, volumeName)
	if err != nil {
		return err
	}
	defer rc.Close()
	_, _, err = r.Vol.Import(ctx, into, rc, o)
	return err
}

// filterTar re-emits only entries whose name passes filter.
func filterTar(r io.Reader, filter func(string) bool) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		tr := tar.NewReader(r)
		tw := tar.NewWriter(pw)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				pw.CloseWithError(tw.Close())
				return
			}
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			if !filter(h.Name) {
				continue
			}
			if err := tw.WriteHeader(h); err != nil {
				pw.CloseWithError(err)
				return
			}
			if _, err := io.Copy(tw, tr); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
	}()
	return pr
}

type logWriter struct{ r *Runner }

func (l logWriter) Write(p []byte) (int, error) { l.r.logf("%s", p); return len(p), nil }

func tool() string { return "WSLPilot " + version.Version }
