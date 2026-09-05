package main

// dal_lore_search.go — T-33, the retrieval hop. The design calls it hop ②: an
// agent that has seen the subject directory and wants to know what is actually
// filed under one of those subjects.
//
// 🔴 WHY THE SELECTION CONDITIONS ARE NOT A FREE-FOR-ALL. This is the hop whose
// entire value IS the selection: getting it wrong does not raise an error, it
// hands an agent a set of memories that is not the set it asked for, and the
// symptom of that is "somebody forgot something today" — indistinguishable from
// having got dumber. So every condition this layer applies is reported back in
// the result, and every condition it CANNOT apply is refused at the door rather
// than quietly ignored.
//
// 🔴 WHAT THIS LAYER DELIBERATELY DOES NOT DO:
//   - It does not MINT a subject. Writing invents subjects; reading must not.
//     A subject key that names nothing comes back as "unresolved", which is a
//     different answer from "resolved, and it has no entries" — the owner ruled
//     (rc-455a5d3c308c) that telling those two apart is the requirement.
//   - It does not pretend `heading` is a semantic axis. It is returned and
//     it is not a query parameter, because it has no table and no index, and
//     because two people writing the same symptom were measured to produce text
//     with almost no words in common — a literal match over that column would
//     report them as two different things while looking like it worked.

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrLoreSearchLimitRange   = errors.New("lore: `limit` is out of range")
	ErrLoreSearchSubjectBlank = errors.New("lore: the subject filter is blank")
)

const (
	loreSearchLimitDefault = 20
	loreSearchLimitMax     = 100
)

// 🔴 THERE IS NO TIER HERE ANY MORE, AND THAT IS OWNER'S RULING OF 2026-09-05,
// NOT A SIMPLIFICATION. T1/T2 was computed as `matched == askedAxes`, over the
// two axes subject × action. With `action` gone there is exactly ONE axis, so
// `matched < askedAxes` can never hold and T2 can never be produced: the label
// would have been a field with one possible value, which reads like a judgement
// and makes none.
//
// ⚠️ WHAT WENT WITH IT, said plainly so nobody looks for it: the CROSS-SUBJECT
// WALL. A `trust`-class entry used to be withheld from the analogy tier, and the
// class itself was derived from the action names. Both halves of that rule lost
// their input. Nothing guards it today.

// LoreSearch is one retrieval request. Every field is optional; the zero value
// asks for "everything that is still retrievable", which is a legitimate and
// occasionally correct question.
//
// 🔴 THERE IS NO `context_labels` FIELD, AND ITS ABSENCE IS DELIBERATE. The
// detail design lists that parameter in two tables and NEVER SAYS WHAT IT IS
// COMPARED AGAINST — there is no definition anywhere in the document. Guessing
// it would produce a selection condition whose behaviour nobody specified, and a
// wrong selection condition is invisible: the caller gets memories, they are the
// wrong memories, and nothing reports it. Leaving it out fails the other way and
// fails LOUDLY: the request DTO is a closed set, so a caller that sends
// `context_labels` gets a 422 naming the field instead of a quietly different
// answer.
type LoreSearch struct {
	SubjectKey string
	Query      string
	Limit      int
}

// LoreSearchHit is one entry as retrieval sees it: the row and its ONE axis
// spelled out.
type LoreSearchHit struct {
	Entry    LoreEntry
	Subjects []string // canonical subject keys, not entity ids

	humanOrigin bool
}

// LoreSearchResult carries the hits AND everything the caller needs to tell a
// real empty answer from a question that never got asked properly.
type LoreSearchResult struct {
	Hits      []LoreSearchHit
	Total     int
	Truncated bool

	// SubjectResolved is false when a subject key was given and named nothing.
	// 🔴 IT IS NOT FOLDED INTO "zero hits". Those are different facts and the
	// owner ruled that they must stay different: "this subject has nothing on
	// it" is an answer, "this subject does not exist" is a typo.
	SubjectResolved   bool
	UnresolvedSubject string

	// SubjectEntityID is the entity the subject key actually resolved ONTO —
	// after an alias was followed and after a merged-away subject was chased to
	// its survivor. It is not on the wire; it exists so the recall journal can
	// file the subject that was really searched rather than the string the
	// caller happened to type, which is the only form two callers asking the
	// same question can be recognised as having done so.
	SubjectEntityID string
}

