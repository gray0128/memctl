package dream

import (
	"path"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gray0128/memctl/internal/vault"
)

// This file recalls the existing entries a prepare bundle hands to the current
// agent for checking, from docs/18-agent-memory-review-and-optimization.md
// section 4.3.2: the agent distils new memories from the session digests and
// also compares the digests' troubleshooting path and tool outcomes against
// the old entries most related to them, so a wrong or outdated fact can be
// corrected with replace_entry or delete_entry. Recall reuses the in-memory
// tokenizer and BM25 scorer of internal/vault/search.go; nothing is indexed or
// persisted.

const (
	// MaxRelatedExistingEntries bounds the entries one bundle recalls.
	MaxRelatedExistingEntries = 20
	// relatedQueryTerms bounds the frequent digest terms used as queries.
	relatedQueryTerms = 24
	// relatedProjectTermWeight weights a project name above content terms.
	relatedProjectTermWeight = 3.0
	// relatedProjectFileBonus is added to every entry of a project-associated
	// topic file, so the project's own notes are recalled first.
	relatedProjectFileBonus = 2.0
)

// RelatedExistingEntry is one unexpired baseline entry recalled for checking.
// id is the entry ID of the baseline file revision, so a replace_entry or
// delete_entry operation can target it by targetEntryId or targetRawLine.
type RelatedExistingEntry struct {
	ID         string `json:"id"`
	FilePath   string `json:"filePath"`
	LineNumber int    `json:"lineNumber"`
	Heading    string `json:"heading,omitempty"`
	FactText   string `json:"factText"`
	RawLine    string `json:"rawLine"`
	AddedDate  string `json:"addedDate,omitempty"`
	UntilDate  string `json:"untilDate,omitempty"`
}

// relatedStopwords are frequent English function words that say nothing about
// a topic. CJK unigrams are skipped by construction; only bigrams are used.
var relatedStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "this": true, "that": true,
	"from": true, "are": true, "was": true, "were": true, "you": true, "your": true,
	"have": true, "has": true, "not": true, "but": true, "can": true, "will": true,
	"all": true, "any": true, "use": true, "using": true, "into": true, "then": true,
	"than": true, "there": true, "their": true, "what": true, "when": true, "which": true,
	"should": true, "would": true, "could": true, "been": true, "also": true, "only": true,
	"just": true, "now": true, "out": true, "our": true, "its": true, "via": true,
	"failed": true, "tool": true, "true": true, "false": true, "null": true,
}

// relatedExistingEntries recalls at most MaxRelatedExistingEntries unexpired
// baseline entries related to the bundle's sessions. The queries are the
// sessions' project names and their most frequent user, assistant and tool
// outcome terms; each query is scored with the vault BM25 scorer and the
// scores are summed. Entries of a project-associated topic file get a bonus.
func relatedExistingEntries(base *baseline, sessions []BundleSession, now time.Time) []RelatedExistingEntry {
	if base == nil || len(sessions) == 0 {
		return nil
	}
	projects := map[string]bool{}
	var texts []string
	for _, session := range sessions {
		if session.CwdBase != "" {
			projects[strings.ToLower(session.CwdBase)] = true
		}
		for _, turn := range session.Turns {
			texts = append(texts, turn.User, turn.Assistant)
			texts = append(texts, turn.ToolOutcomes...)
		}
	}
	terms := frequentTerms(texts, relatedQueryTerms)

	entries := recallableEntries(base, now)
	if len(entries) == 0 {
		return nil
	}
	docs := make([][]vault.SearchField, len(entries))
	dates := make([]string, len(entries))
	for index, entry := range entries {
		docs[index] = []vault.SearchField{
			{Text: entry.FilePath, Weight: vault.WeightFilePath},
			{Text: entry.Heading, Weight: vault.WeightHeading},
			{Text: entry.FactText, Weight: vault.WeightFactText},
			{Text: strings.Join(entry.DetailLines, "\n"), Weight: vault.WeightDetailLines},
		}
		dates[index] = entry.AddedDate
	}
	scores := make([]float64, len(entries))
	addQuery := func(query string, weight float64) {
		for index, result := range vault.ScoreMultiFieldDocs(docs, dates, query, now) {
			if result.Matched {
				scores[index] += weight * result.Score
			}
		}
	}
	projectNames := make([]string, 0, len(projects))
	for name := range projects {
		projectNames = append(projectNames, name)
	}
	sort.Strings(projectNames)
	for _, name := range projectNames {
		addQuery(name, relatedProjectTermWeight)
	}
	for _, term := range terms {
		addQuery(term, 1.0)
	}
	for index, entry := range entries {
		for _, name := range projectNames {
			if entryInProject(entry, name) {
				scores[index] += relatedProjectFileBonus
				break
			}
		}
	}

	order := make([]int, 0, len(entries))
	for index := range entries {
		if scores[index] > 0 {
			order = append(order, index)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if scores[a] != scores[b] {
			return scores[a] > scores[b]
		}
		if entries[a].FilePath != entries[b].FilePath {
			return entries[a].FilePath < entries[b].FilePath
		}
		return entries[a].LineNumber < entries[b].LineNumber
	})
	if len(order) > MaxRelatedExistingEntries {
		order = order[:MaxRelatedExistingEntries]
	}
	if len(order) == 0 {
		return nil
	}
	out := make([]RelatedExistingEntry, 0, len(order))
	for _, index := range order {
		entry := entries[index]
		out = append(out, RelatedExistingEntry{
			ID:         entry.ID,
			FilePath:   entry.FilePath,
			LineNumber: entry.LineNumber,
			Heading:    entry.Heading,
			FactText:   entry.FactText,
			RawLine:    entry.RawLine,
			AddedDate:  entry.AddedDate,
			UntilDate:  entry.UntilDate,
		})
	}
	return out
}

