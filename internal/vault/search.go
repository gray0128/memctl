package vault

import (
	"math"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Field weights for multi-field BM25 entry ranking (docs/18 section 4.3.1).
const (
	WeightFilePath    = 2.0
	WeightHeading     = 1.8
	WeightFactText    = 1.5
	WeightDetailLines = 1.0

	bm25K1 = 1.2
	bm25B  = 0.3
)

// SearchField represents one weighted text field of a searchable document.
type SearchField struct {
	Text   string
	Weight float64
	// LengthOverride optionally overrides the token count used for BM25 length
	// normalization (for example, FilePath normalizes by stem tokens rather
	// than boilerplate "notes/" and ".md" segments).
	LengthOverride int
}

// ScoredEntry holds a MemoryEntry matched by RankEntries together with its
// ranking metadata.
type ScoredEntry struct {
	Entry     MemoryEntry
	Score     float64
	FullMatch bool
	Coverage  float64
}

// isCJKRune reports whether r is a CJK Han ideograph.
func isCJKRune(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

// isWordRune reports whether r is a non-CJK letter or digit that forms
// contiguous word tokens.
func isWordRune(r rune) bool {
	if isCJKRune(r) {
		return false
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// Tokenize splits text into lowercase search tokens using pure Go rules:
//   - Non-CJK letters and digits are split on word boundaries and
//     non-alphanumeric separators (`-`, `_`, `/`, `.`, spaces, punctuation)
//     and converted to lowercase (e.g. "release-tag", "tag/release", and
//     "release tag" all yield ["release", "tag"]).
//   - Contiguous CJK Han character sequences emit both character-level
//     unigrams and adjacent bigrams so single-character and compound Chinese
//     queries both match accurately while preserving term frequency for BM25.
func Tokenize(text string) []string {
	if text == "" {
		return nil
	}
	var tokens []string
	var wordBuf []rune
	var cjkBuf []rune

	flushWord := func() {
		if len(wordBuf) == 0 {
			return
		}
		tokens = append(tokens, string(wordBuf))
		wordBuf = wordBuf[:0]
	}
	flushCJK := func() {
		if len(cjkBuf) == 0 {
			return
		}
		for _, r := range cjkBuf {
			tokens = append(tokens, string(r))
		}
		for i := 0; i+1 < len(cjkBuf); i++ {
			tokens = append(tokens, string(cjkBuf[i:i+2]))
		}
		cjkBuf = cjkBuf[:0]
	}

	for _, r := range text {
		switch {
		case isCJKRune(r):
			flushWord()
			cjkBuf = append(cjkBuf, r)
		case isWordRune(r):
			flushCJK()
			wordBuf = append(wordBuf, unicode.ToLower(r))
		default:
			flushWord()
			flushCJK()
		}
	}
	flushWord()
	flushCJK()
	return tokens
}

// TokenFreqs returns the frequency of each token in text and the total token
// count.
func TokenFreqs(text string) (map[string]int, int) {
	tokens := Tokenize(text)
	if len(tokens) == 0 {
		return nil, 0
	}
	freqs := make(map[string]int, len(tokens))
	for _, tok := range tokens {
		freqs[tok]++
	}
	return freqs, len(tokens)
}

// TokenSet returns the deduplicated set of tokens in text. It is shared by
// search coverage checks, Dream consolidation (I-3b), and Doctor near-duplicate
// Jaccard detection (I-4).
func TokenSet(text string) map[string]struct{} {
	tokens := Tokenize(text)
	if len(tokens) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(tokens))
	for _, tok := range tokens {
		set[tok] = struct{}{}
	}
	return set
}

// UniqueTokens returns the deduplicated tokens of text in first-seen order.
func UniqueTokens(text string) []string {
	tokens := Tokenize(text)
	if len(tokens) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return out
}

// JaccardSimilarity computes |A ∩ B| / |A ∪ B| for two token sets. It returns
// 0 when both sets are empty.
func JaccardSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for k := range a {
		if _, ok := b[k]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union <= 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// TextJaccardSimilarity tokenizes a and b and returns their Jaccard similarity.
func TextJaccardSimilarity(a, b string) float64 {
	return JaccardSimilarity(TokenSet(a), TokenSet(b))
}

type queryRun struct {
	isCJK    bool
	raw      string
	unigrams []string
	bigrams  []string
	all      []string
}

type queryPlan struct {
	rawTrimmed string
	lowerRaw   string
	runs       []queryRun
	tokens     []queryTokenWeight
}

type queryTokenWeight struct {
	token  string
	weight float64
}

func buildQueryPlan(q string) queryPlan {
	trimmed := strings.TrimSpace(q)
	if trimmed == "" {
		return queryPlan{}
	}
	plan := queryPlan{
		rawTrimmed: trimmed,
		lowerRaw:   strings.ToLower(trimmed),
	}

	var wordBuf []rune
	var cjkBuf []rune
	seenRuns := make(map[string]struct{})

	flushWord := func() {
		if len(wordBuf) == 0 {
			return
		}
		w := string(wordBuf)
		wordBuf = wordBuf[:0]
		key := "w:" + w
		if _, ok := seenRuns[key]; ok {
			return
		}
		seenRuns[key] = struct{}{}
		plan.runs = append(plan.runs, queryRun{
			isCJK:    false,
			raw:      w,
			unigrams: []string{w},
			all:      []string{w},
		})
	}
	flushCJK := func() {
		if len(cjkBuf) == 0 {
			return
		}
		raw := string(cjkBuf)
		runes := append([]rune(nil), cjkBuf...)
		cjkBuf = cjkBuf[:0]
		key := "c:" + raw
		if _, ok := seenRuns[key]; ok {
			return
		}
		seenRuns[key] = struct{}{}

		var unigrams, bigrams, all []string
		seenTok := make(map[string]struct{}, len(runes)*2)
		for _, r := range runes {
			u := string(r)
			if _, ok := seenTok[u]; !ok {
				seenTok[u] = struct{}{}
				unigrams = append(unigrams, u)
				all = append(all, u)
			}
		}
		for i := 0; i+1 < len(runes); i++ {
			bg := string(runes[i : i+2])
			if _, ok := seenTok[bg]; !ok {
				seenTok[bg] = struct{}{}
				bigrams = append(bigrams, bg)
				all = append(all, bg)
			}
		}
		plan.runs = append(plan.runs, queryRun{
			isCJK:    true,
			raw:      raw,
			unigrams: unigrams,
			bigrams:  bigrams,
			all:      all,
		})
	}

	for _, r := range trimmed {
		switch {
		case isCJKRune(r):
			flushWord()
			cjkBuf = append(cjkBuf, r)
		case isWordRune(r):
			flushCJK()
			wordBuf = append(wordBuf, unicode.ToLower(r))
		default:
			flushWord()
			flushCJK()
		}
	}
	flushWord()
	flushCJK()

	tokenWeights := make(map[string]float64)
	var tokenOrder []string
	addToken := func(tok string, w float64) {
		if prev, ok := tokenWeights[tok]; ok {
			if w > prev {
				tokenWeights[tok] = w
			}
			return
		}
		tokenWeights[tok] = w
		tokenOrder = append(tokenOrder, tok)
	}
	for _, run := range plan.runs {
		if !run.isCJK {
			addToken(run.raw, 1.0)
			continue
		}
		if len(run.bigrams) == 0 {
			for _, u := range run.unigrams {
				addToken(u, 1.0)
			}
			continue
		}
		for _, bg := range run.bigrams {
			addToken(bg, 1.0)
		}
		for _, u := range run.unigrams {
			addToken(u, 0.5)
		}
	}
	for _, tok := range tokenOrder {
		plan.tokens = append(plan.tokens, queryTokenWeight{
			token:  tok,
			weight: tokenWeights[tok],
		})
	}
	return plan
}

// evaluateCoverage checks whether a document's token set and raw lowercase
// field texts satisfy the query plan. It returns (matched, fullMatch, coverage,
// phraseFieldWeight).
func evaluateCoverage(plan queryPlan, docTokens map[string]struct{}, fields []SearchField, lowerFields []string) (bool, bool, float64, float64) {
	if plan.rawTrimmed == "" {
		return true, true, 1.0, 0
	}

	phraseWeight := 0.0
	for i, lf := range lowerFields {
		if lf != "" && strings.Contains(lf, plan.lowerRaw) {
			if fields[i].Weight > phraseWeight {
				phraseWeight = fields[i].Weight
			}
		}
	}

	if len(plan.runs) == 0 {
		if phraseWeight > 0 {
			return true, true, 1.0, phraseWeight
		}
		return false, false, 0, 0
	}

	fullRuns := 0
	matchedRuns := 0
	totalCov := 0.0

	for _, run := range plan.runs {
		if !run.isCJK {
			_, hasTok := docTokens[run.raw]
			if !hasTok && len(run.raw) >= 3 {
				for _, lf := range lowerFields {
					if strings.Contains(lf, run.raw) {
						hasTok = true
						break
					}
				}
			}
			if hasTok {
				fullRuns++
				matchedRuns++
				totalCov += 1.0
			}
			continue
		}

		// CJK run evaluation.
		matchedAll := 0
		for _, tok := range run.all {
			if _, ok := docTokens[tok]; ok {
				matchedAll++
			}
		}
		if matchedAll == len(run.all) && len(run.all) > 0 {
			fullRuns++
			matchedRuns++
			totalCov += 1.0
			continue
		}

		matchedUni := 0
		for _, u := range run.unigrams {
			if _, ok := docTokens[u]; ok {
				matchedUni++
			}
		}
		matchedBi := 0
		for _, bg := range run.bigrams {
			if _, ok := docTokens[bg]; ok {
				matchedBi++
			}
		}

		// Partial match within a CJK run requires >= 50% token coverage and either
		// at least one adjacent bigram match or all unigrams present (e.g. a 2-char
		// CJK word where both characters appear in the entry).
		if len(run.all) > 0 && matchedAll*2 >= len(run.all) && (matchedBi >= 1 || (len(run.unigrams) >= 2 && matchedUni == len(run.unigrams))) {
			matchedRuns++
			totalCov += float64(matchedAll) / float64(len(run.all))
		}
	}

	m := len(plan.runs)
	if phraseWeight > 0 || fullRuns == m {
		return true, true, 1.0, phraseWeight
	}
	if m == 1 {
		if matchedRuns == 1 {
			return true, false, totalCov, phraseWeight
		}
		return false, false, 0, 0
	}
	// Multi-run query: Tier 2 (OR) requires >= 50% of query runs to match.
	if matchedRuns*2 >= m {
		return true, false, float64(matchedRuns) / float64(m), phraseWeight
	}
	return false, false, 0, 0
}

// RecencyBoost returns a deterministic positive score boost for entries with a
// valid YYYY-MM-DD addedDate, monotonically increasing with newer dates while
// staying bounded so field-weight hierarchy is preserved.
func RecencyBoost(addedDate string, now time.Time) float64 {
	if addedDate == "" {
		return 0
	}
	added, err := time.Parse("2006-01-02", addedDate)
	if err != nil {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	daysOld := now.Sub(added).Hours() / 24.0
	epochDays := float64(added.Unix()/86400) * 1e-8
	if daysOld < 0 {
		return 0.10 + math.Min(0.04, (-daysOld)/36500.0) + epochDays
	}
	return (0.10 / (1.0 + daysOld/30.0)) + epochDays
}

func filePathStemTokenCount(relPath string) int {
	trimmed := strings.TrimPrefix(relPath, "notes/")
	ext := path.Ext(trimmed)
	if ext != "" {
		trimmed = strings.TrimSuffix(trimmed, ext)
	}
	toks := Tokenize(trimmed)
	if len(toks) == 0 {
		return 1
	}
	return len(toks)
}

func entrySearchFields(entry MemoryEntry) []SearchField {
	detailText := ""
	if len(entry.DetailLines) > 0 {
		detailText = strings.Join(entry.DetailLines, "\n")
	}
	return []SearchField{
		{
			Text:           entry.FilePath,
			Weight:         WeightFilePath,
			LengthOverride: filePathStemTokenCount(entry.FilePath),
		},
		{
			Text:   entry.Heading,
			Weight: WeightHeading,
		},
		{
			Text:   entry.FactText,
			Weight: WeightFactText,
		},
		{
			Text:   detailText,
			Weight: WeightDetailLines,
		},
	}
}

type preparedField struct {
	freqs  map[string]int
	length int
}

type preparedDoc struct {
	fields      []SearchField
	lowerFields []string
	prepFields  []preparedField
	tokenSet    map[string]struct{}
	matched     bool
	fullMatch   bool
	coverage    float64
	phraseW     float64
}

// ScoreMultiFieldDocs scores a slice of documents (each represented by a fixed
// number of weighted SearchFields and an optional YYYY-MM-DD or RFC3339 date
// string) against query. It returns a parallel slice of (matched, fullMatch,
// coverage, score).
func ScoreMultiFieldDocs(docs [][]SearchField, dates []string, query string, now time.Time) []struct {
	Matched   bool
	FullMatch bool
	Coverage  float64
	Score     float64
} {
	results := make([]struct {
		Matched   bool
		FullMatch bool
		Coverage  float64
		Score     float64
	}, len(docs))

	plan := buildQueryPlan(query)
	if plan.rawTrimmed == "" || len(docs) == 0 {
		return results
	}

	numFields := 0
	for _, f := range docs {
		if len(f) > numFields {
			numFields = len(f)
		}
	}
	fieldLengthSums := make([]int, numFields)
	fieldDocCounts := make([]int, numFields)
	df := make(map[string]int, len(plan.tokens))

	prepDocs := make([]preparedDoc, len(docs))
	for i, fields := range docs {
		pdoc := preparedDoc{
			fields:      fields,
			lowerFields: make([]string, len(fields)),
			prepFields:  make([]preparedField, len(fields)),
			tokenSet:    make(map[string]struct{}),
		}
		for fIdx, f := range fields {
			if f.Text == "" {
				continue
			}
			pdoc.lowerFields[fIdx] = strings.ToLower(f.Text)
			freqs, tokLen := TokenFreqs(f.Text)
			dl := tokLen
			if f.LengthOverride > 0 {
				dl = f.LengthOverride
			}
			pdoc.prepFields[fIdx] = preparedField{
				freqs:  freqs,
				length: dl,
			}
			if dl > 0 {
				fieldLengthSums[fIdx] += dl
				fieldDocCounts[fIdx]++
			}
			for tok := range freqs {
				pdoc.tokenSet[tok] = struct{}{}
			}
		}
		for _, qt := range plan.tokens {
			if _, ok := pdoc.tokenSet[qt.token]; ok {
				df[qt.token]++
			}
		}
		pdoc.matched, pdoc.fullMatch, pdoc.coverage, pdoc.phraseW = evaluateCoverage(
			plan, pdoc.tokenSet, fields, pdoc.lowerFields,
		)
		prepDocs[i] = pdoc
	}

	avgDL := make([]float64, numFields)
	for fIdx := 0; fIdx < numFields; fIdx++ {
		if fieldDocCounts[fIdx] > 0 {
			avgDL[fIdx] = float64(fieldLengthSums[fIdx]) / float64(fieldDocCounts[fIdx])
		}
		if avgDL[fIdx] < 1.0 {
			avgDL[fIdx] = 1.0
		}
	}

	totalDocs := float64(len(docs))
	idf := make(map[string]float64, len(plan.tokens))
	for _, qt := range plan.tokens {
		docFreq := float64(df[qt.token])
		idf[qt.token] = math.Log(1.0+(totalDocs-docFreq+0.5)/(docFreq+0.5)) + 0.5
	}

	for i, pdoc := range prepDocs {
		if !pdoc.matched {
			continue
		}
		bm25Score := 0.0
		for _, qt := range plan.tokens {
			termIDF := idf[qt.token] * qt.weight
			for fIdx, pf := range pdoc.prepFields {
				tf := pf.freqs[qt.token]
				if tf == 0 {
					continue
				}
				w := pdoc.fields[fIdx].Weight
				dl := float64(pf.length)
				if dl < 1.0 {
					dl = 1.0
				}
				normTF := (float64(tf) * (bm25K1 + 1.0)) /
					(float64(tf) + bm25K1*(1.0-bm25B+bm25B*(dl/avgDL[fIdx])))
				bm25Score += w * termIDF * normTF
			}
		}

		// If a query token matched only as a substring (e.g. a prefix not yet a
		// full token), ensure the matching field still contributes a positive
		// base score proportional to its field weight.
		if bm25Score == 0 && pdoc.phraseW > 0 {
			bm25Score = pdoc.phraseW * 0.5
		}

		phraseBonus := 0.25 * pdoc.phraseW
		dateStr := ""
		if i < len(dates) {
			dateStr = dates[i]
		}
		recency := RecencyBoost(dateStr, now)

		results[i] = struct {
			Matched   bool
			FullMatch bool
			Coverage  float64
			Score     float64
		}{
			Matched:   true,
			FullMatch: pdoc.fullMatch,
			Coverage:  pdoc.coverage,
			Score:     bm25Score + phraseBonus + recency,
		}
	}
	return results
}

// RankEntries filters and ranks entries against query using the 4-field
// weighted BM25 scorer (FilePath=2.0, Heading=1.8, FactText=1.5,
// DetailLines=1.0), exact substring/phrase bonus, and AddedDate recency boost.
//
// Tier 1 entries (FullMatch: 100% of query units matched, i.e. AND) are always
// ordered ahead of Tier 2 entries (OR partial match with coverage >= 50%);
// within each tier, entries are ordered by Score descending, then FilePath
// ascending, then LineNumber ascending.
func RankEntries(entries []MemoryEntry, query string, now time.Time) []ScoredEntry {
	if strings.TrimSpace(query) == "" || len(entries) == 0 {
		return nil
	}
	docs := make([][]SearchField, len(entries))
	dates := make([]string, len(entries))
	for i, entry := range entries {
		docs[i] = entrySearchFields(entry)
		dates[i] = entry.AddedDate
	}
	rawScores := ScoreMultiFieldDocs(docs, dates, query, now)

	var scored []ScoredEntry
	for i, rs := range rawScores {
		if !rs.Matched {
			continue
		}
		scored = append(scored, ScoredEntry{
			Entry:     entries[i],
			Score:     rs.Score,
			FullMatch: rs.FullMatch,
			Coverage:  rs.Coverage,
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		a, b := scored[i], scored[j]
		if a.FullMatch != b.FullMatch {
			return a.FullMatch
		}
		if math.Abs(a.Score-b.Score) > 1e-9 {
			return a.Score > b.Score
		}
		if a.Entry.FilePath != b.Entry.FilePath {
			return a.Entry.FilePath < b.Entry.FilePath
		}
		return a.Entry.LineNumber < b.Entry.LineNumber
	})
	return scored
}
