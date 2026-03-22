package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type StalenessAlert struct {
	Type       string `json:"type"`                  // "broken-link", "orphaned-file", "index-mismatch"
	Severity   string `json:"severity"`              // "error", "warning"
	Project    string `json:"project"`
	FilePath   string `json:"file_path"`
	TargetPath string `json:"target_path,omitempty"`
	Message    string `json:"message"`
}

type StalenessReport struct {
	Alerts     []StalenessAlert `json:"alerts"`
	TotalFiles int              `json:"total_files"`
	CheckedAt  int64            `json:"checked_at"`
}

// pathIndex builds a set of known file paths for quick lookup.
func pathIndex(files []*MemoryFile) map[string]bool {
	idx := make(map[string]bool, len(files))
	for _, f := range files {
		idx[f.Path] = true
	}
	return idx
}

var mdLinkRegex = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+\.md)\)`)

// extractMarkdownLinks parses markdown link references to .md files from content.
func extractMarkdownLinks(content string) []string {
	matches := mdLinkRegex.FindAllStringSubmatch(content, -1)
	var links []string
	for _, m := range matches {
		links = append(links, m[2])
	}
	return links
}

// CheckStaleness analyzes memory files for broken links, orphaned files, and index mismatches.
func CheckStaleness(files []*MemoryFile) *StalenessReport {
	report := &StalenessReport{
		TotalFiles: len(files),
		CheckedAt:  time.Now().Unix(),
		Alerts:     []StalenessAlert{},
	}

	report.Alerts = append(report.Alerts, checkBrokenLinks(files)...)
	report.Alerts = append(report.Alerts, checkOrphanedFiles(files)...)
	report.Alerts = append(report.Alerts, checkIndexMismatch(files)...)

	return report
}

// checkBrokenLinks finds MEMORY.md index entries that reference non-existent files.
func checkBrokenLinks(files []*MemoryFile) []StalenessAlert {
	knownPaths := pathIndex(files)
	var alerts []StalenessAlert
	for _, f := range files {
		if f.Category != "auto-memory" {
			continue
		}
		dir := filepath.Dir(f.Path)
		for _, href := range extractMarkdownLinks(f.Content) {
			target := filepath.Join(dir, href)
			if knownPaths[target] {
				continue
			}
			// Double-check filesystem in case file exists but wasn't discovered
			if _, err := os.Stat(target); err == nil {
				continue
			}
			alerts = append(alerts, StalenessAlert{
				Type:       "broken-link",
				Severity:   "error",
				Project:    f.Project,
				FilePath:   f.Path,
				TargetPath: target,
				Message:    fmt.Sprintf("MEMORY.md links to \"%s\" which does not exist", href),
			})
		}
	}
	return alerts
}

// checkOrphanedFiles finds memory files not referenced by any MEMORY.md index.
func checkOrphanedFiles(files []*MemoryFile) []StalenessAlert {
	// Collect all files referenced by MEMORY.md indexes
	referenced := make(map[string]bool)
	for _, f := range files {
		if f.Category != "auto-memory" {
			continue
		}
		dir := filepath.Dir(f.Path)
		for _, href := range extractMarkdownLinks(f.Content) {
			referenced[filepath.Join(dir, href)] = true
		}
	}

	var alerts []StalenessAlert
	for _, f := range files {
		if f.Category != "memory-file" {
			continue
		}
		if referenced[f.Path] {
			continue
		}
		alerts = append(alerts, StalenessAlert{
			Type:     "orphaned-file",
			Severity: "warning",
			Project:  f.Project,
			FilePath: f.Path,
			Message:  fmt.Sprintf("Memory file \"%s\" is not referenced in MEMORY.md", filepath.Base(f.Path)),
		})
	}
	return alerts
}

// checkIndexMismatch detects when a MEMORY.md link count doesn't match actual file count.
func checkIndexMismatch(files []*MemoryFile) []StalenessAlert {
	type projInfo struct {
		indexFile   *MemoryFile
		indexLinks  int
		fileCount   int
	}
	projects := make(map[string]*projInfo)

	for _, f := range files {
		key := f.Project
		if projects[key] == nil {
			projects[key] = &projInfo{}
		}
		switch f.Category {
		case "auto-memory":
			projects[key].indexFile = f
			projects[key].indexLinks = len(extractMarkdownLinks(f.Content))
		case "memory-file":
			projects[key].fileCount++
		}
	}

	var alerts []StalenessAlert
	for _, info := range projects {
		if info.indexFile == nil {
			continue
		}
		if info.indexLinks == info.fileCount {
			continue
		}
		alerts = append(alerts, StalenessAlert{
			Type:     "index-mismatch",
			Severity: "warning",
			Project:  info.indexFile.Project,
			FilePath: info.indexFile.Path,
			Message:  fmt.Sprintf("MEMORY.md lists %d links but %d memory files exist", info.indexLinks, info.fileCount),
		})
	}
	return alerts
}