// recallableEntries lists the unexpired business entries of the baseline's
// Markdown files. MEMORY.md index lines are navigation, not facts, and are
// left out.
func recallableEntries(base *baseline, now time.Time) []vault.MemoryEntry {
	paths := make([]string, 0, len(base.files))
	for rel := range base.files {
		kind := vault.ClassifyKind(rel)
		if kind == vault.KindEntryPoint || kind == vault.KindMarkdown {
			paths = append(paths, rel)
		}
	}
	sort.Strings(paths)
	var entries []vault.MemoryEntry
	for _, rel := range paths {
		file := base.files[rel]
		for _, entry := range vault.ParseFile(rel, file.revision, file.content).Entries {
			if entry.UntilDate != "" && vault.EntryExpired(entry.UntilDate, now) {
				continue
			}
			if rel == vault.EntryPointName && strings.EqualFold(entry.Heading, "Index") {
				continue
			}
			if strings.TrimSpace(entry.FactText) == "" {
				continue
			}
			entries = append(entries, entry)
		}
	}
	return entries
}

// entryInProject reports whether an entry belongs to a project-associated
// topic file (notes/<project>.md or notes/<project>-*.md) or carries a
// `project: <name>` metadata field. name is lower case.
func entryInProject(entry vault.MemoryEntry, name string) bool {
	if name == "" {
		return false
	}
	lower := strings.ToLower(entry.FilePath)
	if strings.HasPrefix(lower, "notes/") {
		stem := strings.TrimSuffix(strings.TrimPrefix(lower, "notes/"), path.Ext(lower))
		if stem == name || strings.HasPrefix(stem, name+"-") {
			return true
		}
	}
	for _, meta := range entry.Metadata {
		if strings.EqualFold(strings.TrimSpace(meta.Key), "project") && strings.EqualFold(strings.TrimSpace(meta.Value), name) {
			return true
		}
	}
	return false
}

// frequentTerms returns the limit most frequent informative tokens of texts:
// ASCII words of at least three characters that are neither numbers nor
// stopwords, and CJK bigrams. Ties are broken by the token itself, so the
// result is deterministic.
func frequentTerms(texts []string, limit int) []string {
	counts := map[string]int{}
	for _, text := range texts {
		if text == "" {
			continue
		}
		freqs, _ := vault.TokenFreqs(text)
		for token, count := range freqs {
			if informativeTerm(token) {
				counts[token] += count
			}
		}
	}
	terms := make([]string, 0, len(counts))
	for token := range counts {
		terms = append(terms, token)
	}
	sort.Slice(terms, func(i, j int) bool {
		if counts[terms[i]] != counts[terms[j]] {
			return counts[terms[i]] > counts[terms[j]]
		}
		return terms[i] < terms[j]
	})
	if len(terms) > limit {
		terms = terms[:limit]
	}
	return terms
}

// informativeTerm filters one token produced by vault.Tokenize.
func informativeTerm(token string) bool {
	first, _ := utf8.DecodeRuneInString(token)
	if first >= 0x80 && !unicode.IsLetter(first) && !unicode.IsNumber(first) {
		return false
	}
	if first >= 0x80 {
		// CJK tokens are unigrams or adjacent bigrams; a single character
		// is too ambiguous to recall a topic.
		return utf8.RuneCountInString(token) >= 2
	}
	if len(token) < 3 || relatedStopwords[token] {
		return false
	}
	for _, char := range token {
		if !unicode.IsDigit(char) {
			return true
		}
	}
	return false
}
