package memory

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

type MemoryFile struct {
	Path        string `json:"path"`
	Project     string `json:"project"`
	Category    string `json:"category"` // "claude-md", "rules", "auto-memory", "memory-file"
	Content     string `json:"content"`
	HTMLContent string `json:"html_content"`
	ModTime     int64  `json:"mod_time"`

	// Parsed frontmatter fields
	FMName        string `json:"fm_name,omitempty"`
	FMDescription string `json:"fm_description,omitempty"`
	FMType        string `json:"fm_type,omitempty"` // user, feedback, project, reference
}

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(html.WithHardWraps()),
)

var localLinkRe = regexp.MustCompile(`href="([^"]*\.md)"`)

// rewriteLocalLinks converts relative .md links in rendered HTML to vscode:// URLs.
func rewriteLocalLinks(html, dir string) string {
	return localLinkRe.ReplaceAllStringFunc(html, func(match string) string {
		sub := localLinkRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		href := sub[1]
		// Skip already-absolute URLs
		if strings.HasPrefix(href, "http") || strings.HasPrefix(href, "vscode") {
			return match
		}
		abs := filepath.Join(dir, href)
		return `href="vscode://file/` + abs + `"`
	})
}

func renderMarkdown(source string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(source), &buf); err != nil {
		return "<pre>" + source + "</pre>"
	}
	return buf.String()
}

// parseFrontmatter extracts YAML frontmatter fields from markdown content.
// Returns the frontmatter values and the body without the frontmatter block.
func parseFrontmatter(content string) (name, description, fmType, body string) {
	body = content
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---") {
		return
	}

	// Find the closing ---
	rest := trimmed[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return
	}

	fmBlock := rest[:idx]
	body = strings.TrimSpace(rest[idx+4:])

	// Simple line-by-line YAML parsing (no dependency needed)
	for _, line := range strings.Split(fmBlock, "\n") {
		line = strings.TrimSpace(line)
		if colon := strings.Index(line, ":"); colon > 0 {
			key := strings.TrimSpace(line[:colon])
			val := strings.TrimSpace(line[colon+1:])
			// Strip surrounding quotes
			val = strings.Trim(val, "\"'")
			val = strings.TrimSpace(val)
			switch key {
			case "name":
				name = val
			case "description":
				description = val
			case "type":
				fmType = val
			}
		}
	}
	return
}

type trashRecord struct {
	ID                string           `json:"id"`
	OriginalPath      string           `json:"original_path"`
	DeletedAt         string           `json:"deleted_at"`
	IndexPath         string           `json:"index_path,omitempty"`
	RemovedIndexLines []trashIndexLine `json:"removed_index_lines,omitempty"`
}

// trashIndexLine is a MEMORY.md pointer line pruned during a trash operation,
// with its original 0-based line number so a restore can put it back in place.
type trashIndexLine struct {
	Text string `json:"text"`
	Line int    `json:"line"`
}

// trashMu serializes trash operations: the HTTP delete/restore handlers and
// the daily prune goroutine all mutate the same .claumon-trash tree.
var trashMu sync.Mutex

// TrashRetention is how long recoverable memory deletions are kept before the
// daily maintenance pass removes them permanently.
const TrashRetention = 30 * 24 * time.Hour

// TrashFile moves a memory file into claudeDir/.claumon-trash and returns an
// opaque restore ID. It refuses any path that is not among the currently
// discoverable memory files under claudeDir, guarding against path traversal
// and arbitrary file operations through the API.
func TrashFile(claudeDir, path string) (string, error) {
	trashMu.Lock()
	defer trashMu.Unlock()

	files, err := DiscoverAll(claudeDir)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if f.Path == path {
			if f.Category != "memory-file" {
				return "", fmt.Errorf("refusing to delete protected %s file: %s", f.Category, path)
			}

			id := newTrashID()
			recordDir := filepath.Join(trashRoot(claudeDir), id)
			if err := os.MkdirAll(recordDir, 0700); err != nil {
				return "", err
			}

			indexPath := filepath.Join(filepath.Dir(path), "MEMORY.md")
			removedLines, prunedIndex, indexMode := planIndexPrune(indexPath, filepath.Base(path))
			record := trashRecord{
				ID:                id,
				OriginalPath:      path,
				DeletedAt:         time.Now().UTC().Format(time.RFC3339Nano),
				IndexPath:         indexPath,
				RemovedIndexLines: removedLines,
			}
			metadata, err := json.MarshalIndent(record, "", "  ")
			if err != nil {
				_ = os.RemoveAll(recordDir)
				return "", err
			}
			if err := os.WriteFile(filepath.Join(recordDir, "record.json"), metadata, 0600); err != nil {
				_ = os.RemoveAll(recordDir)
				return "", err
			}

			trashedPath := filepath.Join(recordDir, "memory.md")
			if err := os.Rename(path, trashedPath); err != nil {
				_ = os.RemoveAll(recordDir)
				return "", err
			}
			// Best effort: if the index cannot be rewritten, the memory itself is
			// still safely recoverable from trash.
			if prunedIndex != nil {
				_ = os.WriteFile(indexPath, prunedIndex, indexMode)
			}
			return id, nil
		}
	}
	return "", fmt.Errorf("not a known memory file: %s", path)
}

