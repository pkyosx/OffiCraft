package main

// diff_mirror_test.go — ocagent's half of the comparison-address mirror
// confrontation (T-59).
//
// diff.go's sideRefusal is a HAND-TRANSCRIBED copy of
// server/ocserverd/diffaddr.go, which is the authority; the two are separate Go
// modules with no import path between them. Both are driven against
// bin/tests/fixtures/diff-side-addresses.tsv rather than against each other, so
// a drift reddens THIS copy by name rather than merely reporting that two
// copies differ. The fixture's header carries the full argument.
//
// The second confrontation below is the PAGE URL: this module builds the
// internal link itself (no request), so it also copies the page path and the
// four parameter names from server/ocserverd/api_diff.go. That copy is checked
// against that file's SOURCE, because a Go module boundary is exactly what
// these tests exist to reach across.

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

const (
	diffAddrFixturePath = "../../bin/tests/fixtures/diff-side-addresses.tsv"
	diffServerSourceDir = "../../server/ocserverd/"
)

type diffAddrRow struct {
	line    int
	address string
	ok      bool
	about   string
}

// loadDiffAddrRows parses the shared table. A parse failure is FATAL, never a
// skip: a mirror guard that quietly passes when it cannot find its own fixture
// is worse than no guard.
func loadDiffAddrRows(t *testing.T) []diffAddrRow {
	t.Helper()
	f, err := os.Open(diffAddrFixturePath)
	if err != nil {
		t.Fatalf("open %s: %v — the shared table is the ONLY thing keeping this copy "+
			"aligned with server/ocserverd/diffaddr.go; if it moved, fix the path, "+
			"do not delete the test", diffAddrFixturePath, err)
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

func TestSideRefusalMatchesTheSharedAddressTable(t *testing.T) {
	for _, row := range loadDiffAddrRows(t) {
		msg := sideRefusal(row.address, "before")
		switch {
		case row.ok && msg != "":
			t.Errorf("%s:%d: %q is sayable to the server (%s) but this CLI refuses it: %s",
				diffAddrFixturePath, row.line, row.address, row.about, msg)
		case !row.ok && msg == "":
			t.Errorf("%s:%d: %q is refused by the server (%s) but this CLI accepts it — "+
				"the member would learn that from a round trip that cannot say which "+
				"argument was wrong", diffAddrFixturePath, row.line, row.address, row.about)
		}
	}
}

// The page URL is minted by the server for the external flavour and built HERE
// for the internal one, so the two must spell it identically or the CLI prints
// a link the cockpit cannot read.
func TestPageURLSpellingMatchesTheServersOwn(t *testing.T) {
	source, err := os.ReadFile(diffServerSourceDir + "api_diff.go")
	if err != nil {
		t.Fatalf("read the server's api_diff.go: %v — it is the authority on the page "+
			"URL this file copies; if it moved, fix the path, do not delete the test", err)
	}
	// gofmt aligns a const block with padding, so the comparison is made on a
	// whitespace-normalised copy rather than on the bytes.
	normalised := strings.Join(strings.Fields(string(source)), " ")
	for name, want := range map[string]string{
		"diffPagePath":        diffPagePath,
		"diffParamBefore":     diffParamBefore,
		"diffParamAfter":      diffParamAfter,
		"diffParamLabelBefor": diffParamLabelBefor,
		"diffParamLabelAfter": diffParamLabelAfter,
	} {
		decl := name + " = \"" + want + "\""
		if !strings.Contains(normalised, decl) {
			t.Errorf("server/ocserverd/api_diff.go does not declare %s — this module "+
				"builds the internal link from that spelling, so the two links would "+
				"disagree", decl)
		}
	}
}
