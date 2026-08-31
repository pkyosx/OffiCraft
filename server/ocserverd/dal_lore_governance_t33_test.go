package main

// T-33 — retiring an entry, reviving one, and the journal both leave behind.
//
// Each test below guards ONE claim and carries its own positive control, because
// a refusal test that would also pass against a function that refuses everything
// proves nothing. Where the control is the interesting half it is named in the
// test's own comment.

import (
	"errors"
	"testing"
)

func t33RetireEntry(t *testing.T, d *DAL, id string) LoreEntry {
	t.Helper()
	e := t33Entry(id)
	t33Put(t, d, e)
	return e
}

func t33Status(t *testing.T, d *DAL, id string) string {
	t.Helper()
	got := t33Get(t, d, id)
	if got == nil {
		t.Fatalf("entry %s vanished", id)
	}
	return got.Status
}

// An ordinary agent may park an entry as expired or merged, and MAY NOT declare
// one false. The positive control is the first half: without it this test would
// still pass against a RetireLoreEntry that refused every agent write.
func TestLoreRetireReasonDecidesWhoMayFileIt(t *testing.T) {
	d := newTestDAL(t)
	for _, reason := range []string{LoreRetireExpired, LoreRetireMerged} {
		id := "e-agent-" + reason
		t33RetireEntry(t, d, id)
		if err := d.RetireLoreEntry(id, reason, "agent:O-197", "", false, 200); err != nil {
			t.Fatalf("agent retire as %s: want allowed, got %v", reason, err)
		}
		if got := t33Status(t, d, id); got != "retired" {
			t.Fatalf("agent retire as %s: status = %q, want retired", reason, got)
		}
	}

	t33RetireEntry(t, d, "e-false")
	err := d.RetireLoreEntry("e-false", LoreRetireFalsified, "agent:O-197", "", false, 200)
	if !errors.Is(err, ErrLoreRetireOwnerOnly) {
		t.Fatalf("agent retire as falsified: want ErrLoreRetireOwnerOnly, got %v", err)
	}
	// The refusal has to leave the world untouched, not merely return an error.
	if got := t33Status(t, d, "e-false"); got != "active" {
		t.Fatalf("refused retire still changed status to %q", got)
	}
	if evs := t33Events(t, d, "e-false"); len(evs) != 0 {
		t.Fatalf("refused retire wrote %d journal rows, want 0", len(evs))
	}
	// Control: the SAME call from the owner is allowed, so the refusal above is
	// about who is asking and not about the reason being rejected outright.
	if err := d.RetireLoreEntry("e-false", LoreRetireFalsified, "owner", "", true, 300); err != nil {
		t.Fatalf("owner retire as falsified: want allowed, got %v", err)
	}
	if got := t33Status(t, d, "e-false"); got != "retired" {
		t.Fatalf("owner retire as falsified: status = %q, want retired", got)
	}
}

// An unrecognised reason is refused rather than quietly treated as the
// permissive kind — a typo must not be able to retire something as if it were
// merely stale.
func TestLoreRetireRefusesAnUnknownReason(t *testing.T) {
	d := newTestDAL(t)
	t33RetireEntry(t, d, "e-typo")
	err := d.RetireLoreEntry("e-typo", "falsifed", "owner", "", true, 200)
	if !errors.Is(err, ErrLoreRetireReasonUnknown) {
		t.Fatalf("typo reason: want ErrLoreRetireReasonUnknown, got %v", err)
	}
	if got := t33Status(t, d, "e-typo"); got != "active" {
		t.Fatalf("refused retire still changed status to %q", got)
	}
	// Control: the correctly spelled reason from the same actor goes through.
	if err := d.RetireLoreEntry("e-typo", LoreRetireFalsified, "owner", "", true, 200); err != nil {
		t.Fatalf("spelled correctly: want allowed, got %v", err)
	}
}

// The status change and the journal row are one act. This is the test that would
// go red if the two were ever split apart and only one of them ran.
func TestLoreRetireRecordsWhoWhenWhyAndReplacedBy(t *testing.T) {
	d := newTestDAL(t)
	t33RetireEntry(t, d, "e-merged")
	if err := d.RetireLoreEntry(
		"e-merged", LoreRetireMerged, "agent:O-197", "e-survivor", false, 4242); err != nil {
		t.Fatalf("retire: %v", err)
	}
	ev, err := d.LatestLoreGovernanceEvent("e-merged")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if ev == nil {
		t.Fatal("retire left no governance event")
	}
	if ev.Kind != LoreGovRetire || ev.Reason != LoreRetireMerged ||
		ev.ActorID != "agent:O-197" || ev.ReplacedBy != "e-survivor" || ev.CreatedTS != 4242 {
		t.Fatalf("journal row = %+v", *ev)
	}
	// Control: an entry nobody has touched has no event, so a non-nil answer
	// above is evidence the retire wrote one rather than evidence the reader
	// returns something for everything.
	other, err := d.LatestLoreGovernanceEvent("e-never-touched")
	if err != nil {
		t.Fatalf("latest on untouched: %v", err)
	}
	if other != nil {
		t.Fatalf("untouched entry reported event %+v", *other)
	}
}

// Retiring an already-retired entry for a different reason is a real second act,
// and BOTH reasons survive. A reason column on the entry could not do this, which
// is why there isn't one.
func TestLoreRetireTwiceKeepsBothReasons(t *testing.T) {
	d := newTestDAL(t)
	t33RetireEntry(t, d, "e-twice")
	if err := d.RetireLoreEntry("e-twice", LoreRetireExpired, "agent:O-197", "", false, 100); err != nil {
		t.Fatalf("first retire: %v", err)
	}
	if err := d.RetireLoreEntry("e-twice", LoreRetireFalsified, "owner", "", true, 200); err != nil {
		t.Fatalf("second retire: %v", err)
	}
	evs := t33Events(t, d, "e-twice")
	if len(evs) != 2 {
		t.Fatalf("want 2 journal rows, got %d", len(evs))
	}
	if evs[0].Reason != LoreRetireExpired || evs[1].Reason != LoreRetireFalsified {
		t.Fatalf("reasons out of order or lost: %q then %q", evs[0].Reason, evs[1].Reason)
	}
}

