package main

// diffaddr.go — THE AUTHORITY on how one side of a comparison is spelled.
//
// A comparison is a URL naming two SIDES (T-59, owner 2026-09-03: 「可以指定兩個
// 文件位置，就可以跳出我們這個 diff 的畫面 … 實際上是兩個文件連結」). A side is
// EITHER a stored blob id or one field of a system document at one point in
// time, and nothing else — never a file path, never inline bytes. Links rather
// than copies, so the two sides stay openable on their own and nothing is
// stored twice.
//
// ── WHO IS THE AUTHORITY, AND WHO IS A COPY ─────────────────────────────────
//
// THIS FILE IS THE AUTHORITY. Two other places judge the same spelling and both
// are deliberate pre-flights rather than second opinions:
//
//   * cli/ocagent/diff.go — so a mistyped side costs one local sentence in the
//     member's own vocabulary instead of a round trip whose 400 does not say
//     WHICH of the two arguments to look at. It is a separate Go module, so
//     there is no import to share; the copy is confronted against this one
//     through bin/tests/fixtures/diff-side-addresses.tsv, which BOTH modules'
//     mirror tests read (diffaddr_mirror_test.go here, diff_mirror_test.go
//     there). A drift reddens the copy that drifted, by name.
//   * the cockpit (frontend/) — which parses the same address out of the page
//     URL to render each column. It is a reader of this contract, not a
//     definer of it; when the two disagree, this file wins.
//
// The rule for every reader: an address this file refuses is not a comparison,
// and an address it accepts is SAYABLE — never a promise that it still
// resolves. Whether a blob is still stored or a revision still retained is a
// read-time fact the pair route answers with an honest "this side is gone".

import (
	"regexp"
	"strings"
)

// docSidePrefix marks an argument as a DOCUMENT ADDRESS: `doc:<kind>/<key>/
// <at>/<field>`. Four segments, split on "/" — the one character a kind, key,
// `at` or field may never contain (see diffAddrSegment), which is what makes
// the split unambiguous.
const docSidePrefix = "doc:"

// diffBlobSideID is the SHAPE a minted blob id actually has ("att-" +
// newHexID(12), see resolveChatAttachment) — anchored, so it matches the whole
// string or nothing.
//
// A prefix test is not enough, and the gap is not cosmetic. T-59's independent
// review fed the prefix version "att-" (empty id, the easiest possible typo:
// copying an id and losing the tail) and "att-/../../api/version", and BOTH
// were accepted. The second one is the one that bites: a reader that builds a
// URL by concatenation lets the browser normalise that away to a different
// endpoint entirely, and the compare screen then draws an unrelated response as
// "before" — a confident wrong answer.
var diffBlobSideID = regexp.MustCompile(`^att-[0-9a-f]{12}$`)

// diffAddrSegment constrains the three parts of a DOCUMENT address (kind / key
// / field) by CHARACTER SET rather than by membership in a list.
//
// The character set is the point: each part is spliced into a URL by readers,
// so excluding "/", "%", "?" and "#" removes the normalisation class outright,
// and "."/".." are refused by name because they traverse without containing any
// excluded character.
//
// It is deliberately NOT a list of known kinds. This validator's promise is
// that the address is SAYABLE, not that it resolves. A kind list here would be
// a second copy of an enumeration that goes stale the moment a new editable
// document ships, with nothing to go red.
var diffAddrSegment = regexp.MustCompile(`^[A-Za-z0-9._:@+-]+$`)

// The two reserved values of a document side's `at`.
const (
	diffAtCurrent = "current"
	diffAtSeed    = "seed"
)

// diffAtRevision matches the third form of `at`: a retained revision's id. The
// id is an int64 everywhere else and travels here as its decimal spelling so
// that `at` stays ONE string — a field that is sometimes a number and sometimes
// one of two words would be a union type on a frozen wire.
var diffAtRevision = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

// diffDocAddress is one field of one system document at one point in time.
//
// `at` is one of three things: "current" (the live document — a LIVE pointer,
// so the same link shows a different comparison later), "seed" (the shipped
// default, 初始版本), or a retained revision's id in decimal.
//
// `field` is REQUIRED, and that is a property of the data rather than a choice:
// a revision stores a MAP of fields, not one text.
type diffDocAddress struct {
	Kind  string
	Key   string
	At    string
	Field string
}

// diffSide is one parsed side. Exactly one of AttachmentID / Doc is set.
type diffSide struct {
	Raw          string
	AttachmentID string
	Doc          *diffDocAddress
}

// parseDiffSide judges ONE side and returns the sentence to hand the caller
// when it is not one.
//
// EVERY match is against the value AS GIVEN, never a trimmed copy: a padded
// address is one no reader can resolve, so accepting it here would split "it
// was accepted" from "it will draw", which is the one thing this file exists to
// prevent.
func parseDiffSide(raw string) (diffSide, string) {
	if strings.TrimSpace(raw) == "" {
		return diffSide{}, "a comparison side must name a stored attachment id (att-…) or a document (doc:<kind>/<key>/<at>/<field>)"
	}
	if !strings.HasPrefix(raw, docSidePrefix) {
		if !diffBlobSideID.MatchString(raw) {
			return diffSide{}, "'" + raw + "' is neither a stored attachment id (att- plus 12 hex digits) nor a document address (doc:<kind>/<key>/<at>/<field>)"
		}
		return diffSide{Raw: raw, AttachmentID: raw}, ""
	}
	parts := strings.Split(strings.TrimPrefix(raw, docSidePrefix), "/")
	if len(parts) != 4 {
		return diffSide{}, "'" + raw + "' is not a document address — it is doc:<kind>/<key>/<at>/<field>, where <at> is current, seed or a revision id"
	}
	for i, what := range []string{"kind", "key"} {
		if msg := diffAddrSegmentRefusal(raw, what, parts[i]); msg != "" {
			return diffSide{}, msg
		}
	}
	if msg := diffAddrSegmentRefusal(raw, "field", parts[3]); msg != "" {
		return diffSide{}, msg
	}
	if at := parts[2]; at != diffAtCurrent && at != diffAtSeed && !diffAtRevision.MatchString(at) {
		return diffSide{}, "'" + raw + "' has an <at> of '" + at + "' — it must be " +
			diffAtCurrent + ", " + diffAtSeed + ", or a revision id from list_document_history"
	}
	return diffSide{Raw: raw, Doc: &diffDocAddress{
		Kind: parts[0], Key: parts[1], At: parts[2], Field: parts[3],
	}}, ""
}

func diffAddrSegmentRefusal(raw, what, value string) string {
	if value == "" {
		return "'" + raw + "' leaves its " + what + " empty"
	}
	// "." and ".." pass the character set and traverse anyway, so they are
	// refused by name rather than by pattern.
	if value == "." || value == ".." || !diffAddrSegment.MatchString(value) {
		return "'" + raw + "' has a " + what + " that is not a usable address segment: '" + value + "'"
	}
	return ""
}
