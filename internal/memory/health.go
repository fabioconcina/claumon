package memory

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// HealthScore represents the health assessment of a single memory file.
type HealthScore struct {
	Path           string   `json:"path"`
	Project        string   `json:"project"`
	Category       string   `json:"category"`
	FMName         string   `json:"fm_name,omitempty"`
	Overall        int      `json:"overall"`         // 0-100 weighted average
	Freshness      int      `json:"freshness"`       // 0-100 based on age
	Structure      int      `json:"structure"`       // 0-100 frontmatter completeness
	Specificity    int      `json:"specificity"`     // 0-100 concrete entities vs vague prose
	Connectedness  int      `json:"connectedness"`   // 0-100 indexed, cross-referenced
	Suggestions    []string `json:"suggestions"`     // improvement hints
	Prompt         string   `json:"prompt"`          // Claude Code prompt to fix issues
	Grade          string   `json:"grade"`           // A/B/C/D/F
}

// HealthReport is the overall health assessment for all memory files.
type HealthReport struct {
	Scores       []HealthScore `json:"scores"`
	AverageScore int           `json:"average_score"`
	GradeCount   map[string]int `json:"grade_count"`
	CheckedAt    int64         `json:"checked_at"`
}

const (
	weightFreshness     = 0.20
	weightStructure     = 0.25
	weightSpecificity   = 0.40
	weightConnectedness = 0.15
)

// Patterns for specificity scoring.
var (
	codeBlockRegex  = regexp.MustCompile("(?s)```[\\s\\S]*?```")
	inlineCodeRegex = regexp.MustCompile("`[^`]+`")
	whyBlockRegex   = regexp.MustCompile(`(?i)\*\*why[:\*]`)
	howToApplyRegex = regexp.MustCompile(`(?i)\*\*how to apply[:\*]`)

	// "Actionable language" — verbs/phrases that tell Claude what to do or not do.
	actionRegex = regexp.MustCompile(`(?i)\b(always|never|must|don't|do not|prefer|avoid|instead|use|run|add|check|ensure|make sure|default to|before|after|when|if .+ then)\b`)
	// Scoping language — tells Claude *when* a rule applies.
	scopeRegex = regexp.MustCompile(`(?i)\b(when|whenever|if|before|after|during|for .+ projects|in this repo|in this project|for refactors|for PRs|for tests)\b`)
)

// ScoreHealth computes health scores for all memory files.
func ScoreHealth(files []*MemoryFile) *HealthReport {
	report := &HealthReport{
		Scores:     []HealthScore{},
		GradeCount: map[string]int{"A": 0, "B": 0, "C": 0, "D": 0, "F": 0},
		CheckedAt:  time.Now().Unix(),
	}

	if len(files) == 0 {
		return report
	}

	// Build lookup structures for scoring
	indexed := buildIndexedSet(files)
	idxPaths := indexPaths(files)

	var totalScore int
	var scored int

	for _, f := range files {
		// Only score individual memory files (not CLAUDE.md, rules, or MEMORY.md indexes)
		if f.Category != "memory-file" {
			continue
		}

		s := scoreFile(f, indexed, idxPaths)
		report.Scores = append(report.Scores, s)
		report.GradeCount[s.Grade]++
		totalScore += s.Overall
		scored++
	}

	if scored > 0 {
		report.AverageScore = totalScore / scored
	}

	// Sort: worst scores first (actionable view)
	sort.Slice(report.Scores, func(i, j int) bool {
		return report.Scores[i].Overall < report.Scores[j].Overall
	})

	return report
}

func scoreFile(f *MemoryFile, indexed map[string]bool, indexPathMap map[string]string) HealthScore {
	_, _, _, body := parseFrontmatter(f.Content)

	freshness := scoreFreshness(f.ModTime, f.FMType)
	structure := scoreStructure(f)
	specificity := scoreSpecificity(body, f.Content)
	connectedness := scoreConnectedness(f, indexed)

	overall := int(
		weightFreshness*float64(freshness) +
			weightStructure*float64(structure) +
			weightSpecificity*float64(specificity) +
			weightConnectedness*float64(connectedness),
	)

	suggestions := buildSuggestions(f, body, freshness, structure, specificity, connectedness, indexed, indexPathMap)
	prompt := buildHealthPrompt(f, suggestions)

	return HealthScore{
		Path:          f.Path,
		Project:       f.Project,
		Category:      f.Category,
		FMName:        f.FMName,
		Overall:       overall,
		Freshness:     freshness,
		Structure:     structure,
		Specificity:   specificity,
		Connectedness: connectedness,
		Suggestions:   suggestions,
		Prompt:        prompt,
		Grade:         letterGrade(overall),
	}
}

