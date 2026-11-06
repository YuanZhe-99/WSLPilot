package gui

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/compose"
	"github.com/YuanZhe-99/WSLPilot/internal/core"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// A project that was taken down has no containers, but its volumes still
// hold its data: Remove unused must leave them alone (regression).
func TestRemoveUnusedKeepsProjectVolumes(t *testing.T) {
	a, _, _ := testApp(t)
	a.vol.list = []wslc.Volume{
		{Name: "demo_db", Labels: map[string]string{compose.LabelProject: "demo"}},
		{Name: "gone_db", Labels: map[string]string{compose.LabelProject: "gone"}},
		{Name: "loose"},
	}
	a.cmp.projects = []*compose.Project{{Name: "demo"}}
	if got := a.unusedVolumes(); !slices.Equal(got, []string{"gone_db", "loose"}) {
		t.Fatalf("unused %v", got)
	}
}

// demoProject writes a project with a bind folder inside its folder, a
// named volume and an external volume.
func demoProject(t *testing.T) *compose.Project {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "vc-data"), 0o755)
	os.WriteFile(filepath.Join(dir, "vc-data", "a.txt"), []byte("12345"), 0o644)
	file := filepath.Join(dir, "compose.yaml")
	os.WriteFile(file, []byte(`services:
  web:
    image: alpine:3.20
    ports: ["127.0.0.1:8080:80"]
    volumes:
      - ./vc-data:/data
      - db:/var/lib/db
      - shared:/shared:ro
volumes:
  db: {}
  shared:
    external: true
    name: wslpilot-test-shared
`), 0o644)
	p, err := compose.NewProject("wslpilot-test-data", file)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// ProjectData lists the folders, volumes and external volumes a project
// backup is built from, and what each service mounts.
func TestProjectData(t *testing.T) {
	a, _, _ := testApp(t)
	p := demoProject(t)
	d, err := a.k.ProjectData(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Folders) != 1 || d.Folders[0].Relative != "vc-data" || d.Folders[0].ID != "vc-data" || !slices.Equal(d.Folders[0].Services, []string{"web"}) {
		t.Errorf("folders %+v", d.Folders)
	}
	if len(d.Volumes) != 1 || d.Volumes[0].Name != "wslpilot-test-data_db" || d.Volumes[0].Key != "db" {
		t.Errorf("volumes %+v", d.Volumes)
	}
	if !slices.Equal(d.External, []string{"wslpilot-test-shared"}) {
		t.Errorf("external %v", d.External)
	}
	if _, ok := d.Files["compose.yaml"]; !ok {
		t.Errorf("files %v", d.Files)
	}
	sd := d.Services["web"]
	if !slices.Equal(sd.Ports, []string{"127.0.0.1:8080 → 80"}) || len(sd.Mounts) != 3 || sd.Mounts[0].Display != "./vc-data" || sd.Mounts[0].Kind != core.MountFolder {
		t.Errorf("service %+v", sd)
	}
}

// The project page lists the same data, and its backup dialog says what
// the backup holds and that external volumes are left out.
func TestComposeDataCardAndBackupDialog(t *testing.T) {
	a, _, _ := testApp(t)
	a.k.Projects.Save(demoProject(t))
	tt := ui.NewTester(a.View, 1300, 820)
	tt.Click(L.NavCompose())
	settle(t, tt, func() bool { return tt.HasText("wslpilot-test-data") })
	a.cmp.sel = 0
	settle(t, tt, func() bool { return tt.HasText("./vc-data") && tt.HasText("wslpilot-test-data_db") })
	if !tt.HasText(L.CmpData()) || !tt.HasText(L.DataExternal()) {
		t.Errorf("data card: %q", tt.Texts())
	}
	if err := tt.Click(L.CmpBackUp()); err != nil {
		t.Fatal(err)
	}
	settle(t, tt, func() bool { return tt.HasText(L.CmpBackUpTitle("wslpilot-test-data")) })
	if !tt.HasText(L.CmpBackUpExternal("wslpilot-test-shared")) {
		t.Errorf("backup dialog: %q", tt.Texts())
	}
}