// RestoreFile restores a file previously moved by TrashFile. Trash IDs are
// intentionally opaque and path separators are rejected before touching disk.
func RestoreFile(claudeDir, id string) (string, error) {
	if id == "" || filepath.Base(id) != id || id == "." || id == ".." {
		return "", fmt.Errorf("invalid trash id")
	}
	trashMu.Lock()
	defer trashMu.Unlock()
	recordDir := filepath.Join(trashRoot(claudeDir), id)
	data, err := os.ReadFile(filepath.Join(recordDir, "record.json"))
	if err != nil {
		return "", fmt.Errorf("trash entry not found: %w", err)
	}
	var record trashRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return "", fmt.Errorf("invalid trash entry: %w", err)
	}
	if record.ID != id || !isRestorableMemoryPath(claudeDir, record.OriginalPath) {
		return "", fmt.Errorf("invalid trash entry")
	}
	if _, err := os.Stat(record.OriginalPath); err == nil {
		return "", fmt.Errorf("cannot restore: destination already exists")
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(record.OriginalPath), 0755); err != nil {
		return "", err
	}
	if err := os.Rename(filepath.Join(recordDir, "memory.md"), record.OriginalPath); err != nil {
		return "", err
	}
	restoreIndexLines(record.IndexPath, record.RemovedIndexLines)
	_ = os.RemoveAll(recordDir)
	return record.OriginalPath, nil
}

func trashRoot(claudeDir string) string {
	return filepath.Join(claudeDir, ".claumon-trash")
}

// PruneTrash permanently removes recoverable deletions that are at least 30
// days old. Malformed entries are preserved so a cleanup bug cannot destroy
// files whose retention age cannot be established safely.
func PruneTrash(claudeDir string, now time.Time) (int, error) {
	trashMu.Lock()
	defer trashMu.Unlock()

	root := trashRoot(claudeDir)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	cutoff := now.Add(-TrashRetention)
	removed := 0
	var pruneErr error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		recordDir := filepath.Join(root, entry.Name())
		data, err := os.ReadFile(filepath.Join(recordDir, "record.json"))
		if err != nil {
			continue
		}
		var record trashRecord
		if err := json.Unmarshal(data, &record); err != nil || record.ID != entry.Name() {
			continue
		}
		deletedAt, err := time.Parse(time.RFC3339Nano, record.DeletedAt)
		if err != nil || deletedAt.After(cutoff) {
			continue
		}
		if err := os.RemoveAll(recordDir); err != nil {
			if pruneErr == nil {
				pruneErr = fmt.Errorf("removing trash entry %s: %w", entry.Name(), err)
			}
			continue
		}
		removed++
	}
	return removed, pruneErr
}

func newTrashID() string {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random)
}

func isRestorableMemoryPath(claudeDir, path string) bool {
	rel, err := filepath.Rel(claudeDir, path)
	if err != nil || rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	return len(parts) == 4 &&
		strings.EqualFold(parts[0], "projects") &&
		strings.EqualFold(parts[2], "memory") &&
		isMemoryFileName(parts[3])
}

// isMemoryFileName reports whether a bare filename counts as an individual
// project memory file: any .md except the protected MEMORY.md index. This is
// the single definition shared by discovery and restore validation.
func isMemoryFileName(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".md") && !strings.EqualFold(name, "MEMORY.md")
}

// planIndexPrune returns a rewritten MEMORY.md and the lines removed from it,
// each tagged with its original position so a restore can reinsert in place.
// A missing or unreadable index produces nil output and does not block trashing.
func planIndexPrune(indexPath, basename string) ([]trashIndexLine, []byte, os.FileMode) {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, nil, 0
	}
	linkRef := "](" + basename + ")"
	lines := strings.Split(string(data), "\n")
	kept := make([]string, 0, len(lines))
	var removed []trashIndexLine
	for i, line := range lines {
		if strings.Contains(line, linkRef) {
			removed = append(removed, trashIndexLine{Text: line, Line: i})
			continue
		}
		kept = append(kept, line)
	}
	if len(removed) == 0 {
		return nil, nil, 0
	}
	info, err := os.Stat(indexPath)
	if err != nil {
		return nil, nil, 0
	}
	return removed, []byte(strings.Join(kept, "\n")), info.Mode().Perm()
}

