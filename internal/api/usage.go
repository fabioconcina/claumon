package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/fabioconcina/claumon/internal/auth"
)

// UsageResponse represents the parsed OAuth usage data.
type UsageResponse struct {
	SessionPercent   float64
	WeeklyPercent    float64
	SessionResetAt   string
	WeeklyResetAt    string
	WeeklySonnetPct   *float64
	WeeklySonnetReset string
	WeeklyOpusPct     *float64
	WeeklyOpusReset   string
	ExtraUsageEnabled bool
	ExtraUsageLimit   *float64
	ExtraUsageUsed    *float64
	Raw              json.RawMessage
}

type rawUsageResponse struct {
	FiveHour *struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    *string `json:"resets_at"`
	} `json:"five_hour"`
	SevenDay *struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    *string `json:"resets_at"`
	} `json:"seven_day"`
	SevenDaySonnet *struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    *string `json:"resets_at"`
	} `json:"seven_day_sonnet"`
	SevenDayOpus *struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    *string `json:"resets_at"`
	} `json:"seven_day_opus"`
	ExtraUsage *struct {
		IsEnabled    bool     `json:"is_enabled"`
		MonthlyLimit *float64 `json:"monthly_limit"`
		UsedCredits  *float64 `json:"used_credits"`
		Utilization  *float64 `json:"utilization"`
	} `json:"extra_usage"`
}

type Client struct {
	creds      *auth.Credentials
	httpClient *http.Client
	logOnce    sync.Once
}

func NewClient(creds *auth.Credentials) *Client {
	return &Client{
		creds: creds,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) FetchUsage(ctx context.Context) (*UsageResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.creds.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching usage: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		if retryAfter != "" {
			return nil, fmt.Errorf("usage API rate limited (429), retry after %s", retryAfter)
		}
		return nil, fmt.Errorf("usage API rate limited (429)")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("usage API returned %d: %s", resp.StatusCode, string(body))
	}

	// Log the raw response once for debugging/discovery
	c.logOnce.Do(func() {
		log.Printf("[api] Raw usage response: %s", string(body))
	})

	var raw rawUsageResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parsing usage response: %w", err)
	}

	return mapUsageResponse(raw, body), nil
}

func mapUsageResponse(raw rawUsageResponse, body []byte) *UsageResponse {
	usage := &UsageResponse{
		Raw: json.RawMessage(body),
	}

	if raw.FiveHour != nil {
		usage.SessionPercent = raw.FiveHour.Utilization
		if raw.FiveHour.ResetsAt != nil {
			usage.SessionResetAt = *raw.FiveHour.ResetsAt
		}
	}
	if raw.SevenDay != nil {
		usage.WeeklyPercent = raw.SevenDay.Utilization
		if raw.SevenDay.ResetsAt != nil {
			usage.WeeklyResetAt = *raw.SevenDay.ResetsAt
		}
	}
	if raw.SevenDaySonnet != nil {
		v := raw.SevenDaySonnet.Utilization
		usage.WeeklySonnetPct = &v
		if raw.SevenDaySonnet.ResetsAt != nil {
			usage.WeeklySonnetReset = *raw.SevenDaySonnet.ResetsAt
		}
	}
	if raw.SevenDayOpus != nil {
		v := raw.SevenDayOpus.Utilization
		usage.WeeklyOpusPct = &v
		if raw.SevenDayOpus.ResetsAt != nil {
			usage.WeeklyOpusReset = *raw.SevenDayOpus.ResetsAt
		}
	}
	if raw.ExtraUsage != nil {
		usage.ExtraUsageEnabled = raw.ExtraUsage.IsEnabled
		usage.ExtraUsageLimit = raw.ExtraUsage.MonthlyLimit
		usage.ExtraUsageUsed = raw.ExtraUsage.UsedCredits
	}

	return usage
}

// SessionResetDuration parses the session reset time and returns duration until reset.
func (u *UsageResponse) SessionResetDuration() time.Duration {
	return ParseResetDuration(u.SessionResetAt)
}

// WeeklyResetDuration parses the weekly reset time and returns duration until reset.
func (u *UsageResponse) WeeklyResetDuration() time.Duration {
	return ParseResetDuration(u.WeeklyResetAt)
}

// ParseResetDuration parses an RFC3339 timestamp and returns the duration until that time.
func ParseResetDuration(s string) time.Duration {
	if s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}
	d := time.Until(t)
	if d < 0 {
		return 0
	}
	return d
}
