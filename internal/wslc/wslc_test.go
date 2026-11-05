package wslc

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/YuanZhe-99/WSLPilot/internal/procexec"
)

func TestParseInfo(t *testing.T) {
	b, err := os.ReadFile("testdata/3.0.3.0/info.json")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ParseInfo(procexec.Decode(b))
	if err != nil {
		t.Fatal(err)
	}
	if info.Client.Version != "3.0.3.0" || info.Client.KernelVersion != "6.18.54.1-1" {
		t.Errorf("client %+v", info.Client)
	}
	if info.Server.SessionManagerVersion != "3.0.3" || len(info.Server.Sessions) != 1 {
		t.Errorf("server %+v", info.Server)
	}
}

func TestParseInfoRejects(t *testing.T) {
	for _, in := range []string{"", "Client:\nWSL version: 3.0.3.0", `{"Client":{}}`} {
		if _, err := ParseInfo(in); err == nil {
			t.Errorf("ParseInfo(%q) succeeded", in)
		}
	}
}

func TestHasNativeCompose(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		want    bool
		wantErr bool
	}{
		{"command exists", nil, true, false},
		{"unknown command", &procexec.ExitError{Name: Exe, Code: 1}, false, false},
		{"wslc missing", errors.New("executable file not found"), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{Run: func(context.Context, string, ...string) (string, error) { return "", tt.err }}
			got, err := c.HasNativeCompose(context.Background())
			if got != tt.want || (err != nil) != tt.wantErr {
				t.Errorf("got %v, %v", got, err)
			}
		})
	}
}