// SearchLore runs hop ②.
func (d *DAL) SearchLore(s LoreSearch) (LoreSearchResult, error) {
	var out LoreSearchResult
	out.SubjectResolved = true

	if s.Limit == 0 {
		s.Limit = loreSearchLimitDefault
	}
	if s.Limit < 1 || s.Limit > loreSearchLimitMax {
		return out, fmt.Errorf("%w: %d (1..%d)", ErrLoreSearchLimitRange, s.Limit, loreSearchLimitMax)
	}
	wantSubject := strings.TrimSpace(s.SubjectKey) != ""
	if strings.TrimSpace(s.SubjectKey) == "" && s.SubjectKey != "" {
		return out, ErrLoreSearchSubjectBlank
	}

	var subjectEntity string
	if wantSubject {
		id, err := d.loreEntityIDForKey(s.SubjectKey)
		if err != nil {
			return out, err
		}
		if id == "" {
			out.SubjectResolved = false
			out.UnresolvedSubject = s.SubjectKey
			return out, nil
		}
		subjectEntity = id
		out.SubjectEntityID = id
	}

	rows, err := d.rdb.Query(`
		SELECT ` + loreEntryColumns + ` FROM lore_entry WHERE status <> 'retired'
		ORDER BY created_ts, id`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	var all []LoreEntry
	for rows.Next() {
		e, err := scanLoreEntry(rows)
		if err != nil {
			return out, err
		}
		all = append(all, e)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	needle := strings.ToLower(strings.TrimSpace(s.Query))

	var hits []LoreSearchHit
	for _, e := range all {
		subjectKeys, subjectIDs, err := d.loreSubjectKeys(e.ID)
		if err != nil {
			return out, err
		}
		subjectHit := false
		for _, id := range subjectIDs {
			if id == subjectEntity {
				subjectHit = true
				break
			}
		}
		// There is ONE axis now. An entry reaches the answer when it intersects
		// the subject the caller asked on; asking on nothing asks for everything.
		if wantSubject && !subjectHit {
			continue
		}
		if needle != "" && !loreEntryMatchesLiteral(e, needle) {
			continue
		}

		hits = append(hits, LoreSearchHit{
			Entry: e, Subjects: subjectKeys,
			humanOrigin: strings.HasPrefix(e.Origin, "human:"),
		})
	}

	sortLoreHits(hits)
	out.Total = len(hits)
	out.Hits, out.Truncated = loreHitsWithinLimit(hits, s.Limit)
	return out, nil
}

// loreEntryMatchesLiteral is the `query` filter, and it is a LITERAL,
// case-insensitive substring over 標題格 (`heading`) and 內容格 (`content`).
//
// 🔴 IT IS NOT SEMANTIC AND MUST NOT BE DESCRIBED AS SEARCH. Two entries about
// the same thing were measured to write their 標題 with almost no words in
// common, so a literal filter reports them as unrelated while looking exactly
// like a filter that worked. The wire says which kind it is (`query_match`)
// precisely so a caller never has to guess.
//
// 🔴 掃描面是 `heading`，不是 `trigger`，而那是這一支修掉的 bug 的一半。
// 這段註解以前逐字寫著「in 五格 the name and the axis are both `trigger`」——
// 那句話從 `rc-9002654dd81c`（2026-09-06 owner 逐字「合併成 heading 一格（同時把
// 搜尋改成掃 heading＋內容、待審畫面改顯示 heading）」）起是假的，所以就地改掉
// 而不是加註：兩句相反的話擺在同一段裡，讀的人只會挑一句信。
// ⚠️ 合併前這裡掃的是 `trigger`＋`content`，而列表上顯示的是 `heading` ⇒
// **使用者看到的那一行，搜尋搜不到**，而且搜不到跟「站上真的沒有這條」長得一模
// 一樣。名字與軸現在是同一格，所以那個落差在構造上消失了，不是被補起來的。
//
// 🔴 第 3、4、5 格 (`retire_when`, `impact`, events) are deliberately NOT added:
// making them searchable would widen what `query` answers, and 「要不要能搜到
// 影響那一格」 is a decision nobody has made. Widening it here would make it by
// accident, and the symptom would be extra hits that look exactly like correct
// ones.
func loreEntryMatchesLiteral(e LoreEntry, lowerNeedle string) bool {
	for _, f := range []string{e.Heading, e.Content} {
		if strings.Contains(strings.ToLower(f), lowerNeedle) {
			return true
		}
	}
	return false
}

// sortLoreHits puts the answer in the order the design specifies.
func sortLoreHits(hits []LoreSearchHit) {
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.humanOrigin != b.humanOrigin {
			return a.humanOrigin // a human origin sorts ahead
		}
		an, bn := len(a.Subjects), len(b.Subjects)
		if an != bn {
			return an < bn // fewer of its own tags first: it is more specific
		}
		if a.Entry.CreatedTS != b.Entry.CreatedTS {
			return a.Entry.CreatedTS < b.Entry.CreatedTS
		}
		return a.Entry.ID < b.Entry.ID
	})
}

