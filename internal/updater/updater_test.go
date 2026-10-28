package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestIsHomebrewPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/opt/homebrew/Cellar/claumon/0.21.0/bin/claumon", true},
		{"/usr/local/Cellar/claumon/0.21.0/bin/claumon", true},
		{"/home/linuxbrew/.linuxbrew/Cellar/claumon/0.21.0/bin/claumon", true},
		{"/usr/local/bin/claumon", false},
		{"/home/me/.local/bin/claumon", false},
		{"/opt/homebrew/Cellar/other/1.0/bin/claumon", false},
		{`C:\Users\me\bin\claumon.exe`, false},
	}
	for _, tt := range tests {
		if got := isHomebrewPath(tt.path); got != tt.want {
			t.Errorf("isHomebrewPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestUpdateCommand(t *testing.T) {
	// The test binary is never in a Homebrew Cellar.
	if got := UpdateCommand("systemctl --user restart claumon"); got != "claumon update" {
		t.Errorf("UpdateCommand() = %q, want %q", got, "claumon update")
	}
}

func TestBrewCommand(t *testing.T) {
	tests := []struct {
		restart string
		want    string
	}{
		{"", "brew update && brew upgrade claumon"},
		{"systemctl --user restart claumon", "brew update && brew upgrade claumon && systemctl --user restart claumon"},
	}
	for _, tt := range tests {
		if got := BrewCommand(tt.restart); got != tt.want {
			t.Errorf("BrewCommand(%q) = %q, want %q", tt.restart, got, tt.want)
		}
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

func TestVerifyChecksum(t *testing.T) {
	binary := []byte("claumon binary")
	path := filepath.Join(t.TempDir(), "claumon")
	if err := os.WriteFile(path, binary, 0644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(binary)
	good := hex.EncodeToString(sum[:])
	bad := strings.Repeat("0", len(good))
	asset := "claumon-linux-amd64"

	tests := []struct {
		name      string
		checksums string // "" means the release has no checksums asset
		wantErr   string
	}{
		{name: "match", checksums: good + "  " + asset + "\n"},
		{name: "mismatch", checksums: bad + "  " + asset + "\n", wantErr: "checksum mismatch"},
		{name: "asset not listed", checksums: good + "  claumon-darwin-arm64\n", wantErr: "no checksum found"},
		{name: "no checksums file", wantErr: "no checksums file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := &Release{TagName: "v9.9.9"}
			if tt.checksums != "" {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, tt.checksums)
				}))
				defer srv.Close()
				rel.Assets = append(rel.Assets, struct {
					Name               string `json:"name"`
					BrowserDownloadURL string `json:"browser_download_url"`
				}{Name: "checksums.txt", BrowserDownloadURL: srv.URL})
			}

			err := verifyChecksum(rel, asset, path)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("verifyChecksum() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("verifyChecksum() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}
