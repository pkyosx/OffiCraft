package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// manifestUplinkPaths is the runtime half of the uplink join: per route, how many
// JSON uplinks cli/uplinks.json hangs on one wire test. bin/uplink-guard.py cannot
// answer this — everything it validates it validates in the same pass — so the
// committed side is read back here and compared against what a producer actually
// put on the wire.
func manifestUplinkPaths(t *testing.T, wireTest string) map[string]int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "uplinks.json"))
	if err != nil {
		t.Fatalf("read uplinks manifest: %v", err)
	}
	var doc struct {
		Uplinks []struct {
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			Path     string `json:"path"`
			WireTest string `json:"wire_test"`
		} `json:"uplinks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse uplinks manifest: %v", err)
	}
	want := map[string]int{}
	for _, one := range doc.Uplinks {
		if one.Kind == "json" && one.WireTest == wireTest {
			want[one.Path]++
		}
	}
	// Zero committed rows makes every comparison below this vacuous rather than
	// passing, so it is a failure and not a floor.
	if len(want) == 0 {
		t.Fatalf("cli/uplinks.json commits no JSON uplink to %s, so the join it closes "+
			"has no committed side", wireTest)
	}
	return want
}
