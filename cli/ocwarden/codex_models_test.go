package main

import "testing"

func TestNewestCodexFamilyModel(t *testing.T) {
	list := []codexModelEntry{
		{ID: "gpt-5.6-sol"},
		{ID: "gpt-6-sol"},
		{ID: "gpt-6.1-sol"},
		{ID: "gpt-7-sol", Hidden: true},
		{ID: "gpt-6.2-sol-mini"},
		{ID: "gpt-6-luna"},
		{ID: "gpt-5.6-luna"},
		{ID: "gpt-5.6-terra"},
		{ID: "gpt-10-astra"},
		{ID: "gpt-9.9-astra"},
		{ID: "gpt-5.5"},
		{ID: "codex-auto-review", Hidden: true},
	}
	cases := []struct {
		family string
		want   string
		found  bool
	}{
		{"sol", "gpt-6.1-sol", true},
		{"luna", "gpt-6-luna", true},
		{"terra", "gpt-5.6-terra", true},
		{"astra", "gpt-10-astra", true},
		{"gpt-5.5", "", false},
	}
	for _, c := range cases {
		got, found := newestCodexFamilyModel(list, c.family)
		if got != c.want || found != c.found {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.family, got, found, c.want, c.found)
		}
	}

	if got, found := newestCodexFamilyModel([]codexModelEntry{{ID: "gpt-6-terra", Hidden: true}}, "terra"); found {
		t.Errorf("a family whose only model is hidden resolved to %q", got)
	}
	if got, _ := newestCodexFamilyModel([]codexModelEntry{{ID: "gpt-6-sol"}, {ID: "gpt-6.0-sol"}}, "sol"); got != "gpt-6-sol" {
		t.Errorf("two spellings of one version: got %q, want the first listed, gpt-6-sol", got)
	}
}
