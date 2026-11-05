package compose

import (
	"sort"
	"strconv"

	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

// ActionKind is what Up does with one container.
type ActionKind string

// Action kinds.
const (
	ActCreate   ActionKind = "create"
	ActRecreate ActionKind = "recreate"
	ActStart    ActionKind = "start"
	ActKeep     ActionKind = "keep"
	ActRemove   ActionKind = "remove" // scale-down or orphan
)

// Action is one step of reconciling a project.
type Action struct {
	Kind    ActionKind
	Service string
	// Want is the desired container (nil for ActRemove).
	Want *ContainerPlan
	// ID and Name identify the existing container (empty for ActCreate).
	ID   string
	Name string
	// Reason explains recreate and remove in plain words.
	Reason string
	// Orphan marks a container whose service left the Compose files.
	Orphan bool
}

// DiffOptions tune Diff.
type DiffOptions struct {
	RemoveOrphans bool
	ForceRecreate bool
	// ImageIDs maps a service to its current image ID; a container built
	// from another image is recreated. Missing entries are not compared.
	ImageIDs map[string]string
}

// Diff compares the plan with the project's existing containers (found by
// the project label) and returns the actions, services in plan order.
//
// Returns: actions; orphans appear as ActRemove only with RemoveOrphans,
// otherwise as ActKeep with Orphan set so the caller can warn.
// Notes: pure. A container matches a desired one by service and container
// number; one with a different config hash or image is recreated, a
// stopped match is started, extra replicas are removed.
func Diff(plan *Plan, actual []wslc.Container, o DiffOptions) []Action {
	type key struct {
		service string
		number  int
	}
	have := map[key][]wslc.Container{}
	services := map[string]bool{}
	for _, s := range plan.Services {
		services[s.Name] = true
	}
	var orphans []wslc.Container
	for _, c := range actual {
		svc := labelsOf(c)[LabelService]
		if !services[svc] {
			orphans = append(orphans, c)
			continue
		}
		n, _ := strconv.Atoi(labelsOf(c)[LabelNumber])
		k := key{svc, n}
		have[k] = append(have[k], c)
	}
	var acts []Action
	for _, s := range plan.Services {
		wanted := map[int]bool{}
		for i := range s.Containers {
			want := &s.Containers[i]
			wanted[want.Number] = true
			cs := have[key{s.Name, want.Number}]
			if len(cs) == 0 {
				acts = append(acts, Action{Kind: ActCreate, Service: s.Name, Want: want})
				continue
			}
			c := cs[0]
			for _, dup := range cs[1:] {
				acts = append(acts, Action{Kind: ActRemove, Service: s.Name, ID: dup.ID, Name: dup.DisplayName(), Reason: "duplicate container number"})
			}
			base := Action{Service: s.Name, Want: want, ID: c.ID, Name: c.DisplayName()}
			switch {
			case o.ForceRecreate:
				base.Kind, base.Reason = ActRecreate, "recreate requested"
			case labelsOf(c)[LabelConfigHash] != want.Hash:
				base.Kind, base.Reason = ActRecreate, "configuration changed"
			case c.DisplayName() != want.Name:
				base.Kind, base.Reason = ActRecreate, "container name changed"
			case o.ImageIDs[s.Name] != "" && c.ImageID != o.ImageIDs[s.Name]:
				base.Kind, base.Reason = ActRecreate, "image changed"
			case c.State.Running:
				base.Kind = ActKeep
			default:
				base.Kind = ActStart
			}
			acts = append(acts, base)
		}
		var extra []wslc.Container
		for k, cs := range have {
			if k.service == s.Name && !wanted[k.number] {
				extra = append(extra, cs...)
			}
		}
		sort.Slice(extra, func(i, j int) bool { return extra[i].DisplayName() < extra[j].DisplayName() })
		for _, c := range extra {
			acts = append(acts, Action{Kind: ActRemove, Service: s.Name, ID: c.ID, Name: c.DisplayName(), Reason: "scaled down"})
		}
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].DisplayName() < orphans[j].DisplayName() })
	for _, c := range orphans {
		a := Action{Service: labelsOf(c)[LabelService], ID: c.ID, Name: c.DisplayName(), Orphan: true, Reason: "service is no longer in the Compose files"}
		if o.RemoveOrphans {
			a.Kind = ActRemove
		} else {
			a.Kind = ActKeep
		}
		acts = append(acts, a)
	}
	return acts
}

// labelsOf returns a container's labels from inspect (Config.Labels, or
// the top-level copy wslc also prints).
func labelsOf(c wslc.Container) map[string]string {
	if len(c.Config.Labels) > 0 {
		return c.Config.Labels
	}
	return c.Labels
}
