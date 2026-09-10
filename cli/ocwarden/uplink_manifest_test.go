package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// manifestUplinkPaths is the runtime half of the uplink join: what cli/uplinks.json
// hangs on one wire test, keyed by (producer run → route). bin/uplink-guard.py
// cannot answer this — everything it validates it validates in the same pass — so
// the committed side is read back here and compared against what the producers
// actually put on the wire.
//
// The key carries wire_case because three of this module's uplinks share one
// route: a per-route count alone is paid for by driving an already-covered
// producer a second time.
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
			WireCase string `json:"wire_case"`
			WireTest string `json:"wire_test"`
		} `json:"uplinks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse uplinks manifest: %v", err)
	}
	want := map[string]int{}
	for _, one := range doc.Uplinks {
		if one.Kind != "json" || one.WireTest != wireTest {
			continue
		}
		key := one.Path
		if one.WireCase != "" {
			key = one.WireCase + " → " + one.Path
		}
		want[key]++
	}
	// Zero committed rows makes every comparison below this vacuous rather than
	// passing, so it is a failure and not a floor.
	if len(want) == 0 {
		t.Fatalf("cli/uplinks.json commits no JSON uplink to %s, so the join it closes "+
			"has no committed side", wireTest)
	}
	return want
}