// The language row reaches the host and is saved.
func TestLanguageSetting(t *testing.T) {
	a, _, h := testApp(t)
	tt := ui.NewTester(a.View, 1300, 820)
	a.openSettings(setGeneral)
	settle(t, tt, func() bool { return tt.HasText(L.SetLanguageSystem()) })
	if err := tt.Click(L.SetLanguageSystem()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("简体中文"); err != nil {
		t.Fatal(err)
	}
	settle(t, tt, func() bool { return h.language == "zh-Hans" && a.Prefs().Language == "zh-Hans" })
}

// Deleting a repository version from the Backups page leaves the other
// version restorable; the message says data moves on to later versions.
func TestDeleteRepositoryVersionFromBackups(t *testing.T) {
	a, _, _ := testApp(t)
	ctx := context.Background()
	w := &backup.RepoWriter{Dir: filepath.Join(a.backupDir(), "Repository")}
	for i := 0; i < 2; i++ {
		var b bytes.Buffer
		tw := tar.NewWriter(&b)
		tw.WriteHeader(&tar.Header{Name: "f", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1})
		tw.Write([]byte{byte('a' + i)})
		tw.Close()
		if _, err := w.Write(ctx, &b, backup.Meta{Source: backup.Source{Kind: backup.SourceVolume, Name: "wslpilot-test-del"}, Time: time.Unix(int64(1700000000+i*60), 0), Consistent: true}); err != nil {
			t.Fatal(err)
		}
	}
	tt := ui.NewTester(a.View, 1300, 820)
	tt.Click(L.NavBackups())
	settle(t, tt, func() bool { return a.bk.loaded && len(a.bk.groups) == 1 && len(a.bk.groups[0].Items) == 2 })
	it := a.bk.groups[0].Items[0]
	if !a.canDelete(it) {
		t.Fatal("a repository version in this PC's folder cannot be deleted")
	}
	a.deleteVersion(it)
	settle(t, tt, func() bool { return tt.HasText(L.BkDeleteRepoMessage()) })
	if err := tt.Click(L.Delete()); err != nil {
		t.Fatal(err)
	}
	settle(t, tt, func() bool { return a.bk.loaded && len(a.bk.groups) == 1 && len(a.bk.groups[0].Items) == 1 })
	if err := backup.Verify(ctx, a.bk.groups[0].Items[0]); err != nil {
		t.Fatal(err)
	}
}

// The sidebar folds to its icons and back, and the choice is kept.
func TestSidebarFolds(t *testing.T) {
	a, _, _ := testApp(t)
	tt := ui.NewTester(a.View, 1300, 820)
	tt.Frame()
	if err := tt.Click(L.SidebarFold()); err != nil {
		t.Fatal(err)
	}
	settle(t, tt, func() bool { return tt.HasText(L.SidebarUnfold()) && a.k.LoadPrefs().SidebarRail })
	if err := tt.Click(L.NavVolumes()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.page != PageVolumes {
		t.Errorf("rail did not open Volumes")
	}
	if err := tt.Click(L.SidebarUnfold()); err != nil {
		t.Fatal(err)
	}
	settle(t, tt, func() bool { return tt.HasText(L.SidebarFold()) && !a.k.LoadPrefs().SidebarRail })
}

// Start in the tray is on by default; turning it off makes a start at
// sign-in open the window, and is kept.
func TestLoginToTraySwitch(t *testing.T) {
	a, _, _ := testApp(t)
	if !a.LoginToTray() {
		t.Fatal("a start at sign-in should stay in the tray by default")
	}
	tt := ui.NewTester(a.View, 1300, 820)
	a.openSettings(setGeneral)
	settle(t, tt, func() bool { return tt.HasText(L.SetLoginTrayHelp()) })
	a.settings.openAtLogin = true // the fake host says it is off
	tt.Frame()
	// The switch ends where the row's controls end (Quit, below it), on the
	// line of its help text; its title shares its name, so click by place.
	help, ok1 := tt.Find(L.SetLoginTrayHelp())
	quit, ok2 := tt.Find(L.SetCloseQuit())
	if !ok1 || !ok2 {
		t.Fatal("row not found")
	}
	tt.ClickAt(quit.X+quit.W-16, help.Y)
	settle(t, tt, func() bool { return a.k.LoadPrefs().LoginWindow })
	if a.LoginToTray() {
		t.Error("LoginToTray still on")
	}
}
