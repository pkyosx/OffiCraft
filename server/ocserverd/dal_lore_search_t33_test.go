package main

// T-33 — hop ②. Every test here asks the same question from a different angle:
// does the answer contain exactly what was asked for, and does it SAY what it
// did? A retrieval bug does not raise; it hands back a plausible set.

import (
	"errors"
	"testing"
)

// t33SearchSeed builds a small store: two subjects.
func t33SearchSeed(t *testing.T, d *DAL) {
	t.Helper()
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	t33Entity(t, d, "e-kyle", "agent", "agent:Kyle")
}

func t33Filed(t *testing.T, d *DAL, id, subject string, mutate func(*LoreEntry)) LoreEntry {
	t.Helper()
	e := t33Entry(id)
	if mutate != nil {
		mutate(&e)
	}
	t33Put(t, d, e)
	if err := d.PutLoreSubject(id, subject); err != nil {
		t.Fatalf("file %s under %s: %v", id, subject, err)
	}
	return e
}

func t33Search(t *testing.T, d *DAL, s LoreSearch) LoreSearchResult {
	t.Helper()
	got, err := d.SearchLore(s)
	if err != nil {
		t.Fatalf("search %+v: %v", s, err)
	}
	return got
}

func t33IDs(r LoreSearchResult) []string {
	out := make([]string, 0, len(r.Hits))
	for _, h := range r.Hits {
		out = append(out, h.Entry.ID)
	}
	return out
}

// 🔴 THE ONLY AXIS. Asking on `subject` returns what is filed there and NOTHING
// else. `lore-b` is the negative control — it exists, it is retrievable, and it
// must not come back; without it a version that returns the whole table passes.
func TestLoreSearchSubjectAxisReturnsOnlyWhatIsFiledUnderIt(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	t33Filed(t, d, "lore-a", "e-repo", nil)
	t33Filed(t, d, "lore-b", "e-kyle", nil)

	got := t33Search(t, d, LoreSearch{SubjectKey: "repo:officraft"})
	if ids := t33IDs(got); len(ids) != 1 || ids[0] != "lore-a" {
		t.Fatalf("subject filter returned %v", ids)
	}
	if got.Total != 1 || got.Truncated {
		t.Fatalf("total/truncated: %d/%v", got.Total, got.Truncated)
	}
}

// 🔴 「這個對象沒有東西」 and 「這個對象不存在」 are different answers and the
// owner ruled they must stay different (rc-455a5d3c308c). Both halves are
// asserted, because either alone would pass a version that always said one.
func TestLoreSearchTellsAnEmptySubjectApartFromAMissingOne(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)

	empty := t33Search(t, d, LoreSearch{SubjectKey: "agent:Kyle"})
	if !empty.SubjectResolved || len(empty.Hits) != 0 || empty.UnresolvedSubject != "" {
		t.Fatalf("a real subject with no entries: %+v", empty)
	}
	missing := t33Search(t, d, LoreSearch{SubjectKey: "repo:no-such-thing"})
	if missing.SubjectResolved || missing.UnresolvedSubject != "repo:no-such-thing" {
		t.Fatalf("a subject that does not exist: %+v", missing)
	}
}

// The `query` filter is a LITERAL substring, and this test pins BOTH halves —
// that it matches, and that it fails on text about the same thing written
// differently. The second half is the honest limitation, asserted so nobody
// later reads this filter as semantic search.
func TestLoreSearchQueryIsLiteralAndSaysNothingAboutMeaning(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	// 🔴 這兩筆把字放在 **heading** 上，而那是這一支現在同時在守的第二件事：
	// 掃描面是 `heading`＋`content`（owner 2026-09-06 `rc-9002654dd81c` 逐字
	// 「同時把搜尋改成掃 heading＋內容」）。換之前掃的是 `trigger`，而列表上顯示
	// 的是 `heading` ⇒ 使用者讀到的那一行搜不到。這兩筆如果改回 trigger，這支測試
	// 會紅在「命中 0 筆」。
	t33Filed(t, d, "lore-x", "e-repo", func(e *LoreEntry) {
		e.Heading = "two blocks disagree about the same fact"
	})
	t33Filed(t, d, "lore-y", "e-repo", func(e *LoreEntry) {
		e.Heading = "the assembler and the roster report different numbers"
	})

	hit := t33Search(t, d, LoreSearch{Query: "DISAGREE"})
	if ids := t33IDs(hit); len(ids) != 1 || ids[0] != "lore-x" {
		t.Fatalf("case-insensitive literal match: %v", ids)
	}
	// 🔴 The two 標題格 above describe the same situation. A literal filter
	// finds one and not the other, and that is the documented limit — if this
	// assertion ever starts failing, somebody made the filter semantic and the
	// wire's `query_match` value became a lie.
	miss := t33Search(t, d, LoreSearch{Query: "disagree"})
	if len(miss.Hits) != 1 {
		t.Fatalf("a literal filter matched something it cannot understand: %v", t33IDs(miss))
	}
}