// scoreFreshness returns 100 for recent files, decaying to 0 at 90+ days.
// Only applies to "project" type memories (deadlines, incidents, in-progress work).
// Other types (user, feedback, reference) are durable by nature and score 100.
func scoreFreshness(modTime int64, fmType string) int {
	switch fmType {
	case "user", "feedback", "reference":
		return 100
	}
	// "project" type or unknown/missing type: age matters
	age := time.Since(time.Unix(modTime, 0))
	days := int(age.Hours() / 24)
	if days <= 0 {
		return 100
	}
	if days >= 90 {
		return 0
	}
	return 100 - (days * 100 / 90)
}

// scoreStructure: frontmatter completeness.
func scoreStructure(f *MemoryFile) int {
	score := 0
	// Has frontmatter at all (30 pts)
	if f.FMName != "" || f.FMDescription != "" || f.FMType != "" {
		score += 30
	}
	// Has name (25 pts)
	if f.FMName != "" {
		score += 25
	}
	// Has description (25 pts)
	if f.FMDescription != "" {
		score += 25
	}
	// Has valid type (20 pts)
	switch f.FMType {
	case "user", "feedback", "project", "reference":
		score += 20
	}
	return score
}

// scoreSpecificity measures how clear and actionable a memory is.
// Rewards: clear instructions, reasoning structure, appropriate length,
// and optional technical detail. Does NOT require code to score well.
func scoreSpecificity(body, fullContent string) int {
	if len(strings.TrimSpace(body)) == 0 {
		return 0
	}

	score := 0

	// Actionable language — does it tell Claude what to do? (up to 30 pts)
	actions := len(actionRegex.FindAllString(body, -1))
	actionScore := actions * 8
	if actionScore > 30 {
		actionScore = 30
	}
	score += actionScore

	// Scoping — does it say *when* the rule applies? (up to 15 pts)
	scopes := len(scopeRegex.FindAllString(body, -1))
	scopeScore := scopes * 8
	if scopeScore > 15 {
		scopeScore = 15
	}
	score += scopeScore

	// Structured reasoning: **Why:** and **How to apply:** (up to 20 pts)
	if whyBlockRegex.MatchString(fullContent) {
		score += 10
	}
	if howToApplyRegex.MatchString(fullContent) {
		score += 10
	}

	// Technical detail — code blocks, inline code (bonus, up to 15 pts)
	codeBlocks := len(codeBlockRegex.FindAllString(fullContent, -1))
	inlineCodes := len(inlineCodeRegex.FindAllString(fullContent, -1))
	codeScore := codeBlocks*8 + inlineCodes*3
	if codeScore > 15 {
		codeScore = 15
	}
	score += codeScore

	// Content length: too short is unclear, moderate is good (up to 20 pts)
	wordCount := len(tokenize(body))
	if wordCount >= 10 && wordCount <= 200 {
		score += 20
	} else if wordCount > 5 {
		score += 10
	}

	if score > 100 {
		score = 100
	}
	return score
}

// scoreConnectedness: indexed in MEMORY.md = 100, not indexed = 0.
func scoreConnectedness(f *MemoryFile, indexed map[string]bool) int {
	if indexed[f.Path] {
		return 100
	}
	return 0
}

// buildIndexedSet returns the set of file paths referenced by any MEMORY.md index.
func buildIndexedSet(files []*MemoryFile) map[string]bool {
	indexed := make(map[string]bool)
	for _, f := range files {
		if f.Category != "auto-memory" {
			continue
		}
		dir := filepath.Dir(f.Path)
		for _, href := range extractMarkdownLinks(f.Content) {
			indexed[filepath.Join(dir, href)] = true
		}
	}
	return indexed
}


// indexPaths maps each project to its MEMORY.md path.
func indexPaths(files []*MemoryFile) map[string]string {
	m := make(map[string]string)
	for _, f := range files {
		if f.Category == "auto-memory" {
			m[f.Project] = f.Path
		}
	}
	return m
}