// Reviving is what lets retirement be called reversible at all. Owner only, and
// the control is that the same call from an agent is refused — otherwise this
// test would pass against a revive that let anyone through.
func TestLoreReviveIsOwnerOnlyAndBringsTheEntryBack(t *testing.T) {
	d := newTestDAL(t)
	t33RetireEntry(t, d, "e-back")
	if err := d.RetireLoreEntry("e-back", LoreRetireExpired, "agent:O-197", "", false, 100); err != nil {
		t.Fatalf("retire: %v", err)
	}

	err := d.ReviveLoreEntry("e-back", "agent:O-197", "the situation came back", false, 200)
	if !errors.Is(err, ErrLoreReviveOwnerOnly) {
		t.Fatalf("agent revive: want ErrLoreReviveOwnerOnly, got %v", err)
	}
	if got := t33Status(t, d, "e-back"); got != "retired" {
		t.Fatalf("refused revive changed status to %q", got)
	}

	if err := d.ReviveLoreEntry("e-back", "owner", "the situation came back", true, 300); err != nil {
		t.Fatalf("owner revive: %v", err)
	}
	if got := t33Status(t, d, "e-back"); got != "active" {
		t.Fatalf("after revive status = %q, want active", got)
	}
	ev, err := d.LatestLoreGovernanceEvent("e-back")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if ev == nil || ev.Kind != LoreGovRevive {
		t.Fatalf("revive left %+v, want a revive event", ev)
	}
}

// Reviving something that was never retired is refused, because answering "done"
// would confirm a belief about the entry's state that is wrong.
func TestLoreReviveRefusesAnEntryThatIsNotRetired(t *testing.T) {
	d := newTestDAL(t)
	t33RetireEntry(t, d, "e-active")
	err := d.ReviveLoreEntry("e-active", "owner", "", true, 200)
	if !errors.Is(err, ErrLoreEntryNotRetired) {
		t.Fatalf("revive an active entry: want ErrLoreEntryNotRetired, got %v", err)
	}
	if evs := t33Events(t, d, "e-active"); len(evs) != 0 {
		t.Fatalf("refused revive wrote %d journal rows, want 0", len(evs))
	}
}

// A retire or revive aimed at an id nothing carries is an error, not a silent
// success that leaves a journal row pointing at nothing.
func TestLoreGovernanceRefusesAnUnknownEntry(t *testing.T) {
	d := newTestDAL(t)
	if err := d.RetireLoreEntry("e-ghost", LoreRetireExpired, "agent:O-197", "", false, 200); !errors.Is(err, ErrLoreEntryUnknown) {
		t.Fatalf("retire a ghost: want ErrLoreEntryUnknown, got %v", err)
	}
	if err := d.ReviveLoreEntry("e-ghost", "owner", "", true, 200); !errors.Is(err, ErrLoreEntryUnknown) {
		t.Fatalf("revive a ghost: want ErrLoreEntryUnknown, got %v", err)
	}
	if evs := t33Events(t, d, "e-ghost"); len(evs) != 0 {
		t.Fatalf("refused acts on a ghost wrote %d journal rows, want 0", len(evs))
	}
	// Control: the same calls against a real entry succeed, so the refusals above
	// are about the id and not about the calls being broken.
	t33RetireEntry(t, d, "e-real")
	if err := d.RetireLoreEntry("e-real", LoreRetireExpired, "agent:O-197", "", false, 200); err != nil {
		t.Fatalf("retire a real entry: %v", err)
	}
}

// Retired entries stop being retrieved by subject, and reviving puts them back —
// the two halves together are what "reversible" means in practice.
func TestLoreRetireAndReviveMoveTheEntryOutOfAndBackIntoRetrieval(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "ent-1", "repo", "repo:OffiCraft")
	t33RetireEntry(t, d, "e-fold")
	if err := d.PutLoreSubject("e-fold", "ent-1"); err != nil {
		t.Fatalf("subject: %v", err)
	}
	if n := t33CountBySubject(t, d, "ent-1"); n != 1 {
		t.Fatalf("before retire count = %d, want 1", n)
	}
	if err := d.RetireLoreEntry("e-fold", LoreRetireExpired, "agent:O-197", "", false, 200); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if n := t33CountBySubject(t, d, "ent-1"); n != 0 {
		t.Fatalf("after retire count = %d, want 0", n)
	}
	if err := d.ReviveLoreEntry("e-fold", "owner", "", true, 300); err != nil {
		t.Fatalf("revive: %v", err)
	}
	if n := t33CountBySubject(t, d, "ent-1"); n != 1 {
		t.Fatalf("after revive count = %d, want 1 — retirement was not reversible", n)
	}
}

func t33Events(t *testing.T, d *DAL, target string) []LoreGovernanceEvent {
	t.Helper()
	evs, err := d.ListLoreGovernanceEvents(target)
	if err != nil {
		t.Fatalf("list events for %s: %v", target, err)
	}
	return evs
}

func t33CountBySubject(t *testing.T, d *DAL, entityID string) int {
	t.Helper()
	n, err := d.CountLoreEntriesBySubject(entityID)
	if err != nil {
		t.Fatalf("count by subject %s: %v", entityID, err)
	}
	return n
}
