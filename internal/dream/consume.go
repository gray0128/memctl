package dream

import (
	"github.com/gray0128/memctl/internal/corpus"
	"github.com/gray0128/memctl/internal/memerr"
)

// This file implements the consumption write-back of
// docs/12-session-corpus-dreaming.md section 2.3 step 4. Only an apply that
// already committed reaches it, and it is best effort and re-entrant: a digest
// whose bytes changed or disappeared after the proposal was built is skipped
// instead of being overwritten with a consumedBy that never saw it. The result
// is returned to the caller and the skips are appended to the proposal warnings.

// ConsumeResult reports which digests the write-back marked and which it skipped
// because they changed or disappeared after the proposal was built.
type ConsumeResult struct {
	Consumed []string `json:"consumed"`
	Skipped  []string `json:"skipped"`
}

// ConsumeCorpusDigests marks the digests a local proposal consumed. The caller
// must only invoke it after the apply commit succeeded. A repeated call re-reads
// each digest, keeps an already consumedBy untouched and reports the same skips,
// so an interrupted write-back is completed by the next apply.
func (s *Service) ConsumeCorpusDigests(proposalID string) (ConsumeResult, error) {
	result := ConsumeResult{Consumed: []string{}, Skipped: []string{}}
	value, err := s.store.Load(proposalID)
	if err != nil {
		return result, err
	}
	if len(value.CorpusDigests) == 0 {
		return result, nil
	}
	cache := s.corpusCache()
	byPath := make(map[string]corpus.Digest, len(value.CorpusDigests))
	for _, digest := range cache.List() {
		byPath[cache.Path(digest.Agent, digest.SessionID)] = digest
	}

	var warnings []string
	for _, recorded := range value.CorpusDigests {
		name := recorded.File
		current, ok := byPath[recorded.File]
		if !ok {
			result.Skipped = append(result.Skipped, name)
			warnings = append(warnings, "the session digest "+name+" disappeared before it could be marked as consumed")
			continue
		}
		name = current.Agent + "/" + current.SessionID
		// A digest consumed by an earlier apply keeps the first record: a second
		// apply must never rewrite consumedRevision with newer content.
		if current.ConsumedBy != "" {
			result.Consumed = append(result.Consumed, name)
			continue
		}
		revision := digestRevision(cache, current.Agent, current.SessionID)
		if revision == "" || revision != recorded.Revision {
			result.Skipped = append(result.Skipped, name)
			warnings = append(warnings, "the session digest "+name+
				" changed after the proposal was built and was not marked as consumed")
			continue
		}
		current.ConsumedBy = proposalID
		current.ConsumedRevision = revision
		if err := cache.Save(current); err != nil {
			return result, memerr.Wrap(memerr.IOFailed, err,
				"cannot mark the session digest %s as consumed", name)
		}
		result.Consumed = append(result.Consumed, name)
	}
	if len(warnings) > 0 {
		value.Warnings = append(value.Warnings, warnings...)
		if err := s.store.Save(value); err != nil {
			return result, err
		}
	}
	return result, nil
}
