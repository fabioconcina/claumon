package pricing

import (
	"math"
	"testing"
)

func TestLoadEmbedded(t *testing.T) {
	table := Load(nil)
	models := table.Models()

	if len(models) == 0 {
		t.Fatal("Load() returned empty pricing table")
	}

	// Verify a known model
	opus, ok := table.Get("claude-opus-4-6")
	if !ok {
		t.Fatal("claude-opus-4-6 not found in pricing table")
	}
	if opus.Input != 5.0 {
		t.Errorf("opus input = %f, want 5.0", opus.Input)
	}
	if opus.Output != 25.0 {
		t.Errorf("opus output = %f, want 25.0", opus.Output)
	}
	if opus.CacheRead != 0.50 {
		t.Errorf("opus cache_read = %f, want 0.50", opus.CacheRead)
	}
}

func TestLoadWithOverrides(t *testing.T) {
	overrides := map[string]ModelPricing{
		"claude-opus-4-6": {Input: 99.0, Output: 199.0, CacheRead: 9.0, CacheWrite5m: 19.0, CacheWrite1h: 29.0},
		"custom-model":    {Input: 1.0, Output: 2.0, CacheRead: 0.1, CacheWrite5m: 0.2, CacheWrite1h: 0.3},
	}

	table := Load(overrides)

	// Override should take precedence
	opus, ok := table.Get("claude-opus-4-6")
	if !ok {
		t.Fatal("claude-opus-4-6 not found")
	}
	if opus.Input != 99.0 {
		t.Errorf("overridden opus input = %f, want 99.0", opus.Input)
	}

	// Custom model should be present
	custom, ok := table.Get("custom-model")
	if !ok {
		t.Fatal("custom-model not found")
	}
	if custom.Input != 1.0 {
		t.Errorf("custom input = %f, want 1.0", custom.Input)
	}

	// Non-overridden model should still be present from embedded
	sonnet, ok := table.Get("claude-sonnet-4-6")
	if !ok {
		t.Fatal("claude-sonnet-4-6 not found")
	}
	if math.Abs(sonnet.Input-3.0) > 0.001 {
		t.Errorf("sonnet input = %f, want 3.0", sonnet.Input)
	}
}

func TestGetMissing(t *testing.T) {
	table := Load(nil)
	_, ok := table.Get("nonexistent-model")
	if ok {
		t.Error("expected ok=false for nonexistent model")
	}
}

func TestMergeInto(t *testing.T) {
	dst := map[string]ModelPricing{
		"a": {Input: 1.0},
		"b": {Input: 2.0},
	}
	src := map[string]ModelPricing{
		"b": {Input: 99.0},
		"c": {Input: 3.0},
	}
	mergeInto(dst, src)

	if dst["a"].Input != 1.0 {
		t.Error("a should be unchanged")
	}
	if dst["b"].Input != 99.0 {
		t.Error("b should be overwritten by src")
	}
	if dst["c"].Input != 3.0 {
		t.Error("c should be added from src")
	}
}
