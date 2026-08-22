package main

// boot_doc_registry_mirror_test.go — the server's half of the boot-document
// registry mirror (T-3201). The twin is
// frontend/src/api/mock.boot-doc-registry.test.ts and the reasoning lives in
// bin/tests/fixtures/boot-doc-registry.tsv.
//
// bootDocRegistry is the AUTHORITY: it is what the server actually serves. The
// cockpit carries its own list so it can give each document a row with a name
// and an icon, and there is no compiler between the two. This half pins the
// authority to the shared table; the other half pins the cockpit to the same
// table. A document added to one side and not the other is then a red test on
// the side that was not updated, rather than a document the owner can never see.

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

const bootDocRegistryFixturePath = "../../bin/tests/fixtures/boot-doc-registry.tsv"

type bootDocFixtureRow struct {
	kind     string
	key      string
	readOnly bool
}

// loadBootDocRegistryFixture parses the shared table. Missing, unreadable or
// empty is FATAL, never a skip: a guard that goes green because it could not
// read its fixture is a lie, and this one would go green on an EMPTY file by
// agreeing that nothing exists.
func loadBootDocRegistryFixture(t *testing.T) []bootDocFixtureRow {
	t.Helper()
	f, err := os.Open(bootDocRegistryFixturePath)
	if err != nil {
		t.Fatalf("open %s: %v", bootDocRegistryFixturePath, err)
	}
	defer f.Close()

	var rows []bootDocFixtureRow
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text := sc.Text()
		if strings.HasPrefix(text, "#") || strings.TrimSpace(text) == "" {
			continue
		}
		cols := strings.Split(text, "\t")
		if len(cols) != 3 {
			t.Fatalf("%s:%d: want 3 tab-separated columns, got %d", bootDocRegistryFixturePath, line, len(cols))
		}
		if cols[0] == "kind" {
			continue // the header row
		}
		switch cols[2] {
		case "true", "false":
		default:
			t.Fatalf("%s:%d: read_only is %q, want true or false", bootDocRegistryFixturePath, line, cols[2])
		}
		rows = append(rows, bootDocFixtureRow{kind: cols[0], key: cols[1], readOnly: cols[2] == "true"})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", bootDocRegistryFixturePath, err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s parsed to zero rows", bootDocRegistryFixturePath)
	}
	return rows
}

func TestBootDocRegistry_MatchesTheSharedTableBothWays(t *testing.T) {
	s := newEventProcServer(t)

	want := map[string]bool{} // address -> read_only
	for _, row := range loadBootDocRegistryFixture(t) {
		addr := row.kind + "/" + row.key
		if _, dup := want[addr]; dup {
			t.Fatalf("%s is listed twice in %s", addr, bootDocRegistryFixturePath)
		}
		want[addr] = row.readOnly
	}

	got := map[string]bool{}
	for _, reg := range bootDocRegistry {
		for _, key := range reg.Keys {
			spec, ok := s.bootDocSpecFor(reg.Kind, key)
			if !ok {
				t.Fatalf("%s/%s is in bootDocRegistry but did not resolve", reg.Kind, key)
			}
			got[reg.Kind+"/"+key] = spec.ReadOnly
		}
	}

	for addr, readOnly := range got {
		fixture, listed := want[addr]
		if !listed {
			t.Errorf("this server serves %s, which %s does not list — add the row in the "+
				"same commit, or the cockpit will have no row for it and nothing will say so",
				addr, bootDocRegistryFixturePath)
			continue
		}
		if fixture != readOnly {
			t.Errorf("%s: the registry says read_only=%v, the shared table says %v",
				addr, readOnly, fixture)
		}
	}
	for addr := range want {
		if _, served := got[addr]; !served {
			t.Errorf("%s lists %s, which this server does not serve", bootDocRegistryFixturePath, addr)
		}
	}
}
