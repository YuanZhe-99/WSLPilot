package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/YuanZhe-99/WSLPilot/internal/configdoc"
	"github.com/YuanZhe-99/WSLPilot/internal/volume"
)

// DataMove is the outcome of MoveWSLcData.
type DataMove struct {
	Images  int
	Volumes int
	// Containers lists containers that existed in the old storage; WSLc
	// does not move them and they must be created again (Compose: Up).
	Containers []string
	// Staging is the folder holding the exported images and volumes; it is
	// kept so nothing is lost if a step failed.
	Staging string
}

// MoveWSLcData moves WSL Containers storage to a new folder the only way
// WSLc allows: images and volumes are exported, session.storagePath is set
// in settings.yaml, the session restarts (empty, in the new place), and the
// images and volumes are imported again.
// Inputs: dest is the new storagePath; settingsPath is settings.yaml;
// staging is a folder with room for every image and volume.
// Side effects: stops every container (session restart); writes
// settings.yaml (backed up first). The old storage is left untouched by
// WSLc (Microsoft Learn, "WSL container settings"), so a failure loses
// nothing; the caller tells the user where the old data is.
func (k *Core) MoveWSLcData(ctx context.Context, dest, settingsPath, staging string, log func(string)) (DataMove, error) {
	res := DataMove{Staging: staging}
	if !filepath.IsAbs(dest) {
		return res, errors.New("the new folder must be an absolute path")
	}
	if err := os.MkdirAll(filepath.Join(staging, "volumes"), 0o755); err != nil {
		return res, err
	}
	// 1. Inventory.
	cs, err := k.WSLc.Containers(ctx)
	if err != nil {
		return res, err
	}
	for _, c := range cs {
		res.Containers = append(res.Containers, c.Names)
	}
	imgs, err := k.WSLc.Images(ctx)
	if err != nil {
		return res, err
	}
	var refs []string
	for _, im := range imgs {
		if im.Repository != "" && im.Repository != "<none>" {
			refs = append(refs, im.Ref())
		}
	}
	vols, err := k.WSLc.Volumes(ctx)
	if err != nil {
		return res, err
	}
	// 2. Export. Containers are stopped so volume copies are consistent.
	var running []string
	for _, c := range cs {
		if c.Running() {
			running = append(running, c.ID)
		}
	}
	if len(running) > 0 {
		log("Stopping containers")
		if err := k.WSLc.Stop(ctx, -1, running...); err != nil {
			return res, err
		}
	}
	imageFile := filepath.Join(staging, "images.tar")
	if len(refs) > 0 {
		log("Saving " + strconv.Itoa(len(refs)) + " images")
		if err := k.WSLc.Save(ctx, imageFile, refs...); err != nil {
			return res, fmt.Errorf("save images: %w", err)
		}
	}
	for _, v := range vols {
		if v.Labels[volume.HelperLabel] == "1" {
			continue
		}
		log("Exporting volume " + v.Name)
		f, err := os.Create(volumeFile(staging, v.Name))
		if err != nil {
			return res, err
		}
		_, err = k.Volumes.Export(ctx, v.Name, f)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return res, fmt.Errorf("export volume %s: %w", v.Name, err)
		}
	}
	// 3. Point the session at the new folder.
	snap, err := configdoc.ReadFile(settingsPath)
	if err != nil {
		return res, err
	}
	doc := configdoc.ParseYAML(snap.Text)
	doc.Set("session.storagePath", configdoc.QuoteYAML(dest))
	if err := doc.Validate(); err != nil {
		return res, err
	}
	if _, err := snap.Save(doc.String()); err != nil {
		return res, err
	}
	log("Restarting the container session in " + dest)
	if err := k.WSLc.TerminateSession(ctx); err != nil {
		return res, err
	}
	// 4. Import into the new storage.
	if len(refs) > 0 {
		log("Loading images")
		if err := k.WSLc.Load(ctx, imageFile, nil); err != nil {
			return res, fmt.Errorf("load images: %w", err)
		}
		res.Images = len(refs)
	}
	for _, v := range vols {
		if v.Labels[volume.HelperLabel] == "1" {
			continue
		}
		log("Importing volume " + v.Name)
		f, err := os.Open(volumeFile(staging, v.Name))
		if err != nil {
			return res, err
		}
		o := volume.ImportOptions{Create: true, Driver: v.Driver, Labels: v.Labels}
		if v.Driver == "vhd" {
			o.Size, _ = strconv.ParseInt(v.Options["SizeBytes"], 10, 64)
		}
		_, _, err = k.Volumes.Import(ctx, v.Name, f, o)
		f.Close()
		if err != nil {
			return res, fmt.Errorf("import volume %s: %w", v.Name, err)
		}
		res.Volumes++
	}
	return res, nil
}

// volumeFile is a volume's tar in the staging folder.
func volumeFile(staging, name string) string {
	safe := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	return filepath.Join(staging, "volumes", safe+".tar")
}
