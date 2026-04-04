package updater

import (
	"runtime"
	"testing"
)

func TestAssetName(t *testing.T) {
	name := AssetName()
	if name == "" {
		t.Fatal("AssetName returned empty string")
	}
	want := "claumon-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if name != want {
		t.Errorf("AssetName() = %q, want %q", name, want)
	}
}

func TestNeedsUpdate(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v0.7.1", "v0.7.1", false},
		{"v0.7.1", "v0.8.0", true},
		{"0.7.1", "v0.7.1", false},
		{"dev", "v0.8.0", false},
		{"v0.7.0", "v0.7.1", true},
	}
	for _, tt := range tests {
		got := NeedsUpdate(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("NeedsUpdate(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}