func buildSuggestions(f *MemoryFile, body string, freshness, structure, specificity, connectedness int, indexed map[string]bool, indexPathMap map[string]string) []string {
	var suggestions []string
	base := filepath.Base(f.Path)

	// --- Freshness (only relevant for project-type or untyped memories) ---
	if freshness == 0 {
		suggestions = append(suggestions, fmt.Sprintf("Stale (90+ days) — open %s and verify the content is still accurate, or delete it", base))
	} else if freshness < 50 {
		suggestions = append(suggestions, fmt.Sprintf("Getting stale — review %s to confirm it's still relevant", base))
	}

	// --- Structure: missing frontmatter fields ---
	var missingFM []string
	if f.FMName == "" {
		missingFM = append(missingFM, "name: <short title>")
	}
	if f.FMDescription == "" {
		missingFM = append(missingFM, "description: <one-line summary>")
	}
	if f.FMType == "" {
		missingFM = append(missingFM, "type: <user|feedback|project|reference>")
	}
	if len(missingFM) > 0 {
		hasFM := strings.Contains(f.Content, "---")
		if hasFM {
			suggestions = append(suggestions, fmt.Sprintf("Add missing frontmatter: %s", strings.Join(missingFM, ", ")))
		} else {
			suggestions = append(suggestions, fmt.Sprintf("Add frontmatter block to top of file: ---\\n%s\\n---", strings.Join(missingFM, "\\n")))
		}
	}

	// --- Specificity ---
	wordCount := len(tokenize(body))
	if wordCount < 5 {
		suggestions = append(suggestions, fmt.Sprintf("Only %d words — add enough context for Claude to act on this without guessing", wordCount))
	} else if specificity < 50 {
		if !actionRegex.MatchString(body) {
			suggestions = append(suggestions, "Not clear what to do — use actionable language (always/never/prefer/avoid/use)")
		}
		if !scopeRegex.MatchString(body) {
			suggestions = append(suggestions, "Not clear when this applies — add scoping (when/if/before/after/for X projects)")
		}
	} else if specificity < 85 {
		// Targeted suggestions for decent-but-not-great specificity
		if !actionRegex.MatchString(body) {
			suggestions = append(suggestions, "Could be more direct — add actionable language (always/never/prefer/avoid)")
		} else if !scopeRegex.MatchString(body) {
			suggestions = append(suggestions, "Could clarify when this applies — add scoping (when/if/before/after)")
		}
	}
	if wordCount > 200 {
		suggestions = append(suggestions, fmt.Sprintf("Long (%d words) — consider splitting into focused files, one topic each", wordCount))
	}

	// --- Connectedness ---
	if !indexed[f.Path] {
		if indexPath, ok := indexPathMap[f.Project]; ok {
			indexBase := filepath.Base(indexPath)
			suggestions = append(suggestions, fmt.Sprintf("Not linked — add `- [%s](%s)` to %s", base, base, indexBase))
		} else {
			suggestions = append(suggestions, fmt.Sprintf("Not indexed — create a MEMORY.md with `- [%s](%s)`", base, base))
		}
	}

	// --- Reasoning structure for feedback/project types ---
	if f.FMType == "feedback" || f.FMType == "project" {
		if !whyBlockRegex.MatchString(f.Content) {
			suggestions = append(suggestions, "Add a **Why:** line explaining the reason — helps Claude judge edge cases")
		}
		if !howToApplyRegex.MatchString(f.Content) {
			suggestions = append(suggestions, "Add a **How to apply:** line so Claude knows when this rule kicks in")
		}
	}

	return suggestions
}

func buildHealthPrompt(f *MemoryFile, suggestions []string) string {
	if len(suggestions) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Improve the memory file at %s. ", f.Path))
	sb.WriteString("Read the file, then fix these issues:\n")
	for _, s := range suggestions {
		sb.WriteString(fmt.Sprintf("- %s\n", s))
	}
	sb.WriteString("\nKeep the existing content and meaning intact — just improve structure and clarity. Do not remove information.")
	return sb.String()
}

func letterGrade(score int) string {
	switch {
	case score >= 85:
		return "A"
	case score >= 70:
		return "B"
	case score >= 50:
		return "C"
	case score >= 30:
		return "D"
	default:
		return "F"
	}
}
