package version

import (
	"encoding/json"
	"os"
	"testing"
)

// TestMatchesMygoJSON keeps Version and mygo.json's version identical, since
// the packaged app and the updater read mygo.json while the code reads Version.
func TestMatchesMygoJSON(t *testing.T) {
	data, err := os.ReadFile("../../mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Version != Version {
		t.Fatalf("mygo.json version %q, internal/version %q", cfg.Version, Version)
	}
}
