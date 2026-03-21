package memory

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

type ConsolidationGroup struct {
	ID             int           `json:"id"`
	Files          []*MemoryFile `json:"files"`
	MaxScore       float64       `json:"max_score"`
	SharedKeywords []string      `json:"shared_keywords"`
	Reason         string        `json:"reason"`
	Prompt         string        `json:"prompt"`
}

type ConsolidationReport struct {
	Groups    []ConsolidationGroup `json:"groups"`
	CheckedAt int64                `json:"checked_at"`
}

type scoredPair struct {
	idxA, idxB     int
	score          float64
	sharedWords    []string
	sharedEntities []string
}

const similarityThreshold = 0.3
const maxGroups = 20

var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
	"with": true, "by": true, "from": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "been": true, "being": true, "have": true, "has": true,
	"had": true, "do": true, "does": true, "did": true, "will": true, "would": true,
	"could": true, "should": true, "may": true, "might": true, "can": true,
	"this": true, "that": true, "these": true, "those": true, "it": true,
	"its": true, "not": true, "no": true, "as": true, "if": true, "then": true,
	"so": true, "up": true, "out": true, "about": true, "into": true,
	"all": true, "each": true, "every": true, "both": true, "few": true,
	"more": true, "most": true, "other": true, "some": true, "such": true,
	"only": true, "same": true, "than": true, "too": true, "very": true,
	"just": true, "also": true, "use": true, "used": true, "using": true,
}

// FindConsolidation detects cross-project duplicate memories and groups them.
func FindConsolidation(files []*MemoryFile) *ConsolidationReport {
	report := &ConsolidationReport{
		Groups:    []ConsolidationGroup{},
		CheckedAt: time.Now().Unix(),
	}

	// Only consider memory-file category (not CLAUDE.md, rules, or MEMORY.md indexes)
	var candidates []*MemoryFile
	for _, f := range files {
		if f.Category == "memory-file" && f.Project != "" {
			candidates = append(candidates, f)
		}
	}

	if len(candidates) < 2 {
		return report
	}

	// Pre-compute bigram sets and entity sets for each candidate
	bigramSets := make([]map[string]bool, len(candidates))
	entitySets := make([]map[string]bool, len(candidates))
	for i, f := range candidates {
		_, _, _, body := parseFrontmatter(f.Content)
		words := tokenize(body)
		bigramSets[i] = bigrams(words)

		entities := extractEntities(f.Content)
		entitySets[i] = make(map[string]bool, len(entities))
		for _, e := range entities {
			entitySets[i][e] = true
		}
	}

	// Score all cross-project pairs
	var pairs []scoredPair
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[i].Project == candidates[j].Project {
				continue
			}
			score, sharedWords, sharedEntities := scorePair(
				candidates[i], candidates[j],
				bigramSets[i], bigramSets[j],
				entitySets[i], entitySets[j],
			)
			if score >= similarityThreshold {
				pairs = append(pairs, scoredPair{
					idxA: i, idxB: j,
					score:          score,
					sharedWords:    sharedWords,
					sharedEntities: sharedEntities,
				})
			}
		}
	}

	if len(pairs) == 0 {
		return report
	}

	// Cluster pairs into groups using union-find
	groups := clusterPairs(pairs, candidates)

	// Sort by max score descending
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].MaxScore > groups[j].MaxScore
	})

	// Cap at maxGroups
	if len(groups) > maxGroups {
		groups = groups[:maxGroups]
	}

	// Assign IDs and build reasons/prompts
	for i := range groups {
		groups[i].ID = i + 1
		groups[i].Reason = buildReason(groups[i])
		groups[i].Prompt = buildPrompt(groups[i])
	}

	report.Groups = groups
	return report
}

func scorePair(a, b *MemoryFile, bigramsA, bigramsB, entitiesA, entitiesB map[string]bool) (float64, []string, []string) {
	// Entity similarity (weight 0.4)
	entityScore := jaccard(entitiesA, entitiesB)

	// Bigram similarity (weight 0.4)
	bigramScore := jaccard(bigramsA, bigramsB)

	// FMType match (weight 0.2)
	var typeScore float64
	if a.FMType != "" && a.FMType == b.FMType {
		typeScore = 1.0
	}

	score := 0.4*entityScore + 0.4*bigramScore + 0.2*typeScore

	// Collect shared words for display
	var sharedWords []string
	for bg := range bigramsA {
		if bigramsB[bg] {
			sharedWords = append(sharedWords, bg)
		}
	}
	sort.Strings(sharedWords)
	if len(sharedWords) > 10 {
		sharedWords = sharedWords[:10]
	}

	var sharedEntities []string
	for e := range entitiesA {
		if entitiesB[e] {
			sharedEntities = append(sharedEntities, e)
		}
	}
	sort.Strings(sharedEntities)

	return score, sharedWords, sharedEntities
}

