package compose

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
)

// testID is a valid project ID for tests.
const testID = "0123456789abcdef"

// metaFor returns project metadata for absolute files.
func metaFor(t *testing.T, name string, files ...string) *Project {
	t.Helper()
	for i, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			t.Fatal(err)
		}
		files[i] = abs
	}
	return &Project{SchemaVersion: ProjectSchema, ID: testID, Name: name, Backend: BackendBuiltin, Files: files}
}

// loadInline writes a Compose file to a temp dir and loads it.
func loadInline(t *testing.T, yaml string) (*types.Project, *Project) {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(f, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := metaFor(t, "demo", f)
	p, err := Load(context.Background(), meta)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return p, meta
}

func TestLoadMergeInterpolateExtendsInclude(t *testing.T) {
	meta := metaFor(t, "demo", "testdata/load/compose.yaml", "testdata/load/compose.override.yaml")
	t.Setenv("WSLPILOT_TEST_SHELL", "from-shell")
	p, err := Load(context.Background(), meta)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "demo" {
		t.Errorf("name = %q, metadata name must win", p.Name)
	}
	web := p.Services["web"]
	if web.Image != "alpine:3.20" {
		t.Errorf("image = %q, want .env interpolation", web.Image)
	}
	env := func(k string) string {
		if v := web.Environment[k]; v != nil {
			return *v
		}
		return "<nil>"
	}
	if env("GREETING") != "from-dotenv" || env("OVERRIDDEN") != "yes" {
		t.Errorf("environment = %v", web.Environment)
	}
	if env("FROM_SHELL") != "from-shell" {
		t.Errorf("FROM_SHELL = %q, the shell must override .env", env("FROM_SHELL"))
	}
	if len(web.Ports) != 2 {
		t.Errorf("ports = %v, want both files merged", web.Ports)
	}
	if len(web.Command) != 2 || web.Command[0] != "sleep" || web.Labels["from.base"] != "1" {
		t.Errorf("extends not applied: command %v labels %v", web.Command, web.Labels)
	}
	if _, ok := p.Services["sidecar"]; !ok {
		t.Error("include not applied")
	}
	if _, ok := p.Services["debug"]; ok {
		t.Error("profile service enabled without its profile")
	}
	if len(p.ComposeFiles) != 2 {
		t.Errorf("compose files = %v", p.ComposeFiles)
	}
}

func TestLoadProfiles(t *testing.T) {
	meta := metaFor(t, "demo", "testdata/load/compose.yaml")
	meta.Profiles = []string{"debug"}
	p, err := Load(context.Background(), meta)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Services["debug"]; !ok {
		t.Error("debug profile not enabled")
	}
}

func TestLoadEnvFiles(t *testing.T) {
	dir := t.TempDir()
	envf := filepath.Join(dir, "custom.env")
	if err := os.WriteFile(envf, []byte("ALPINE_TAG=3.19\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := metaFor(t, "demo", "testdata/load/compose.yaml")
	meta.EnvFiles = []string{envf}
	p, err := Load(context.Background(), meta)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Services["web"].Image; got != "alpine:3.19" {
		t.Errorf("image = %q, want the explicit env file instead of .env", got)
	}
}
