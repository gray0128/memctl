package corpus

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// encodeDigest serializes one digest and enforces MaxSessionDigestBytes. The
// turn cap in parse is a parsing limit; this is the output limit, and both
// apply: whichever fires first decides the kept turns. Earlier non-correction
// turns are dropped until the record fits.
func encodeDigest(d Digest) ([]byte, error) {
	turns, err := fitTurns(d)
	if err != nil {
		return nil, err
	}
	d.Turns = turns
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return data, nil
}

// fitTurns drops the earliest non-correction turns until the serialized digest
// fits MaxSessionDigestBytes. A correction turn is kept while the record fits,
// and the newest turn is never the first to go when an older non-correction
// turn exists. A digest whose every turn carries a correction cannot be
// represented within the documented output limit; that is a collector bug, so
// it is reported instead of silently clipped.
func fitTurns(d Digest) ([]Turn, error) {
	turns := append([]Turn{}, d.Turns...)
	for {
		candidate := d
		candidate.Turns = turns
		data, err := json.Marshal(candidate)
		if err != nil {
			return nil, err
		}
		if len(data) <= MaxSessionDigestBytes {
			return turns, nil
		}
		index := trimIndex(turns)
		if index < 0 {
			return nil, memerr.New(memerr.LimitExceeded,
				"the session digest cannot fit in %d bytes", MaxSessionDigestBytes)
		}
		turns = append(turns[:index], turns[index+1:]...)
	}
}

// trimIndex picks the earliest turn without a correction signal. It returns -1
// when every remaining turn carries one, in which case the caller stops
// trimming and accepts the oversized record rather than dropping a correction.
func trimIndex(turns []Turn) int {
	for index, turn := range turns {
		if !hasCorrectionSignal(turn.User) && !hasCorrectionSignal(turn.Assistant) {
			return index
		}
	}
	return -1
}

// decodeDigest parses one stored digest and validates its layout.
func decodeDigest(data []byte) (Digest, error) {
	var value Digest
	if err := json.Unmarshal(data, &value); err != nil {
		return Digest{}, err
	}
	if value.SchemaVersion != SchemaVersion || value.Agent == "" || value.SessionID == "" {
		return Digest{}, errUnsupported
	}
	if value.Turns == nil {
		value.Turns = []Turn{}
	}
	return value, nil
}

// errUnsupported marks a record written by another schema version or a
// truncated file. Callers skip such a record instead of guessing its fields.
var errUnsupported = jsonError("unsupported corpus digest")

type jsonError string

func (e jsonError) Error() string { return string(e) }

// correctionSignals are the user correction cues of
// docs/12-session-corpus-dreaming.md section 2.2.3. A turn containing one is
// kept in preference to older turns without one when the turn or byte budget
// is exceeded.
var correctionSignals = []string{
	"不要", "改为", "记住", "不对", "以后", "禁止", "别再", "应该是", "而不是",
	"don't", "do not", "instead", "remember", "never", "always use", "stop using",
}

// hasCorrectionSignal reports whether text carries a user correction cue.
func hasCorrectionSignal(text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, signal := range correctionSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

// boundTurns applies MaxTurnsPerSession while preferring correction turns and
// the most recent ones. The input is in chronological order and the output
// keeps that order.
func boundTurns(turns []Turn) []Turn {
	if len(turns) <= MaxTurnsPerSession {
		return turns
	}
	keep := make([]bool, len(turns))
	remaining := MaxTurnsPerSession
	for index := len(turns) - 1; index >= 0 && remaining > 0; index-- {
		if hasCorrectionSignal(turns[index].User) {
			keep[index] = true
			remaining--
		}
	}
	for index := len(turns) - 1; index >= 0 && remaining > 0; index-- {
		if keep[index] {
			continue
		}
		keep[index] = true
		remaining--
	}
	kept := make([]Turn, 0, MaxTurnsPerSession)
	for index, turn := range turns {
		if keep[index] {
			kept = append(kept, turn)
		}
	}
	return kept
}

// truncateText bounds one text field by UTF-8 bytes without splitting a rune.
// The documented ellipsis is part of the limit, so the marked text never
// exceeds it.
func truncateText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	cut := limit - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return strings.TrimRight(text[:cut], " \t\r\n") + ellipsis
}

// ellipsis marks a text that the collector cut.
const ellipsis = "…"

// boundTools applies MaxToolsPerTurn and MaxToolTextBytes.
func boundTools(tools []string) []string {
	if len(tools) == 0 {
		return nil
	}
	kept := make([]string, 0, MaxToolsPerTurn)
	for _, tool := range tools {
		tool = truncateText(asciiSafe(singleLine(tool)), MaxToolTextBytes)
		if tool == "" {
			continue
		}
		kept = append(kept, tool)
		if len(kept) >= MaxToolsPerTurn {
			break
		}
	}
	return kept
}

// singleLine collapses a multi-line tool signature onto its first line, which
// is where the command or path is.
func singleLine(text string) string {
	if index := strings.IndexAny(text, "\r\n"); index >= 0 {
		text = text[:index]
	}
	return strings.TrimSpace(text)
}

// digestDate picks the date stored with a digest: the local date of the newest
// event, falling back to the wall clock when the log carries no timestamp.
func digestDate(newest time.Time, fallback time.Time) string {
	value := newest
	if value.IsZero() {
		value = fallback
	}
	return value.Local().Format(vault.DateLayout)
}

// asciiSafe keeps a tool signature inside MaxToolTextBytes even when the
// ellipsis itself would overflow the limit, which happens only for a path made
// of multi-byte runes. It is byte-slicing on purpose: the bound is a hard one.
func asciiSafe(text string) string {
	if len(text) <= MaxToolTextBytes {
		return text
	}
	cut := MaxToolTextBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}
