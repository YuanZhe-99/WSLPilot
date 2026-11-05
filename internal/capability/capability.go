// Package capability gathers what the machine supports: the WSL release,
// WSL Containers, and whether a native Compose is available. Domain packages
// and the GUI decide what to offer from a Report instead of probing again.
package capability

import (
	"context"
	"sync"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/wsl"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

// Report is one probe of the machine. An Err field set means that part
// could not be determined; its value fields are then zero.
type Report struct {
	At time.Time

	WSL     wsl.Version
	WSLErr  error
	Distros []string

	WSLc    wslc.Info
	WSLcErr error

	// NativeCompose is true when wslc has a compose command. It only
	// informs the user; see PLAN.md §4.3.
	NativeCompose bool
}

// HasWSL reports whether a WSL release with `wsl --version` was found.
func (r Report) HasWSL() bool { return r.WSLErr == nil && r.WSL.WSL != "" }

// HasWSLc reports whether wslc.exe answered.
func (r Report) HasWSLc() bool { return r.WSLcErr == nil && r.WSLc.Client.Version != "" }

// Prober runs the probes against the given clients.
type Prober struct {
	WSL  *wsl.Client
	WSLc *wslc.Client
}

// NewProber returns a Prober using the real programs.
func NewProber() *Prober { return &Prober{WSL: wsl.New(), WSLc: wslc.New()} }

// Probe runs every probe concurrently and returns the combined Report.
//
// Side effects: runs wsl.exe and wslc.exe (wslc may start its session
// manager). Never call from a view; run it on a goroutine.
func (p *Prober) Probe(ctx context.Context) Report {
	r := Report{At: time.Now()}
	var wg sync.WaitGroup
	wg.Go(func() {
		r.WSL, r.WSLErr = p.WSL.Version(ctx)
		if r.WSLErr == nil {
			r.Distros, _ = p.WSL.Names(ctx)
		}
	})
	wg.Go(func() {
		r.WSLc, r.WSLcErr = p.WSLc.Info(ctx)
		if r.WSLcErr == nil {
			r.NativeCompose, _ = p.WSLc.HasNativeCompose(ctx)
		}
	})
	wg.Wait()
	return r
}
