package gui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/capability"
	"github.com/YuanZhe-99/WSLPilot/internal/compose"
	"github.com/YuanZhe-99/WSLPilot/internal/core"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/procexec"
	"github.com/YuanZhe-99/WSLPilot/internal/scheduler"
	"github.com/YuanZhe-99/WSLPilot/internal/store"
	"github.com/YuanZhe-99/WSLPilot/internal/volume"
	"github.com/YuanZhe-99/WSLPilot/internal/wsl"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// fakeHost records what the GUI asks of the system.
type fakeHost struct {
	mu        sync.Mutex
	terminals [][]string
	ops       []WindowOp
	language  string
}

func (h *fakeHost) PickFiles(string, bool, ...string) []string      { return nil }
func (h *fakeHost) PickSave(string, string) string                  { return "" }
func (h *fakeHost) OpenPath(string)                                 {}
func (h *fakeHost) OpenURL(string)                                  {}
func (h *fakeHost) Notify(string, string)                           {}
func (h *fakeHost) SetOpenAtLogin(bool) error                       { return nil }
func (h *fakeHost) OpenAtLogin() bool                               { return false }
func (h *fakeHost) SetTheme(string)                                 {}
func (h *fakeHost) SetLanguage(tag string)                          { h.language = tag }
func (h *fakeHost) Window(op WindowOp)                              { h.mu.Lock(); h.ops = append(h.ops, op); h.mu.Unlock() }
func (h *fakeHost) Maximized() bool                                 { return false }
func (h *fakeHost) UpdatesEnabled() bool                            { return false }
func (h *fakeHost) CheckUpdate(context.Context) (*AppUpdate, error) { return nil, nil }
func (h *fakeHost) Relaunch()                                       {}
func (h *fakeHost) IdleSeconds() float64                            { return 0 }
func (h *fakeHost) Terminal(title string, argv []string) (Term, error) {
	h.mu.Lock()
	h.terminals = append(h.terminals, argv)
	h.mu.Unlock()
	return fakeTerm{}, nil
}

type fakeTerm struct{}

func (fakeTerm) View(c *ui.Context) ui.Element { return ui.Text(c, "terminal") }
func (fakeTerm) Done() bool                    { return false }
func (fakeTerm) Close()                        {}

// fakeCmd answers commands by their argument line.
type fakeCmd struct {
	mu   sync.Mutex
	out  map[string]string
	runs []string
}

func (f *fakeCmd) run(ctx context.Context, name string, args ...string) (string, error) {
	key := strings.Join(args, " ")
	f.mu.Lock()
	f.runs = append(f.runs, name+" "+key)
	out, ok := f.out[key]
	f.mu.Unlock()
	if !ok {
		return "", &procexec.ExitError{Name: name, Args: args, Code: 1}
	}
	return out, nil
}

func (f *fakeCmd) stream(ctx context.Context, s procexec.Spec) error {
	out, err := f.run(ctx, s.Name, s.Args...)
	if s.Stdout != nil {
		io.WriteString(s.Stdout, out)
	}
	return err
}

