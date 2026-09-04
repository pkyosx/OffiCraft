package main

// authz_admin_role_key_pin_test.go — T-48 / 00076 guard rail.
//
// WHY THIS FILE EXISTS. Migration 00076 renames the member KIND 'assistant' to
// 'staff'. The string "assistant" is also, and unrelatedly, the ROLE_KEY that
// grants admin_agent capability (authz.go: adminRoleKey). The two are different
// axes that happen to spell themselves the same way today:
//
//	domain.go  KindStaff     = "staff"       ← the kind axis, already renamed
//	authz.go   adminRoleKey  = "assistant"   ← the authorization axis, MUST NOT move
//
// A repo-wide substitution of that literal — the obvious way to do a rename this
// size — silently relocates the admin boundary. Nothing else in the tree would
// go red: the two axes are compared against different fields, so every existing
// test keeps passing while `classifyMember` stops recognising the admin.
//
// 🔴 WHY THE EXPECTED VALUE IS ASSEMBLED FROM RUNES INSTEAD OF WRITTEN OUT.
// A test that spells "assistant" as a literal is rewritten by the very
// substitution it is meant to catch, and then passes — it would pin the mutation
// rather than the value. Assembling the bytes keeps the expectation out of reach
// of any textual find-and-replace, which is the only reason for the ugliness
// below. Do not "clean this up" into a string literal.

import "testing"

// wantAdminRoleKey is "assistant", spelled so that no textual substitution of
// that word can reach it. See the file header before changing this.
func wantAdminRoleKey() string {
	return string([]rune{'a', 's', 's', 'i', 's', 't', 'a', 'n', 't'})
}

// TestAdminRoleKeyIsPinnedAgainstTheKindRename is the guard proper: the
// authorization literal must still be the word it was, whatever happened to the
// kind vocabulary in the same package.
func TestAdminRoleKeyIsPinnedAgainstTheKindRename(t *testing.T) {
	if adminRoleKey != wantAdminRoleKey() {
		t.Fatalf("adminRoleKey moved: got %q, want %q.\n"+
			"This is the admin capability boundary, NOT the member kind. If you are "+
			"renaming the kind 'assistant' -> 'staff' (migration 00076), this constant "+
			"is NOT part of that rename — a repo-wide substitution caught it by "+
			"accident. Revert this constant; leave the kind rename in place.",
			adminRoleKey, wantAdminRoleKey())
	}
}

// TestAdminClassificationSurvivesOnRoleKeyAlone pins the BEHAVIOUR rather than
// the constant, so the guard still bites if someone keeps the literal but
// rewires classifyMember onto the kind axis instead.
func TestAdminClassificationSurvivesOnRoleKeyAlone(t *testing.T) {
	admin := &Member{ID: "m-admin", Kind: KindStaff, RoleKey: wantAdminRoleKey()}
	if got := classifyMember(admin); got != principalAdminAgent {
		t.Fatalf("a member whose role_key is the admin role key classified as %q, want %q "+
			"— the admin boundary has moved off role_key", got, principalAdminAgent)
	}

	// The kind axis must NOT confer admin on its own. This is the assertion that
	// fails if someone "helpfully" makes classifyMember look at Kind.
	plain := &Member{ID: "m-plain", Kind: KindStaff, RoleKey: "r-25debddcf5dd"}
	if got := classifyMember(plain); got != principalAgent {
		t.Fatalf("a member with a non-admin role_key classified as %q, want %q "+
			"— kind must not grant admin", got, principalAgent)
	}
}

// TestAdminRoleKeyAndMachineKindAreSeparateAxes pins the third literal in the
// neighbourhood. machineKind is not part of the 00076 rename either, and it is
// checked BEFORE role_key in classifyMember, so a change here silently outranks
// the admin test above.
func TestAdminRoleKeyAndMachineKindAreSeparateAxes(t *testing.T) {
	if machineKind != KindWarden {
		t.Fatalf("machineKind %q no longer equals KindWarden %q — classifyMember's "+
			"first branch and the schema CHECK have drifted apart", machineKind, KindWarden)
	}
	warden := &Member{ID: "m-warden", Kind: KindWarden, RoleKey: wantAdminRoleKey()}
	if got := classifyMember(warden); got != principalMachine {
		t.Fatalf("a warden row classified as %q, want %q — a machine must stay a "+
			"machine regardless of role_key", got, principalMachine)
	}
}
