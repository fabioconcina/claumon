package parser

import "time"

// usage is the token usage Claude Code logs on each assistant line.
type usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	// CacheCreation splits CacheCreationInputTokens by cache lifetime.
	CacheCreation *struct {
		Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation,omitempty"`
}

// cacheCreate1h returns how many of the cache-write tokens used the 1-hour
// lifetime, which costs more than the default 5 minutes. Lines without the
// split count as all 5-minute writes.
func (u *usage) cacheCreate1h() int {
	if u.CacheCreation == nil {
		return 0
	}
	return max(0, min(u.CacheCreation.Ephemeral1hInputTokens, u.CacheCreationInputTokens))
}

// apiResponse is the token usage of one assistant API response.
type apiResponse struct {
	Time          time.Time
	Model         string
	Input         int
	Output        int
	CacheRead     int
	CacheCreate   int // all cache writes
	CacheCreate1h int // the part of CacheCreate written with the 1-hour lifetime
}

// responseLog collects one entry per assistant API response in a session
// file. Claude Code writes a separate JSONL line for each content block of a
// response (text, thinking, each tool_use), and every one of those lines
// repeats the response's full usage, so summing lines overcounts. Lines are
// deduplicated by message ID; the last line wins. Lines without an ID are
// kept as-is.
type responseLog struct {
	entries []apiResponse
	index   map[string]int
}

func (l *responseLog) add(entry *jsonlLine) {
	if entry.Message == nil || entry.Message.Usage == nil {
		return
	}
	u := entry.Message.Usage
	r := apiResponse{
		Time:          entry.Timestamp,
		Model:         entry.Message.Model,
		Input:         u.InputTokens,
		Output:        u.OutputTokens,
		CacheRead:     u.CacheReadInputTokens,
		CacheCreate:   u.CacheCreationInputTokens,
		CacheCreate1h: u.cacheCreate1h(),
	}
	id := entry.Message.ID
	if id == "" {
		l.entries = append(l.entries, r)
		return
	}
	if l.index == nil {
		l.index = map[string]int{}
	}
	if i, ok := l.index[id]; ok {
		l.entries[i] = r
		return
	}
	l.index[id] = len(l.entries)
	l.entries = append(l.entries, r)
}