func (f *fakeCmd) ran(sub string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.runs {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}

func fixture(t *testing.T, path string) string {
	b, err := os.ReadFile("../" + path)
	if err != nil {
		t.Fatal(err)
	}
	return procexec.Decode(b)
}

// testApp builds an App over fakes whose updates run inline.
func testApp(t *testing.T) (*App, *fakeCmd, *fakeHost) {
	cmd := &fakeCmd{out: map[string]string{
		"--list --verbose":         fixture(t, "wsl/testdata/3.0.3.0/list-verbose-utf16.txt"),
		"--list --running --quiet": "Debian\n",
		"--help":                   "--manage --compact",
		"container list --all --no-trunc --format json":      fixture(t, "wslc/testdata/3.0.3.0/containers.jsonl"),
		"image list --no-trunc --format json":                fixture(t, "wslc/testdata/3.0.3.0/images.jsonl"),
		"stats --no-trunc --format json":                     fixture(t, "wslc/testdata/3.0.3.0/stats.jsonl"),
		"volume list --quiet":                                "wslpilot-test-vol\nwslpilot-test-vhd\n",
		"volume inspect wslpilot-test-vol wslpilot-test-vhd": fixture(t, "wslc/testdata/3.0.3.0/v-inspect.json"),
		"network list --quiet --no-trunc":                    "",
		"--terminate Debian":                                 "",
		"--manage Debian --compact":                          "",
	}}
	cmd.out["container inspect bd5a5bc23b669c7023f4488cda09caa68cb4c14791e40da43eeaf10ff7e82652"] = fixture(t, "wslc/testdata/3.0.3.0/c-inspect.json")
	w := &wsl.Client{Run: cmd.run, Stream: cmd.stream, ReadRegistry: func() []wsl.RegEntry { return nil }}
	c := &wslc.Client{Run: cmd.run, Stream: cmd.stream}
	st := &store.Store{Dir: t.TempDir()}
	k := &core.Core{WSL: w, WSLc: c, Store: st, Jobs: &jobs.Manager{AuditPath: st.Path("audit.jsonl")},
		Volumes: &volume.Pipeline{E: c}, Prober: &capability.Prober{WSL: w, WSLc: c}}
	k.Backup = &backup.Runner{WSL: w, WSLc: c, Vol: k.Volumes}
	k.Scheduler = &scheduler.Service{Store: st, Do: k.BackupTarget}
	k.WireCompose(false)
	h := &fakeHost{}
	var a *App
	// Updates queue up and run on the test goroutine (drain), as
	// Window.Update runs them on the UI thread. The closure keeps this test's
	// channel: work left over from an earlier test must not read the
	// variable while this one replaces it (race detector).
	ch := make(chan func(), 1024)
	pending = ch
	a = New(k, h, func(fn func()) { ch <- fn })
	// Background work writes into the store; it must end before the store's
	// temporary folder is removed (cleanups run last first).
	t.Cleanup(func() {
		done := make(chan struct{})
		go func() { a.bg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})
	a.probe = func(context.Context) capability.Report {
		return capability.Report{WSL: wsl.Version{WSL: "3.0.3.0", Kernel: "6.18.54.1-1"}, Distros: []string{"Debian"}, WSLcErr: errors.New("not installed")}
	}
	return a, cmd, h
}

// pending holds UI updates from background work of the current test app.
var pending chan func()

// drain applies the queued updates.
func drain() {
	for {
		select {
		case fn := <-pending:
			fn()
		default:
			return
		}
	}
}

// settle draws frames until cond holds (background work applies inline).
func settle(t *testing.T, tt *ui.Tester, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		drain()
		tt.Frame()
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not reached; texts %q", tt.Texts())
}

func TestDashboardShowsProbe(t *testing.T) {
	a, _, _ := testApp(t)
	tt := ui.NewTester(a.View, 1200, 780)
	if !tt.HasText(L.DashChecking()) {
		t.Fatalf("before a probe: texts %q", tt.Texts())
	}
	a.Refresh()
	settle(t, tt, func() bool { return a.wsl.loaded && tt.HasText(L.DashDistros(len(a.wsl.distros))) })
	for _, want := range []string{L.DashReady(), L.DashNotFound(), L.DashComposeBuiltin()} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestAutoInstallDue(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 11, 5, h, 30, 0, 0, time.Local) }
	for _, c := range []struct {
		mode    string
		hour    int
		now     time.Time
		idle    time.Duration
		lastTry time.Time
		want    bool
	}{
		{"", 3, at(3), time.Hour, time.Time{}, false},
		{autoInstallIdle, 0, at(9), 20 * time.Minute, time.Time{}, true},
		{autoInstallIdle, 0, at(9), 5 * time.Minute, time.Time{}, false},
		{autoInstallIdle, 0, at(9), 0, time.Time{}, false}, // idle not reported
		{autoInstallDaily, 3, at(3), 0, time.Time{}, true},
		{autoInstallDaily, 3, at(4), 0, time.Time{}, false},
		{autoInstallDaily, 3, at(3), 0, at(3).Add(-time.Hour), false}, // retry waits
		{autoInstallDaily, 3, at(3), 0, at(3).Add(-7 * time.Hour), true},
	} {
		if got := autoInstallDue(c.mode, c.hour, c.now, c.idle, c.lastTry); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}

func TestRefreshRunsOneProbeAtATime(t *testing.T) {
	a, _, _ := testApp(t)
	calls := 0
	block := make(chan struct{})
	a.probe = func(context.Context) capability.Report { calls++; <-block; return capability.Report{} }
	a.Refresh()
	a.Refresh() // ignored: the first probe has not reported yet
	close(block)
	for a.probing {
		drain()
		time.Sleep(time.Millisecond)
	}
	if calls != 1 {
		t.Errorf("calls %d", calls)
	}
}

func TestWSLPageListsAndCompacts(t *testing.T) {
	a, cmd, _ := testApp(t)
	tt := ui.NewTester(a.View, 1300, 820)
	if err := tt.Click(L.NavWSL()); err != nil {
		t.Fatal(err)
	}
	settle(t, tt, func() bool {
		return tt.HasText("Debian") && tt.HasText(L.WSLDefault()) && tt.HasText("wslpilot-test-d1")
	})
	a.selectDistro("Debian")
	settle(t, tt, func() bool { return tt.HasText(L.WSLCompactOpen()) })
	if err := tt.Click(L.WSLCompactOpen()); err != nil {
		t.Fatal(err)
	}
	// The panel lists the steps; a backup first is on by default.
	settle(t, tt, func() bool { return tt.HasText("2. " + L.WSLCompactStep2Official("Debian")) })
	if !a.wsl.compactBackup {
		t.Error("backup first is off by default")
	}
	a.wsl.compactOpen = false
	a.runCompact(a.wsl.distros[0], true, false)
	settle(t, tt, func() bool { return cmd.ran("--manage Debian --compact") && a.k.Jobs.Active() == 0 })
}

func TestUnregisterNeedsTypedName(t *testing.T) {
	a, cmd, _ := testApp(t)
	tt := ui.NewTester(a.View, 1300, 1600)
	tt.Click(L.NavWSL())
	settle(t, tt, func() bool { return len(a.wsl.distros) == 2 })
	a.wsl.sel = 1
	settle(t, tt, func() bool { return tt.HasText(L.WSLUnregister()) })
	if err := tt.Click(L.WSLUnregister()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.confirm.open || a.confirm.typeName != "wslpilot-test-d1" || !a.confirm.backup {
		t.Fatalf("confirm %+v", a.confirm)
	}
	// The action stays disabled until the name is typed.
	tt.Click(L.WSLUnregister())
	tt.Frame()
	if cmd.ran("--unregister") {
		t.Fatal("unregistered without the typed name")
	}
}

func TestContainersPage(t *testing.T) {
	a, _, h := testApp(t)
	a.report = &capability.Report{WSLc: wslc.Info{}}
	a.report.WSLc.Client.Version = "3.0.3.0"
	tt := ui.NewTester(a.View, 1300, 820)
	tt.Click(L.NavWSLc())
	settle(t, tt, func() bool { return tt.HasText("wslpilot-test-c1") })
	a.ctr.csel = 0
	settle(t, tt, func() bool { return tt.HasText(L.CtrShell()) })
	tt.Click(L.CtrShell())
	tt.Frame()
	if len(h.terminals) != 1 || h.terminals[0][0] != "wslc.exe" || a.page != PageTerminal {
		t.Errorf("terminals %q page %s", h.terminals, a.page)
	}
}

func TestVolumesAndBackupsPages(t *testing.T) {
	a, _, _ := testApp(t)
	tt := ui.NewTester(a.View, 1300, 820)
	tt.Click(L.NavVolumes())
	settle(t, tt, func() bool { return tt.HasText("wslpilot-test-vhd") })
	tt.Click(L.NavBackups())
	settle(t, tt, func() bool { return a.bk.loaded })
	if !tt.HasText(L.BkChooseFolder()) {
		t.Errorf("restore view: %q", tt.Texts())
	}
	tt.Click(L.BkPlans())
	tt.Frame()
	tt.Click(L.BkNewPlan())
	settle(t, tt, func() bool { return a.bk.plan.options != nil })
	if a.bk.dlg != "plan" {
		t.Errorf("plan dialog not open")
	}
}

func TestComposePageAddsProject(t *testing.T) {
	a, _, _ := testApp(t)
	dir := t.TempDir()
	file := dir + `\compose.yaml`
	os.WriteFile(file, []byte("services:\n  web:\n    image: alpine:3.20\n    privileged: true\n"), 0o644)
	p, err := compose.NewProject("wslpilot-test-ui", file)
	if err != nil {
		t.Fatal(err)
	}
	a.k.Projects.Save(p)
	tt := ui.NewTester(a.View, 1300, 820)
	tt.Click(L.NavCompose())
	settle(t, tt, func() bool { return tt.HasText("wslpilot-test-ui") })
	a.cmp.sel = 0
	// Unsupported keys are reported as errors, never ignored.
	settle(t, tt, func() bool {
		for _, s := range tt.Texts() {
			if strings.Contains(s, "privileged") {
				return true
			}
		}
		return false
	})
}

func TestSidebarNavigatesEveryPage(t *testing.T) {
	a, _, _ := testApp(t)
	tt := ui.NewTester(a.View, 1300, 820)
	for _, p := range []string{L.NavWSL(), L.NavWSLc(), L.NavCompose(), L.NavVolumes(), L.NavBackups(), L.NavTerminal(), L.NavTasks(), L.NavDashboard()} {
		if err := tt.Click(p); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		tt.Frame()
		tt.Frame()
	}
	if a.page != PageDashboard {
		t.Errorf("page %s", a.page)
	}
}

func TestFriendlyError(t *testing.T) {
	err := &procexec.ExitError{Name: "wsl.exe", Args: []string{"--unregister", "x"}, Code: 1, Stderr: "There is no distribution with the supplied name.\nError code: Wsl/Service/WSL_E_DISTRO_NOT_FOUND"}
	if got := friendlyError(err); got != "There is no distribution with the supplied name." {
		t.Errorf("got %q", got)
	}
}

func TestRunSpecParsesDialog(t *testing.T) {
	s := runSpec(ctrInput{image: "nginx", ports: "8080:80, 127.0.0.1:53:53/udp", env: "A=1\n\nB=2", volumes: `C:\My Data:/data:ro` + "\nweb:/srv", restart: "always"})
	if len(s.Ports) != 2 || s.Ports[1].Protocol != "udp" || s.Ports[1].HostIP != "127.0.0.1" || len(s.Env) != 2 {
		t.Errorf("spec %+v", s)
	}
	if len(s.Mounts) != 2 || s.Mounts[0].Type != "bind" || s.Mounts[0].Source != `C:\My Data` || !s.Mounts[0].ReadOnly || s.Mounts[1].Type != "volume" {
		t.Errorf("mounts %+v", s.Mounts)
	}
	if s.Labels["io.wslpilot.restart"] != "always" {
		t.Errorf("labels %v", s.Labels)
	}
}

func TestContainersSessionSwitch(t *testing.T) {
	a, cmd, _ := testApp(t)
	cmd.out["info --format json"] = fixture(t, "wslc/testdata/3.0.3.0/info-two-sessions.json")
	cmd.out["--session wslpilot-test-sess container list --all --no-trunc --format json"] = ""
	tt := ui.NewTester(a.View, 1300, 820)
	tt.Click(L.NavWSLc())
	settle(t, tt, func() bool { return len(a.ctr.sessions) == 2 && a.ctr.choice == "wslc-cli-user" })
	a.ctr.choice = "wslpilot-test-sess"
	a.selectSession(a.ctr.choice)
	settle(t, tt, func() bool {
		return cmd.ran("--session wslpilot-test-sess container list") && tt.HasText(L.CtrOtherSession("wslpilot-test-sess"))
	})
	a.selectSession("wslc-cli-user")
	if a.wc() != a.k.WSLc {
		t.Error("default session not restored")
	}
}

func TestSettingsSheet(t *testing.T) {
	a, _, _ := testApp(t)
	tt := ui.NewTester(a.View, 1300, 820)
	a.openSettings(setAbout)
	settle(t, tt, func() bool { return tt.HasText(L.SetAboutUpdates()) && tt.HasText(L.SetLicenses()) })
	// A development build cannot update itself and says so.
	if !tt.HasText(L.UpdUnavailable()) {
		t.Errorf("texts %q", tt.Texts())
	}
	a.settingsQ = L.SetCloseWindow()
	settle(t, tt, func() bool { return len(searchSettings(settingsHits(), a.settingsQ)) > 0 })
}

// Regression: the dashboard loads projects and volumes first, which used to
// skip the pages' own setup, so Compose opened the first project and the
// import panel started on "replace".
func TestPagesLoadedByDashboardStartUnselected(t *testing.T) {
	a, _, _ := testApp(t)
	dir := t.TempDir()
	os.WriteFile(dir+`\compose.yaml`, []byte("services:\n  web:\n    image: alpine:3.20\n"), 0o644)
	p, err := compose.NewProject("wslpilot-test-ui", dir+`\compose.yaml`)
	if err != nil {
		t.Fatal(err)
	}
	a.k.Projects.Save(p)
	a.report = &capability.Report{WSL: wsl.Version{WSL: "3.0.3.0"}}
	a.report.WSLc.Client.Version = "3.0.3.0"
	tt := ui.NewTester(a.View, 1300, 820)
	settle(t, tt, func() bool { return a.cmp.loaded && a.vol.loaded })
	a.page = PageCompose
	tt.Frame()
	if a.cmp.sel != -1 || a.vol.sel != -1 || !a.vol.imp.newVolume || !a.vol.imp.check {
		t.Errorf("compose sel %d, volume sel %d, import %+v", a.cmp.sel, a.vol.sel, a.vol.imp)
	}
}

// Regression: the appearance and close segments listened for OnClick, which
// a segmented control never fires, so choosing one saved nothing.
func TestSettingsSegmentsSave(t *testing.T) {
	a, _, _ := testApp(t)
	tt := ui.NewTester(a.View, 1300, 820)
	a.openSettings(setGeneral)
	settle(t, tt, func() bool { return tt.HasText(L.SetThemeLight()) })
	if err := tt.Click(L.SetThemeDark()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(L.SetCloseQuit()); err != nil {
		t.Fatal(err)
	}
	settle(t, tt, func() bool { return a.Prefs().Theme == "dark" && a.Prefs().CloseQuits })
}

// WSLPilot draws its own window buttons on Windows; each reaches the host.
func TestWindowButtons(t *testing.T) {
	if !ownWindowButtons {
		t.Skip("the system draws the window buttons here")
	}
	a, _, h := testApp(t)
	tt := ui.NewTester(a.View, 1300, 820)
	tt.Frame()
	for _, label := range []string{L.WindowMinimize(), L.WindowMaximize(), L.WindowClose()} {
		if err := tt.Click(label); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	want := []WindowOp{WindowMinimize, WindowToggleMaximize, WindowClose}
	if fmt.Sprint(h.ops) != fmt.Sprint(want) {
		t.Errorf("ops %v, want %v", h.ops, want)
	}
}