func restoreIndexLines(indexPath string, removed []trashIndexLine) {
	if indexPath == "" || len(removed) == 0 {
		return
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, rem := range removed {
		if containsLine(lines, rem.Text) {
			continue
		}
		// Clamp to the end of the file, but keep a trailing newline (an empty
		// final element) last. Removed lines arrive in ascending original
		// order, so reinserting at their recorded positions reconstructs the
		// pre-deletion layout when the index was not edited in between.
		end := len(lines)
		if end > 0 && lines[end-1] == "" {
			end--
		}
		pos := rem.Line
		if pos > end {
			pos = end
		}
		lines = append(lines, "")
		copy(lines[pos+1:], lines[pos:])
		lines[pos] = rem.Text
	}
	info, err := os.Stat(indexPath)
	if err != nil {
		return
	}
	_ = os.WriteFile(indexPath, []byte(strings.Join(lines, "\n")), info.Mode().Perm())
}

func containsLine(lines []string, target string) bool {
	for _, l := range lines {
		if l == target {
			return true
		}
	}
	return false
}

func DiscoverAll(claudeDir string) ([]*MemoryFile, error) {
	var files []*MemoryFile

	// 1. Global CLAUDE.md
	if mf := readMemoryFile(filepath.Join(claudeDir, "CLAUDE.md"), "", "claude-md"); mf != nil {
		files = append(files, mf)
	}

	// 2. Global rules
	files = append(files, globMarkdownFiles(filepath.Join(claudeDir, "rules"), "", "rules")...)

	// 3. Per-project memories
	projectsDir := filepath.Join(claudeDir, "projects")
	projEntries, err := os.ReadDir(projectsDir)
	if err != nil {
		return files, nil
	}

	for _, projEntry := range projEntries {
		if !projEntry.IsDir() {
			continue
		}
		projName := DecodePath(projEntry.Name())
		projDir := filepath.Join(projectsDir, projEntry.Name())
		files = append(files, discoverProjectMemories(projDir, projName)...)
	}

	return files, nil
}

// globMarkdownFiles reads every *.md file in dir as a memory of the given category/project.
// Missing directories return nil, not an error.
func globMarkdownFiles(dir, project, category string) []*MemoryFile {
	entries, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil
	}
	var out []*MemoryFile
	for _, e := range entries {
		if mf := readMemoryFile(e, project, category); mf != nil {
			out = append(out, mf)
		}
	}
	return out
}

// discoverProjectMemories collects all memory sources for a single project:
// auto-memory MEMORY.md, individual memory files, project CLAUDE.md, and project rules.
func discoverProjectMemories(projDir, projName string) []*MemoryFile {
	var out []*MemoryFile

	memoryDir := filepath.Join(projDir, "memory")
	if mf := readMemoryFile(filepath.Join(memoryDir, "MEMORY.md"), projName, "auto-memory"); mf != nil {
		out = append(out, mf)
	}

	if entries, err := os.ReadDir(memoryDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !isMemoryFileName(e.Name()) {
				continue
			}
			if mf := readMemoryFile(filepath.Join(memoryDir, e.Name()), projName, "memory-file"); mf != nil {
				out = append(out, mf)
			}
		}
	}

	// Project CLAUDE.md (in actual project directory, first match wins)
	for _, rel := range []string{"CLAUDE.md", ".claude/CLAUDE.md"} {
		if mf := readMemoryFile(filepath.Join(projName, rel), projName, "claude-md"); mf != nil {
			out = append(out, mf)
			break
		}
	}

	out = append(out, globMarkdownFiles(filepath.Join(projName, ".claude", "rules"), projName, "rules")...)
	return out
}

func readMemoryFile(path, project, category string) *MemoryFile {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	content := string(data)
	fmName, fmDesc, fmType, body := parseFrontmatter(content)

	// Render only the body (without frontmatter) as HTML
	dir := filepath.Dir(path)
	htmlContent := rewriteLocalLinks(renderMarkdown(body), dir)

	return &MemoryFile{
		Path:          path,
		Project:       project,
		Category:      category,
		Content:       content,
		HTMLContent:   htmlContent,
		ModTime:       info.ModTime().Unix(),
		FMName:        fmName,
		FMDescription: fmDesc,
		FMType:        fmType,
	}
}

// DecodePath converts an encoded project directory name back to a filesystem path.
func DecodePath(encoded string) string {
	if len(encoded) < 2 {
		return encoded
	}

	// Handle drive letter: "c--" means "c:\"
	if len(encoded) >= 3 && encoded[1] == '-' && encoded[2] == '-' {
		drive := string(encoded[0])
		rest := encoded[3:]
		parts := strings.Split(rest, "-")
		sep := string(filepath.Separator)
		return drive + ":" + sep + strings.Join(parts, sep)
	}

	return "/" + strings.ReplaceAll(encoded, "-", "/")
}

// SearchMemories searches all memory files for a query string (case-insensitive).
func SearchMemories(memories []*MemoryFile, query string) []*MemoryFile {
	if query == "" {
		return memories
	}
	q := strings.ToLower(query)
	var results []*MemoryFile
	for _, m := range memories {
		if strings.Contains(strings.ToLower(m.Content), q) ||
			strings.Contains(strings.ToLower(m.Path), q) ||
			strings.Contains(strings.ToLower(m.Project), q) ||
			strings.Contains(strings.ToLower(m.FMName), q) ||
			strings.Contains(strings.ToLower(m.FMDescription), q) {
			results = append(results, m)
		}
	}
	return results
}
