package api

import (
	"encoding/json"
	"testing"
)

func mustParse(t *testing.T, body string) *UsageResponse {
	t.Helper()
	var raw rawUsageResponse
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return mapUsageResponse(raw, []byte(body))
}

func TestMapUsageResponseScopedLimits(t *testing.T) {
	// Fixture based on a real API response (2026-07): legacy seven_day_*
	// buckets are null, per-model weekly limits live in the limits array.
	body := `{
		"five_hour": {"utilization": 19.0, "resets_at": "2026-07-21T10:10:00+00:00"},
		"seven_day": {"utilization": 17.0, "resets_at": "2026-07-25T16:00:00+00:00"},
		"seven_day_opus": null,
		"seven_day_sonnet": null,
		"seven_day_omelette": null,
		"limits": [
			{"kind": "session", "group": "session", "percent": 19, "resets_at": "2026-07-21T10:10:00+00:00", "scope": null},
			{"kind": "weekly_all", "group": "weekly", "percent": 17, "resets_at": "2026-07-25T16:00:00+00:00", "scope": null},
			{"kind": "weekly_scoped", "group": "weekly", "percent": 21, "resets_at": "2026-07-25T16:00:00+00:00",
			 "scope": {"model": {"id": null, "display_name": "Fable"}, "surface": null}, "is_active": true}
		]
	}`
	u := mustParse(t, body)

	if u.SessionPercent != 19 || u.WeeklyPercent != 17 {
		t.Errorf("top-level pcts = %v/%v, want 19/17", u.SessionPercent, u.WeeklyPercent)
	}
	if u.WeeklyOpusPct != nil || u.WeeklySonnetPct != nil || u.WeeklyDesignPct != nil {
		t.Errorf("legacy per-family pcts should be nil for null buckets")
	}
	if len(u.WeeklyScoped) != 1 {
		t.Fatalf("WeeklyScoped len = %d, want 1", len(u.WeeklyScoped))
	}
	s := u.WeeklyScoped[0]
	if s.Name != "Fable" || s.Percent != 21 || s.ResetAt != "2026-07-25T16:00:00+00:00" {
		t.Errorf("scoped = %+v, want {Fable 21 2026-07-25T16:00:00+00:00}", s)
	}
}

func TestMapUsageResponseScopedLimitsSkipsMalformed(t *testing.T) {
	body := `{
		"limits": [
			{"kind": "weekly_scoped", "percent": 5, "scope": null},
			{"kind": "weekly_scoped", "percent": 6, "scope": {"model": null}},
			{"kind": "weekly_scoped", "percent": 7, "scope": {"model": {"display_name": ""}}},
			{"kind": "weekly_scoped", "percent": 8, "scope": {"model": {"display_name": "Opus"}}},
			{"kind": "weekly_all", "percent": 9, "scope": {"model": {"display_name": "NotScoped"}}}
		]
	}`
	u := mustParse(t, body)

	if len(u.WeeklyScoped) != 1 {
		t.Fatalf("WeeklyScoped len = %d, want 1", len(u.WeeklyScoped))
	}
	if u.WeeklyScoped[0].Name != "Opus" || u.WeeklyScoped[0].Percent != 8 {
		t.Errorf("scoped = %+v, want {Opus 8}", u.WeeklyScoped[0])
	}
	if u.WeeklyScoped[0].ResetAt != "" {
		t.Errorf("ResetAt = %q, want empty for missing resets_at", u.WeeklyScoped[0].ResetAt)
	}
}

func TestMapUsageResponseLegacyBuckets(t *testing.T) {
	// Older API shape: per-family seven_day_* buckets, no limits array.
	body := `{
		"five_hour": {"utilization": 10.0, "resets_at": "2026-07-21T10:00:00+00:00"},
		"seven_day": {"utilization": 20.0, "resets_at": "2026-07-25T16:00:00+00:00"},
		"seven_day_opus": {"utilization": 30.0, "resets_at": "2026-07-25T16:00:00+00:00"},
		"seven_day_sonnet": {"utilization": 40.0},
		"seven_day_omelette": {"utilization": 50.0}
	}`
	u := mustParse(t, body)

	if u.WeeklyOpusPct == nil || *u.WeeklyOpusPct != 30 {
		t.Errorf("WeeklyOpusPct = %v, want 30", u.WeeklyOpusPct)
	}
	if u.WeeklyOpusReset != "2026-07-25T16:00:00+00:00" {
		t.Errorf("WeeklyOpusReset = %q", u.WeeklyOpusReset)
	}
	if u.WeeklySonnetPct == nil || *u.WeeklySonnetPct != 40 {
		t.Errorf("WeeklySonnetPct = %v, want 40", u.WeeklySonnetPct)
	}
	if u.WeeklyDesignPct == nil || *u.WeeklyDesignPct != 50 {
		t.Errorf("WeeklyDesignPct = %v, want 50", u.WeeklyDesignPct)
	}
	if len(u.WeeklyScoped) != 0 {
		t.Errorf("WeeklyScoped = %+v, want empty", u.WeeklyScoped)
	}
}
