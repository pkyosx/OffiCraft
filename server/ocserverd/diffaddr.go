package main

// diffaddr.go — THE AUTHORITY on how one side of a comparison is spelled. Two
// deliberate pre-flight copies judge the same spelling: cli/ocagent/diff.go (a
// separate Go module, no shared import) and the cockpit (frontend/), which
// parses it out of the page URL. The written-down authority is
// bin/tests/fixtures/diff-side-addresses.tsv — 🔴 but NOTHING IN GO READS THAT
// TABLE (only the cockpit's copy is tested against it), so a drift between this
// file and cli/ocagent's is caught by nobody. When they disagree, this file wins.
//
// An accepted address is SAYABLE, never a promise that it still resolves.

import (
	"regexp"
	"strings"
)

const docSidePrefix = "doc:"

// diffBlobSideID is anchored on purpose: a prefix test accepted
// "att-/../../api/version", which a reader building a URL by concatenation lets
// the browser normalise into a different endpoint — the compare screen then
// draws an unrelated response as "before".
var diffBlobSideID = regexp.MustCompile(`^att-[0-9a-f]{12}$`)

// diffAddrSegment is a CHARACTER SET because readers splice each part into a
// URL: excluding "/", "%", "?" and "#" removes the normalisation class outright.
// It is deliberately NOT a list of known kinds — that would be a second
// enumeration that goes stale when a new editable document ships.
var diffAddrSegment = regexp.MustCompile(`^[A-Za-z0-9._:@+-]+$`)

const (
	diffAtCurrent = "current"
	diffAtSeed    = "seed"
)

// diffAtRevision: a revision id travels as its decimal spelling so `at` stays
// ONE string on a frozen wire, not a number-or-word union.
var diffAtRevision = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

// diffDocAddress: At "current" is a LIVE pointer — the same link shows a
// different comparison later.
type diffDocAddress struct {
	Kind  string
	Key   string
	At    string
	Field string
}

type diffSide struct {
	Raw          string
	AttachmentID string
	Doc          *diffDocAddress
}

// parseDiffSide matches the value AS GIVEN, never a trimmed copy: a padded
// address is one no reader can resolve.
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
	if value == "." || value == ".." || !diffAddrSegment.MatchString(value) {
		return "'" + raw + "' has a " + what + " that is not a usable address segment: '" + value + "'"
	}
	return ""
}
