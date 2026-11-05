// Command wslpilot-cli is WSLPilot's scripting and debugging entry point
// over the same packages as the GUI.
//
//	wslpilot-cli status
//	wslpilot-cli distros | containers | volumes
//	wslpilot-cli backup distro|volume <name> --to folder|archive|repo:<path> [--live]
//	wslpilot-cli catalog <path>
//	wslpilot-cli verify <path> [--id <snapshot>]
//	wslpilot-cli restore <path> [--id <snapshot>] --distro <new name> --location <dir>
//	wslpilot-cli restore <path> [--id <snapshot>] --volume <new name>
//	wslpilot-cli compact <distro>
//	wslpilot-cli plans | run-plan <id>
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/core"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/version"
	"github.com/YuanZhe-99/WSLPilot/internal/volume"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	k, err := core.New()
	if err != nil {
		fail(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	k.Backup.Log = func(s string) { fmt.Fprintln(os.Stderr, strings.TrimRight(s, "\n")) }
	args := os.Args[2:]
	switch os.Args[1] {
	case "status":
		err = status(ctx, k)
	case "distros":
		err = distros(ctx, k)
	case "containers":
		err = containers(ctx, k)
	case "volumes":
		err = volumes(ctx, k)
	case "backup":
		err = runBackup(ctx, k, args)
	case "catalog":
		err = catalog(args)
	case "verify":
		err = verify(ctx, args)
	case "restore":
		err = restore(ctx, k, args)
	case "compact":
		if len(args) != 1 {
			usage()
		}
		err = k.WSL.Compact(ctx, args[0], os.Stderr)
	case "plans":
		err = plans(k)
	case "run-plan":
		err = runPlan(ctx, k, args)
	case "version":
		fmt.Println("WSLPilot", version.Version)
	default:
		usage()
	}
	if err != nil {
		fail(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: wslpilot-cli status | distros | containers | volumes
  backup distro|volume <name> --to folder|archive|repo:<path> [--live]
  catalog <path> | verify <path> [--id <snapshot>]
  restore <path> [--id <snapshot>] (--distro <name> --location <dir> | --volume <name>)
  compact <distro> | plans | run-plan <id> | version`)
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "wslpilot-cli:", err)
	os.Exit(1)
}

func table(header string, rows func(w *tabwriter.Writer)) {
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, header)
	rows(w)
	w.Flush()
}

func status(ctx context.Context, k *core.Core) error {
	r := k.Prober.Probe(ctx)
	if r.HasWSL() {
		fmt.Printf("WSL %s (kernel %s), %d distribution(s)\n", r.WSL.WSL, r.WSL.Kernel, len(r.Distros))
	} else {
		fmt.Println("WSL: not found:", r.WSLErr)
	}
	if r.HasWSLc() {
		fmt.Printf("WSL Containers %s, native Compose: %v\n", r.WSLc.Client.Version, r.NativeCompose)
	} else {
		fmt.Println("WSL Containers: not found:", r.WSLcErr)
	}
	return nil
}

func distros(ctx context.Context, k *core.Core) error {
	ds, err := k.WSL.List(ctx)
	if err != nil {
		return err
	}
	table("NAME\tDEFAULT\tRUNNING\tVERSION\tDISK\tPATH", func(w *tabwriter.Writer) {
		for _, d := range ds {
			fmt.Fprintf(w, "%s\t%v\t%v\t%d\t%s\t%s\n", d.Name, d.Default, d.Running, d.Version, jobs.FormatBytes(d.DiskBytes), d.VHDPath)
		}
	})
	return nil
}

func containers(ctx context.Context, k *core.Core) error {
	cs, err := k.WSLc.Containers(ctx)
	if err != nil {
		return err
	}
	table("NAME\tIMAGE\tSTATE\tPORTS", func(w *tabwriter.Writer) {
		for _, c := range cs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.Names, c.Image, c.State, c.Ports)
		}
	})
	return nil
}

func volumes(ctx context.Context, k *core.Core) error {
	vs, err := k.WSLc.Volumes(ctx)
	if err != nil {
		return err
	}
	table("NAME\tDRIVER\tHOST PATH", func(w *tabwriter.Writer) {
		for _, v := range vs {
			fmt.Fprintf(w, "%s\t%s\t%s\n", v.Name, v.Driver, v.Status.HostPath)
		}
	})
	return nil
}

// parseDest reads "kind:path".
func parseDest(s string) (backup.Writer, error) {
	kind, path, ok := strings.Cut(s, ":")
	if !ok || len(kind) == 1 { // a bare "C:\..." has a one-letter kind
		return nil, errors.New("--to must be folder:<path>, archive:<path> or repo:<path>")
	}
	switch kind {
	case "folder":
		return &backup.FolderWriter{Dir: path}, nil
	case "archive":
		return &backup.ArchiveWriter{Dir: path, Compression: backup.CompressZstd}, nil
	case "repo":
		return &backup.RepoWriter{Dir: path}, nil
	}
	return nil, fmt.Errorf("unknown destination kind %q", kind)
}

func runBackup(ctx context.Context, k *core.Core, args []string) error {
	if len(args) < 2 {
		usage()
	}
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	to := fs.String("to", "", "destination kind:path")
	live := fs.Bool("live", false, "do not stop the source (copy may be inconsistent)")
	fs.Parse(args[2:])
	w, err := parseDest(*to)
	if err != nil {
		return err
	}
	var it backup.Item
	switch args[0] {
	case "distro":
		it, err = k.Backup.BackupDistro(ctx, args[1], w, backup.Options{Live: *live})
	case "volume":
		it, err = k.Backup.BackupVolume(ctx, args[1], w, backup.Options{Live: *live})
	default:
		usage()
	}
	if err != nil {
		return err
	}
	fmt.Println(describe(it))
	return nil
}

func describe(it backup.Item) string {
	s := fmt.Sprintf("%s\t%s %s\t%s\t%s", it.Time.Local().Format("2006-01-02 15:04"), it.Source.Kind, it.Source.Name, it.Kind, it.Path)
	if it.ID != "" {
		s += "#" + it.ID[:12]
	}
	if !it.Consistent {
		s += "\t(inconsistent)"
	}
	if it.Note != "" {
		s += "\t" + it.Note
	}
	return s
}

func catalog(args []string) error {
	if len(args) != 1 {
		usage()
	}
	items, errs := backup.Scan(backup.Location{Path: args[0], ReadOnly: true})
	for _, it := range items {
		fmt.Println(describe(it))
	}
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "warning:", e)
	}
	return nil
}

// pick finds the item at path (a snapshot by --id for repositories).
func pick(path, id string) (backup.Item, error) {
	items, errs := backup.Scan(backup.Location{Path: path, ReadOnly: true})
	var match []backup.Item
	for _, it := range items {
		if id == "" || strings.HasPrefix(it.ID, id) {
			match = append(match, it)
		}
	}
	switch {
	case len(match) == 1:
		return match[0], nil
	case len(match) == 0 && len(errs) > 0:
		return backup.Item{}, errs[0]
	case len(match) == 0:
		return backup.Item{}, errors.New("no backup found there")
	}
	return backup.Item{}, fmt.Errorf("%d backups found; choose one with --id or a more specific path", len(match))
}

func verify(ctx context.Context, args []string) error {
	if len(args) < 1 {
		usage()
	}
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	id := fs.String("id", "", "snapshot ID (repositories)")
	fs.Parse(args[1:])
	it, err := pick(args[0], *id)
	if err != nil {
		return err
	}
	if err := backup.Verify(ctx, it); err != nil {
		return err
	}
	fmt.Println("ok:", describe(it))
	return nil
}

func restore(ctx context.Context, k *core.Core, args []string) error {
	if len(args) < 1 {
		usage()
	}
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	id := fs.String("id", "", "snapshot ID (repositories)")
	distro := fs.String("distro", "", "new distribution name")
	location := fs.String("location", "", "folder for the new distribution's disk")
	vol := fs.String("volume", "", "new volume name")
	fs.Parse(args[1:])
	it, err := pick(args[0], *id)
	if err != nil {
		return err
	}
	switch {
	case *distro != "" && *location != "":
		return k.Backup.RestoreAsDistro(ctx, it, *distro, *location)
	case *vol != "":
		return k.Backup.RestoreToVolume(ctx, it, *vol, volume.ImportOptions{Create: true})
	}
	return errors.New("restore needs --distro and --location, or --volume (restoring over existing objects is done in the app, which backs them up first)")
}

func plans(k *core.Core) error {
	ps, err := k.Scheduler.Plans()
	if err != nil {
		return err
	}
	table("ID\tNAME\tENABLED\tEVERY\tNEXT\tLAST", func(w *tabwriter.Writer) {
		for _, p := range ps {
			st := k.Scheduler.State(p.ID)
			fmt.Fprintf(w, "%s\t%s\t%v\t%s\t%s\t%s\n", p.ID, p.Name, p.Enabled, p.Schedule.Every, st.NextRun.Local().Format("2006-01-02 15:04"), st.LastResult)
		}
	})
	return nil
}

func runPlan(ctx context.Context, k *core.Core, args []string) error {
	if len(args) != 1 {
		usage()
	}
	ps, err := k.Scheduler.Plans()
	if err != nil {
		return err
	}
	for _, p := range ps {
		if p.ID == args[0] {
			r := k.Scheduler.Run(ctx, p)
			fmt.Println(r.Result)
			if r.Result != "ok" {
				return errors.New("the plan failed")
			}
			return nil
		}
	}
	return fmt.Errorf("no plan %s", args[0])
}
