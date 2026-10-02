package main

import (
	"maps"
	"reflect"
	"sort"
	"testing"
)

// realUpgradeReports drives the reporter production wires onto the upgrade
// relay once per state the relay reports, and returns each body it posted.
func realUpgradeReports(t *testing.T) map[string]map[string]any {
	t.Helper()
	reports := map[string]upgradeReport{
		"running":               {UpgradeID: "ru-1", State: "running", FromVersion: "2.1.200"},
		"succeeded":             {UpgradeID: "ru-1", State: "succeeded", FromVersion: "2.1.200", ToVersion: "2.1.290"},
		"failed":                {UpgradeID: "ru-1", State: "failed", FromVersion: "2.1.200", ToVersion: "2.1.200", Reason: "版本沒有變"},
		"failed-before-reading": {UpgradeID: "ru-2", State: "failed", Reason: "這台機器沒有安裝 Claude Code"},
	}
	bodies := map[string]map[string]any{}
	for name, rep := range reports {
		posted := wireBodies(t, func(base string) {
			newUpgradeReporter(Config{Base: base, Token: "tok", ID: "m-1"})(rep)
		})
		if len(posted) != 1 {
			t.Fatalf("%s put %d bodies on the wire, want exactly 1", name, len(posted))
		}
		bodies[name] = posted[0]
	}
	return bodies
}

// TestWardenRuntimeUpgradeUplinkBodies confronts every body the upgrade
// reporter posts with the schema the frozen spec declares for the route, and
// with a written-out expectation of the body itself.
func TestWardenRuntimeUpgradeUplinkBodies(t *testing.T) {
	declared := frozenRequestSchema(t, "post", runtimeUpgradePath)
	cases := realUpgradeReports(t)

	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)
	walked := map[string]int{}
	for _, name := range names {
		payload := cases[name]
		if extra := undeclaredPayloadKeys(payload, declared); len(extra) > 0 {
			t.Errorf("%s carries key(s) the frozen spec does not declare: %v; payload = %#v", name, extra, payload)
		}
		if missing := missingRequiredKeys(payload, declared); len(missing) > 0 {
			t.Errorf("%s omits key(s) the frozen spec requires: %v; payload = %#v", name, missing, payload)
		}
		if bad := mistypedPayloadValues(payload, declared); len(bad) > 0 {
			t.Errorf("%s sends declared key(s) with the wrong wire type: %v; payload = %#v", name, bad, payload)
		}
	}
	walked["runtime-upgrade → "+runtimeUpgradePath] = 1
	if committed := manifestUplinkPaths(t, "cli/ocwarden/runtimeupgrade_wire_test.go"); !maps.Equal(walked, committed) {
		t.Errorf("cli/uplinks.json commits %v to this wire test but %v was walked", committed, walked)
	}

	want := map[string]map[string]any{
		"running":   {"upgrade_id": "ru-1", "state": "running", "from_version": "2.1.200"},
		"succeeded": {"upgrade_id": "ru-1", "state": "succeeded", "from_version": "2.1.200", "to_version": "2.1.290"},
		"failed": {"upgrade_id": "ru-1", "state": "failed", "from_version": "2.1.200", "to_version": "2.1.200",
			"reason": "版本沒有變"},
		"failed-before-reading": {"upgrade_id": "ru-2", "state": "failed", "reason": "這台機器沒有安裝 Claude Code"},
	}
	if !reflect.DeepEqual(cases, want) {
		t.Errorf("upgrade report bodies =\n  %#v\nwant\n  %#v", cases, want)
	}
}
