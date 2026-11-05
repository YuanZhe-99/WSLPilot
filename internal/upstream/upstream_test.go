package upstream

import "testing"

func TestManifestParses(t *testing.T) {
	m := Tested()
	if len(m.WSL) == 0 || m.MyGo == "" {
		t.Fatalf("incomplete manifest: %+v", m)
	}
	for i := 1; i < len(m.WSL); i++ {
		if Compare(m.WSL[i-1], m.WSL[i]) >= 0 {
			t.Fatalf("wsl versions not ascending: %v", m.WSL)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"3.0.3.0", "3.0.3", 0},
		{"3.0.10", "3.0.9", 1},
		{"2.9.13", "3.0.1", -1},
		{"v0.7.4", "0.7.4", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
