package vault

import (
	"reflect"
	"testing"
	"time"
)

func TestTokenizeMixedASCIIAndCJK(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{
			input: "release-tag",
			want:  []string{"release", "tag"},
		},
		{
			input: "tag/release",
			want:  []string{"tag", "release"},
		},
		{
			input: "release_tag.v2",
			want:  []string{"release", "tag", "v2"},
		},
		{
			input: "Release TAG",
			want:  []string{"release", "tag"},
		},
		{
			input: "发布",
			want:  []string{"发", "布", "发布"},
		},
		{
			input: "记忆库",
			want:  []string{"记", "忆", "库", "记忆", "忆库"},
		},
		{
			input: "使用 git-tag 发布版本",
			want:  []string{"使", "用", "使用", "git", "tag", "发", "布", "版", "本", "发布", "布版", "版本"},
		},
	}

	for _, tc := range cases {
		got := Tokenize(tc.input)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Tokenize(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestTokenSetAndJaccardSimilarity(t *testing.T) {
	setA := TokenSet("push release-tag to remote")
	setB := TokenSet("push tag/release to origin")
	for _, want := range []string{"push", "release", "tag", "to"} {
		if _, ok := setA[want]; !ok {
			t.Fatalf("TokenSet missing %q: %v", want, setA)
		}
	}
	sim := JaccardSimilarity(setA, setB)
	// A = {push, release, tag, to, remote} (5), B = {push, tag, release, to, origin} (5)
	// intersection = 4, union = 6 -> 4/6 = 2/3
	if sim < 0.66 || sim > 0.67 {
		t.Fatalf("JaccardSimilarity = %f, want ~0.6667", sim)
	}
}

func TestRankEntriesMultiWordOutOfOrderAndCompound(t *testing.T) {
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	entries := []MemoryEntry{
		{
			FilePath:   "notes/git.md",
			LineNumber: 3,
			Heading:    "Workflow",
			FactText:   "push release via git tag",
			AddedDate:  "2026-06-01",
		},
		{
			FilePath:   "notes/git.md",
			LineNumber: 4,
			Heading:    "Workflow",
			FactText:   "only push release branch",
			AddedDate:  "2026-06-01",
		},
		{
			FilePath:   "notes/other.md",
			LineNumber: 2,
			Heading:    "Misc",
			FactText:   "unrelated entry about formatting",
			AddedDate:  "2026-06-01",
		},
	}

	// Out-of-order multi-word query "tag release" hits "push release via git tag"
	// as Tier 1 (AND, FullMatch=true) and "only push release branch" as Tier 2
	// (OR, 50% coverage, FullMatch=false).
	scored := RankEntries(entries, "tag release", now)
	if len(scored) != 2 {
		t.Fatalf("RankEntries('tag release') returned %d items, want 2: %+v", len(scored), scored)
	}
	if !scored[0].FullMatch || scored[0].Entry.FactText != "push release via git tag" {
		t.Fatalf("first result = %+v, want full match on 'push release via git tag'", scored[0])
	}
	if scored[1].FullMatch || scored[1].Entry.FactText != "only push release branch" {
		t.Fatalf("second result = %+v, want partial OR match on 'only push release branch'", scored[1])
	}

	// Compound query "release-tag" also matches "push release via git tag" as FullMatch.
	scoredCompound := RankEntries(entries, "release-tag", now)
	if len(scoredCompound) != 2 || !scoredCompound[0].FullMatch || scoredCompound[0].Entry.LineNumber != 3 {
		t.Fatalf("RankEntries('release-tag') = %+v, want full match on line 3 first", scoredCompound)
	}
}

func TestRankEntriesCJKUnigramAndBigram(t *testing.T) {
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	entries := []MemoryEntry{
		{
			FilePath:   "notes/ops.md",
			LineNumber: 2,
			Heading:    "部署",
			FactText:   "通过镜像标签完成灰度发布流程",
			AddedDate:  "2026-06-01",
		},
		{
			FilePath:   "notes/ops.md",
			LineNumber: 3,
			Heading:    "部署",
			FactText:   "仅执行灰度验证步骤",
			AddedDate:  "2026-06-01",
		},
		{
			FilePath:   "notes/ops.md",
			LineNumber: 4,
			Heading:    "日志",
			FactText:   "发现一行错误日志",
			AddedDate:  "2026-06-01",
		},
	}

	// Multi-term Chinese query "灰度 发布":
	// Line 2 has both "灰度" and "发布" -> Tier 1 (FullMatch).
	// Line 3 has only "灰度" (1/2 = 50%) -> Tier 2 (OR).
	// Line 4 only shares single char '发' with "发布" (< 50%) -> excluded.
	scored := RankEntries(entries, "灰度 发布", now)
	if len(scored) != 2 {
		t.Fatalf("RankEntries('灰度 发布') returned %d items, want 2: %+v", len(scored), scored)
	}
	if !scored[0].FullMatch || scored[0].Entry.LineNumber != 2 {
		t.Fatalf("scored[0] = %+v, want line 2 full match", scored[0])
	}
	if scored[1].FullMatch || scored[1].Entry.LineNumber != 3 {
		t.Fatalf("scored[1] = %+v, want line 3 partial match", scored[1])
	}
}

func TestRankEntriesFieldWeightHierarchyAndRecency(t *testing.T) {
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	entries := []MemoryEntry{
		{
			FilePath:    "notes/alpha.md",
			LineNumber:  10,
			Heading:     "General",
			FactText:    "standard configuration rule",
			DetailLines: []string{"uses sentinel token inside detail line"},
			AddedDate:   "2026-06-01",
		},
		{
			FilePath:   "notes/alpha.md",
			LineNumber: 11,
			Heading:    "General",
			FactText:   "uses sentinel token inside fact text",
			AddedDate:  "2026-06-01",
		},
		{
			FilePath:   "notes/alpha.md",
			LineNumber: 12,
			Heading:    "Sentinel",
			FactText:   "standard configuration rule",
			AddedDate:  "2026-06-01",
		},
		{
			FilePath:   "notes/sentinel.md",
			LineNumber: 13,
			Heading:    "General",
			FactText:   "standard configuration rule",
			AddedDate:  "2026-06-01",
		},
	}

	scored := RankEntries(entries, "sentinel", now)
	if len(scored) != 4 {
		t.Fatalf("RankEntries('sentinel') count = %d, want 4", len(scored))
	}
	// Expected order: FilePath (line 13, w=2.0) > Heading (line 12, w=1.8) >
	// FactText (line 11, w=1.5) > DetailLines (line 10, w=1.0).
	wantLines := []int{13, 12, 11, 10}
	for i, wantLine := range wantLines {
		if scored[i].Entry.LineNumber != wantLine {
			t.Fatalf("rank[%d] line = %d (score=%f), want line %d; full=%+v",
				i, scored[i].Entry.LineNumber, scored[i].Score, wantLine, scored)
		}
	}

	// Recency boost: identical text & field match, newer AddedDate ranks first.
	recencyEntries := []MemoryEntry{
		{
			FilePath:   "notes/a.md",
			LineNumber: 2,
			Heading:    "Decisions",
			FactText:   "adopt zero-dependency bm25",
			AddedDate:  "2026-01-10",
		},
		{
			FilePath:   "notes/b.md",
			LineNumber: 2,
			Heading:    "Decisions",
			FactText:   "adopt zero-dependency bm25",
			AddedDate:  "2026-06-10",
		},
	}
	recencyScored := RankEntries(recencyEntries, "bm25", now)
	if len(recencyScored) != 2 || recencyScored[0].Entry.FilePath != "notes/b.md" {
		t.Fatalf("recency ranking failed: got %+v, want notes/b.md (newer) first", recencyScored)
	}
}

func TestExtractIndexSummariesAndComputeRelatedFiles(t *testing.T) {
	memRaw := "# Memory\n\n## Index\n" +
		"- [[notes/auth]] — Authentication service rules [project: backend]\n" +
		"- [[notes/db]] - Database schema notes\n" +
		"- [[notes/cache]]: Redis cache policy\n"
	idx := NewLinkIndex([]string{"MEMORY.md", "notes/auth.md", "notes/db.md", "notes/cache.md"})
	memParsed := ParseFile("MEMORY.md", "rev1", []byte(memRaw))
	memParsed.ResolveLinks(idx)

	summaries := ExtractIndexSummaries(memParsed, idx)
	if summaries["notes/auth.md"] != "Authentication service rules" {
		t.Fatalf("auth summary = %q, want %q", summaries["notes/auth.md"], "Authentication service rules")
	}
	if summaries["notes/db.md"] != "Database schema notes" {
		t.Fatalf("db summary = %q, want %q", summaries["notes/db.md"], "Database schema notes")
	}
	if summaries["notes/cache.md"] != "Redis cache policy" {
		t.Fatalf("cache summary = %q, want %q", summaries["notes/cache.md"], "Redis cache policy")
	}

	outgoing := map[string][]string{
		"MEMORY.md":     {"notes/auth.md", "notes/db.md", "notes/cache.md"},
		"notes/auth.md": {"notes/db.md", "notes/cache.md", "notes/auth.md"},
		"notes/db.md":   {"notes/auth.md"},
	}
	backlinks := map[string][]string{
		"notes/auth.md":  {"MEMORY.md", "notes/db.md", "notes/auth.md"},
		"notes/db.md":    {"MEMORY.md", "notes/auth.md"},
		"notes/cache.md": {"MEMORY.md", "notes/auth.md"},
	}

	related := ComputeRelatedFiles([]string{"notes/auth.md"}, outgoing, backlinks, summaries, true)
	want := []RelatedFile{
		{Path: "notes/cache.md", Relation: RelationOutgoing, Summary: "Redis cache policy"},
		{Path: "notes/db.md", Relation: RelationBoth, Summary: "Database schema notes"},
	}
	if !reflect.DeepEqual(related, want) {
		t.Fatalf("ComputeRelatedFiles(notes/auth.md) = %+v, want %+v", related, want)
	}
}