// 🔴 `query` 只掃標題格（heading）與內容格（content）。第 3、4 格
// （revisit_when / impact）是**刻意**不進來的：「要不要能搜到後果那一格」
// 是沒有人做過的決定，在比對清單裡多加兩格等於替別人把它做掉，而症狀是
// 多出來的 hit —— 跟正確的 hit 長得一模一樣，沒有任何東西會叫。
//
// dal_lore_search.go 的註解一直寫著這條規則，而 2026-09-04 的陰性對照
// 證明**沒有任何測試在看它**：把 e.RevisitWhen 與 e.Impact 加進
// loreEntryMatchesLiteral 的比對清單，整套 go test ./... 仍然 rc=0。
// 這一支就是補上的那道守衛。
func TestLoreSearchQueryDoesNotReachTheThirdOrFourthCell(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)

	// 只有`revisit_when`帶著那個字。
	t33Filed(t, d, "lore-r", "e-repo", func(e *LoreEntry) {
		e.Heading = "標題格沒有那個字"
		e.Content = "內容格也沒有"
		e.RevisitWhen = "zebrafish"
		e.Impact = "`impact`沒有"
	})
	// 只有`impact`帶著那個字。
	t33Filed(t, d, "lore-p", "e-repo", func(e *LoreEntry) {
		e.Heading = "標題格一樣沒有"
		e.Content = "內容格一樣沒有"
		e.RevisitWhen = "`revisit_when`沒有"
		e.Impact = "zebrafish"
	})
	// 🔑 陽性對照：同一個字放進`content`就搜得到。少了它，下面的「只命中一筆」
	// 也可能是因為查法整個失效 —— 零命中與規則成立長得一模一樣。
	t33Filed(t, d, "lore-c", "e-repo", func(e *LoreEntry) {
		e.Heading = "標題格沒有那個字"
		e.Content = "zebrafish 在內容格"
		e.RevisitWhen = "`revisit_when`沒有"
		e.Impact = "`impact`沒有"
	})

	got := t33Search(t, d, LoreSearch{Query: "zebrafish"})
	ids := t33IDs(got)
	if len(ids) != 1 || ids[0] != "lore-c" {
		t.Fatalf("query 掃到了第 3 或`impact`（只有內容格那一筆該命中）: %v", ids)
	}
}

// A human origin sorts ahead within its tier AND survives the count cap — what
// a person said is not competing with what an agent worked out.
func TestLoreSearchKeepsWhatAPersonSaidAheadAndUncapped(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	for _, id := range []string{"lore-m1", "lore-m2", "lore-m3"} {
		t33Filed(t, d, id, "e-repo", nil)
	}
	t33Filed(t, d, "lore-human", "e-repo", func(e *LoreEntry) {
		e.Origin = "human:Seth"
		e.CreatedTS = 9999 // newest, so only the origin rule can put it first
	})

	got := t33Search(t, d, LoreSearch{SubjectKey: "repo:officraft", Limit: 1})
	ids := t33IDs(got)
	if len(ids) == 0 || ids[0] != "lore-human" {
		t.Fatalf("a human origin did not sort first: %v", ids)
	}
	if len(ids) != 2 {
		t.Fatalf("limit 1 kept %d hits; want the human one plus one agent one", len(ids))
	}
	if got.Total != 4 || !got.Truncated {
		t.Fatalf("total/truncated: %d/%v", got.Total, got.Truncated)
	}
}

// Retired entries are never retrieved — the same predicate the directory and
// the per-subject list use.
func TestLoreSearchNeverReturnsARetiredEntry(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	t33Filed(t, d, "lore-live", "e-repo", nil)
	t33Filed(t, d, "lore-gone", "e-repo", nil)
	if err := d.RetireLoreEntry("lore-gone", LoreRetireExpired, "m-x", "", false, 500); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if ids := t33IDs(t33Search(t, d, LoreSearch{SubjectKey: "repo:officraft"})); len(ids) != 1 || ids[0] != "lore-live" {
		t.Fatalf("retired entry reachable through search: %v", ids)
	}
}

// No axis at all is a legitimate question — "what is in here" — and it must
// return everything rather than nothing. This is the other side of
// TestLoreSearchSubjectAxisReturnsOnlyWhatIsFiledUnderIt: that one pins that the
// filter FILTERS, this one pins that an absent filter does not.
func TestLoreSearchWithNoAxisReturnsEverything(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	t33Filed(t, d, "lore-a", "e-repo", nil)
	t33Filed(t, d, "lore-b", "e-kyle", nil)

	got := t33Search(t, d, LoreSearch{})
	if ids := t33IDs(got); len(ids) != 2 {
		t.Fatalf("no-axis search returned %v, want both entries", ids)
	}
}

// An out-of-range limit is refused rather than clamped: a caller that asked for
// 500 and silently got 100 believes it has seen everything.
func TestLoreSearchRefusesAnOutOfRangeLimitRatherThanClampingIt(t *testing.T) {
	d := newTestDAL(t)
	for _, n := range []int{-1, loreSearchLimitMax + 1} {
		if _, err := d.SearchLore(LoreSearch{Limit: n}); !errors.Is(err, ErrLoreSearchLimitRange) {
			t.Fatalf("limit %d: got %v", n, err)
		}
	}
}

// A subject key that resolves through an alias, and one whose subject was
// merged away, both reach the surviving subject — the same rule the write path
// applies, asserted here so the two cannot drift apart.
func TestLoreSearchResolvesAliasesAndFollowsMergesLikeTheWritePathDoes(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	t33Filed(t, d, "lore-a", "e-repo", nil)
	if _, err := d.wdb.Exec(
		`INSERT INTO entity_alias (alias, entity_id) VALUES ('repo:oc', 'e-repo')`); err != nil {
		t.Fatalf("alias: %v", err)
	}
	t33Entity(t, d, "e-old", "repo", "repo:oldname")
	if _, err := d.wdb.Exec(`UPDATE entity SET merged_into = 'e-repo' WHERE id = 'e-old'`); err != nil {
		t.Fatalf("merge: %v", err)
	}
	for _, key := range []string{"repo:oc", "repo:oldname"} {
		if ids := t33IDs(t33Search(t, d, LoreSearch{SubjectKey: key})); len(ids) != 1 || ids[0] != "lore-a" {
			t.Fatalf("key %q resolved to %v", key, ids)
		}
	}
}