func tokenize(content string) []string {
	var words []string
	for _, word := range strings.FieldsFunc(strings.ToLower(content), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(word) > 1 && !stopWords[word] {
			words = append(words, word)
		}
	}
	return words
}

func bigrams(words []string) map[string]bool {
	set := make(map[string]bool)
	for i := 0; i+1 < len(words); i++ {
		set[words[i]+" "+words[i+1]] = true
	}
	return set
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// clusterPairs groups scored pairs using union-find single-linkage clustering.
func clusterPairs(pairs []scoredPair, candidates []*MemoryFile) []ConsolidationGroup {
	parent := make(map[int]int)
	var find func(int) int
	find = func(x int) int {
		if _, ok := parent[x]; !ok {
			parent[x] = x
		}
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(x, y int) {
		px, py := find(x), find(y)
		if px != py {
			parent[px] = py
		}
	}

	// Track max score and shared keywords per group
	type groupMeta struct {
		maxScore       float64
		sharedKeywords map[string]bool
	}

	// Union all pairs
	for _, p := range pairs {
		union(p.idxA, p.idxB)
	}

	// Collect members per root and accumulate metadata
	members := make(map[int][]int)
	meta := make(map[int]*groupMeta)
	for _, p := range pairs {
		root := find(p.idxA)
		if meta[root] == nil {
			meta[root] = &groupMeta{sharedKeywords: make(map[string]bool)}
		}
		if p.score > meta[root].maxScore {
			meta[root].maxScore = p.score
		}
		for _, w := range p.sharedWords {
			meta[root].sharedKeywords[w] = true
		}
		for _, e := range p.sharedEntities {
			meta[root].sharedKeywords[e] = true
		}
	}

	// Collect unique members per root
	memberSet := make(map[int]map[int]bool)
	for _, p := range pairs {
		root := find(p.idxA)
		if memberSet[root] == nil {
			memberSet[root] = make(map[int]bool)
		}
		memberSet[root][p.idxA] = true
		memberSet[root][p.idxB] = true
	}
	for root, set := range memberSet {
		for idx := range set {
			members[root] = append(members[root], idx)
		}
		sort.Ints(members[root])
	}

	var groups []ConsolidationGroup
	for root, idxs := range members {
		var files []*MemoryFile
		for _, idx := range idxs {
			files = append(files, candidates[idx])
		}

		keywords := make([]string, 0, len(meta[root].sharedKeywords))
		for k := range meta[root].sharedKeywords {
			keywords = append(keywords, k)
		}
		sort.Strings(keywords)
		if len(keywords) > 15 {
			keywords = keywords[:15]
		}

		groups = append(groups, ConsolidationGroup{
			Files:          files,
			MaxScore:       meta[root].maxScore,
			SharedKeywords: keywords,
		})
	}

	return groups
}

func buildReason(g ConsolidationGroup) string {
	projects := make(map[string]bool)
	for _, f := range g.Files {
		projects[filepath.Base(f.Project)] = true
	}
	projList := make([]string, 0, len(projects))
	for p := range projects {
		projList = append(projList, p)
	}
	sort.Strings(projList)

	keywords := g.SharedKeywords
	if len(keywords) > 5 {
		keywords = keywords[:5]
	}

	return fmt.Sprintf("%d files across %s share: %s",
		len(g.Files),
		strings.Join(projList, ", "),
		strings.Join(keywords, ", "),
	)
}

func buildPrompt(g ConsolidationGroup) string {
	var sb strings.Builder
	sb.WriteString("Consolidate these duplicate memories into a single shared memory:\n")
	for _, f := range g.Files {
		sb.WriteString(fmt.Sprintf("- %s\n", f.Path))
	}
	if len(g.SharedKeywords) > 0 {
		sb.WriteString(fmt.Sprintf("They share: %s.\n", strings.Join(g.SharedKeywords, ", ")))
	}
	sb.WriteString("Merge the content into a single file (either a global rule in ~/.claude/rules/ or keep in one project's memory/), then remove the duplicates and update each project's MEMORY.md index.")
	return sb.String()
}
