package pricing

import (
	"bytes"
	"os"
	"testing"
)

// TestEmbeddedMatchesRootPricing guards against drift between the repo-root
// pricing.json (served via the GitHub raw URL) and the embedded fallback
// copy. They are synced manually by scripts/update-pricing.sh; this test
// fails if one is edited without the other.
func TestEmbeddedMatchesRootPricing(t *testing.T) {
	root, err := os.ReadFile("../../pricing.json")
	if err != nil {
		t.Fatalf("read root pricing.json: %v", err)
	}
	embedded, err := embeddedFS.ReadFile("embedded.json")
	if err != nil {
		t.Fatalf("read embedded.json: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(root), bytes.TrimSpace(embedded)) {
		t.Fatal("pricing.json and internal/pricing/embedded.json differ; run scripts/update-pricing.sh to sync them")
	}
}
