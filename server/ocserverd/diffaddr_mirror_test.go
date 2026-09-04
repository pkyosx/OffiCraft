package main

// diffaddr_mirror_test.go — the AUTHORITY's half of the comparison-address
// mirror confrontation (T-59).
//
// diffaddr.go defines how one side of a comparison is spelled; cli/ocagent
// carries a hand-transcribed pre-flight copy of the same rule (a separate Go
// module, no import path between them), and the cockpit carries a third in
// TypeScript. All three are driven against
// bin/tests/fixtures/diff-side-addresses.tsv rather than against each other, so
// a drift reddens the copy that drifted BY NAME. The fixture's header carries
// the full argument and names all three readers.

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

const diffAddrFixturePath = "../../bin/tests/fixtures/diff-side-addresses.tsv"

type diffAddrRow struct {
	line    int
	address string
	ok      bool
	about   string
}

// loadDiffAddrRows parses the shared table. A parse failure is FATAL, never a
// skip: a mirror guard that quietly passes when it cannot find its own fixture
// is worse than no guard, because the green tick then means nothing was checked.
func loadDiffAddrRows(t *testing.T) []diffAddrRow {
	t.Helper()
	f, err := os.Open(diffAddrFixturePath)
	if err != nil {
		t.Fatalf("open %s: %v — the shared table is the ONLY thing keeping cli/ocagent's "+
			"copy aligned with this file; if it moved, fix the path, do not delete the test",
			diffAddrFixturePath, err)
	}
	defer f.Close()

	var rows []diffAddrRow
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			t.Fatalf("%s:%d: want 3 tab-separated fields, got %d", diffAddrFixturePath, n, len(parts))
		}
		var ok bool
		switch parts[1] {
		case "ok":
			ok = true
		case "bad":
		default:
			t.Fatalf("%s:%d: second field must be ok|bad, got %q", diffAddrFixturePath, n, parts[1])
		}
		rows = append(rows, diffAddrRow{
			line:    n,
			address: strings.ReplaceAll(parts[0], "<space>", " "),
			ok:      ok,
			about:   parts[2],
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", diffAddrFixturePath, err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s carries no rows — a table that says nothing passes everything", diffAddrFixturePath)
	}
	return rows
}

func TestParseDiffSideMatchesTheSharedAddressTable(t *testing.T) {
	for _, row := range loadDiffAddrRows(t) {
		_, msg := parseDiffSide(row.address)
		switch {
		case row.ok && msg != "":
			t.Errorf("%s:%d: %q should be sayable (%s) but this server refuses it: %s",
				diffAddrFixturePath, row.line, row.address, row.about, msg)
		case !row.ok && msg == "":
			t.Errorf("%s:%d: %q should be refused (%s) but this server accepts it",
				diffAddrFixturePath, row.line, row.address, row.about)
		}
	}
}

// A refusal has to say WHAT is wrong, not merely that something is: the member
// reading it has two arguments and no other clue which one to look at.
func TestParseDiffSideRefusalNamesTheAddress(t *testing.T) {
	for _, row := range loadDiffAddrRows(t) {
		if row.ok {
			continue
		}
		if _, msg := parseDiffSide(row.address); !strings.Contains(msg, strings.TrimSpace(row.address)) &&
			!strings.Contains(msg, "must name") {
			t.Errorf("%s:%d: refusal of %q never quotes it: %s",
				diffAddrFixturePath, row.line, row.address, msg)
		}
	}
}