// loreHitsWithinLimit applies the count cap.
//
// 🔴 A HUMAN-ORIGIN ENTRY IS EXEMPT FROM THE CAP, and that is the design's rule
// rather than a convenience: what a person told an agent is not competing with
// what the agent worked out for itself. The exemption is applied AFTER sorting
// so the order is unaffected by it — the cap decides who is dropped, never who
// is first.
func loreHitsWithinLimit(hits []LoreSearchHit, limit int) ([]LoreSearchHit, bool) {
	if len(hits) <= limit {
		return hits, false
	}
	kept := make([]LoreSearchHit, 0, limit)
	budget := limit
	for _, h := range hits {
		if h.humanOrigin {
			kept = append(kept, h)
			continue
		}
		if budget > 0 {
			kept = append(kept, h)
			budget--
		}
	}
	return kept, len(kept) < len(hits)
}

// loreEntityIDForKey resolves a subject key to an entity id WITHOUT creating
// one, following an alias and a merge exactly as the write path does. It answers
// "" — never an error — when the key names nothing, because that is a fact about
// the request rather than a fault.
func (d *DAL) loreEntityIDForKey(key string) (string, error) {
	var id string
	err := d.rdb.QueryRow(`SELECT id FROM entity WHERE canonical = ?`, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = d.rdb.QueryRow(`SELECT entity_id FROM entity_alias WHERE alias = ?`, key).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	seen := map[string]bool{id: true}
	for hop := 0; hop < 8; hop++ {
		var into string
		if err := d.rdb.QueryRow(`SELECT merged_into FROM entity WHERE id = ?`, id).Scan(&into); err != nil {
			return "", err
		}
		if into == "" {
			return id, nil
		}
		if seen[into] {
			return "", fmt.Errorf("%w: %q", ErrLoreEntityMergeCycle, key)
		}
		seen[into] = true
		id = into
	}
	return "", fmt.Errorf("%w: %q", ErrLoreEntityMergeCycle, key)
}

// loreSubjectKeys returns one entry's subjects as CANONICAL KEYS plus their
// entity ids. The keys are what a reader can act on; the ids are what the
// matching compares.
func (d *DAL) loreSubjectKeys(entryID string) ([]string, []string, error) {
	rows, err := d.rdb.Query(`
		SELECT n.canonical, n.id FROM lore_subject s
		JOIN entity n ON n.id = s.entity_id
		WHERE s.entry_id = ? ORDER BY n.canonical, n.id`, entryID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var keys, ids []string
	for rows.Next() {
		var k, id string
		if err := rows.Scan(&k, &id); err != nil {
			return nil, nil, err
		}
		keys = append(keys, k)
		ids = append(ids, id)
	}
	return keys, ids, rows.Err()
}
