package gui

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/compose"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/volume"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// System cleanup (doc/en-us/gui.md, "Containers"): what `docker system
// prune` does, from WSLc's own prune commands, which have no system-wide
// form. Volumes are WSLPilot's choice, not `wslc volume prune`: a Compose
// project's volumes stay, even with the project down.

// cleanupInput holds the cleanup dialog's choices.
type cleanupInput struct {
	containers, networks, images, allImages bool
	volumes, backup                         bool
}

// openCleanup opens the cleanup dialog with the safe choices on.
func (a *App) openCleanup() {
	a.ctr.clean = cleanupInput{containers: true, networks: true, images: true, backup: true}
	a.ctr.dlg = "cleanup"
	a.loadVolumes()
	if !a.cmp.loaded && a.k.Compose != nil {
		a.loadProjects()
	}
}

// cleanupVolumes lists the volumes a cleanup removes: those none of the
// containers left mounts (stopped ones count as gone when removeStopped)
// and that no added Compose project owns.
// Inputs: owned reports whether a project name is an added project.
func cleanupVolumes(vols []wslc.Volume, cs []wslc.Container, removeStopped bool, owned func(string) bool) []string {
	used := map[string]bool{}
	for _, c := range cs {
		if c.Labels[volume.HelperLabel] == "1" || c.Config.Labels[volume.HelperLabel] == "1" {
			continue
		}
		if removeStopped && !c.State.Running {
			continue
		}
		for _, m := range c.Mounts {
			if m.Type == "volume" {
				used[m.Name] = true
			}
		}
	}
	var out []string
	for _, v := range vols {
		if used[v.Name] || owned(v.Labels[compose.LabelProject]) {
			continue
		}
		out = append(out, v.Name)
	}
	return out
}

// projectOwner reports, for the volumes' project label, whether it names
// an added Compose project.
func (a *App) projectOwner() func(string) bool {
	names := map[string]bool{}
	for _, p := range a.cmp.projects {
		names[p.Name] = true
	}
	return func(n string) bool { return n != "" && names[n] }
}

// cleanupDialog shows what a cleanup removes and runs it.
func (a *App) cleanupDialog(c *ui.Context) {
	p := &a.ctr
	in := &p.clean
	t := c.Theme()
	ui.Text(c, L.CtrCleanupTitle()).FontSize(16).Bold()
	muted(c, L.CtrCleanupHelp())
	stopped := 0
	var details []wslc.Container
	for _, s := range p.list {
		if !s.Running() {
			stopped++
		}
		if d, ok := p.detail[s.ID]; ok {
			details = append(details, d)
		}
	}
	defaultSession := p.client.Load() == nil
	vols := cleanupVolumes(a.vol.list, details, in.containers, a.projectOwner())
	option := func(key string, on *bool, label, help string) {
		ui.Column(c.Key(key)).FillWidth().Gap(2).Children(func() {
			ui.Checkbox(c.Key(key+"-cb"), on, label)
			if help != "" {
				small(c, help).Padding(0, 0, 0, 28)
			}
		})
	}
	option("clean-containers", &in.containers, L.CtrCleanupContainers(stopped), L.CtrCleanupContainersHelp())
	option("clean-networks", &in.networks, L.CtrCleanupNetworks(), "")
	option("clean-images", &in.images, L.CtrCleanupImages(), "")
	if in.images {
		ui.Box(c).Padding(0, 0, 0, 28).Children(func() {
			ui.Checkbox(c.Key("clean-images-all"), &in.allImages, L.CtrCleanupImagesAll())
		})
	}
	if defaultSession {
		option("clean-volumes", &in.volumes, L.CtrCleanupVolumes(len(vols)), L.CtrCleanupVolumesHelp())
		if in.volumes {
			ui.Column(c.Key("clean-vol-list")).FillWidth().Gap(4).Padding(0, 0, 0, 28).Children(func() {
				if len(vols) > 0 {
					ui.Text(c, strings.Join(vols, ", ")).FontSize(sizeSmall).TextColor(t.Danger)
				}
				ui.Checkbox(c.Key("clean-backup"), &in.backup, L.ConfirmBackupFirst())
			})
		}
	} else {
		small(c, L.CtrCleanupVolumesOther())
	}
	some := in.containers || in.networks || in.images || (in.volumes && defaultSession)
	choice := *in
	choice.volumes = choice.volumes && defaultSession
	dialogButtons(c, a, L.CtrCleanup(), some, func() { a.runCleanup(choice) })
}

// runCleanup removes, in order, stopped containers (which frees what only
// they used), unused networks, images, then volumes, as one job.
// Side effects: every step is written to the job's log and the audit log;
// volumes are backed up to Safety first when chosen.
func (a *App) runCleanup(in cleanupInput) {
	cl := a.wc()
	owned := a.projectOwner()
	a.startJob(L.CtrCleanupTitle(), func(ctx context.Context, j *jobs.Job) error {
		prune := func(kind string, all bool) error {
			j.SetProgress(-1, kind)
			out, err := cl.Prune(ctx, kind, all)
			if strings.TrimSpace(out) != "" {
				j.Logf(strings.TrimSpace(out))
			}
			a.k.Jobs.Audit("wslc."+kind+".prune", kind, "", err)
			return err
		}
		if in.containers {
			if err := prune("container", false); err != nil {
				return err
			}
		}
		if in.networks {
			if err := prune("network", false); err != nil {
				return err
			}
		}
		if in.images {
			if err := prune("image", in.allImages); err != nil {
				return err
			}
		}
		if !in.volumes {
			return nil
		}
		// Read again: the containers removed above no longer hold theirs.
		vs, err := cl.Volumes(ctx)
		if err != nil {
			return err
		}
		cs, err := cl.ContainersDetailed(ctx)
		if err != nil {
			return err
		}
		for _, name := range cleanupVolumes(vs, cs, false, owned) {
			if in.backup {
				j.SetProgress(-1, L.JobBackingUp()+" "+name)
				if _, err := a.k.Backup.BackupVolume(ctx, name, a.safetyWriter(), backupOptions()); err != nil {
					return fmt.Errorf("%s: %w", L.JobBackupFailedStop(), err)
				}
			}
			err := cl.RemoveVolumes(ctx, name)
			a.k.Jobs.Audit("wslc.volume.remove", name, "cleanup", err)
			if err != nil {
				return err
			}
			j.Logf("removed volume " + name)
		}
		return nil
	}, func() { a.loadContainers(); a.loadVolumes() })
}
