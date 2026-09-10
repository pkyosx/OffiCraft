// Skeleton generated from server/ocserverd/doc_vars.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"reflect"
	"testing"
)

func TestDocVarsIn(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "names are returned in first appearance order and repeated names are removed",
			text: "{owner} assigned {task} to {owner} for {task}",
			want: []string{"owner", "task"},
		},
		{
			name: "text without a complete slot returns no names",
			text: "a dangling {name and no closing slot",
			want: nil,
		},
		{
			name: "an empty slot is still a matched name",
			text: "before {} after",
			want: []string{""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DocVarsIn(tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("DocVarsIn(%q) = %#v, want %#v", tc.text, got, tc.want)
			}
		})
	}
}

func TestDocVarsUndeclared(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		declared []string
		want     []string
	}{
		{
			name:     "nil declarations opt out of validation",
			text:     "{owner} {unknown}",
			declared: nil,
			want:     nil,
		},
		{
			name:     "an empty declaration rejects every used name",
			text:     "{owner} {task} {owner}",
			declared: []string{},
			want:     []string{"owner", "task"},
		},
		{
			name:     "only names absent from the declaration are returned",
			text:     "{owner} {task} {owner} {missing}",
			declared: []string{"owner", "task"},
			want:     []string{"missing"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DocVarsUndeclared(tc.text, tc.declared); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("DocVarsUndeclared(%q, %#v) = %#v, want %#v", tc.text, tc.declared, got, tc.want)
			}
		})
	}
}

func TestRenderDocVars(t *testing.T) {
	t.Run("declared slots are replaced without reprocessing braces in values", func(t *testing.T) {
		got, err := RenderDocVars(
			"owner={owner}; task={task}; again={owner}",
			[]string{"owner", "task"},
			map[string]string{"owner": "Eva {owner}", "task": "T-125"},
		)
		if err != nil {
			t.Fatalf("RenderDocVars returned an error: %v", err)
		}
		want := "owner=Eva {owner}; task=T-125; again=Eva {owner}"
		if got != want {
			t.Fatalf("RenderDocVars returned %q, want %q", got, want)
		}
	})

	t.Run("a document with no slots is returned unchanged", func(t *testing.T) {
		got, err := RenderDocVars("plain document", nil, nil)
		if err != nil {
			t.Fatalf("RenderDocVars returned an error: %v", err)
		}
		if got != "plain document" {
			t.Fatalf("RenderDocVars returned %q, want %q", got, "plain document")
		}
	})

	t.Run("an undeclared slot refuses the whole render", func(t *testing.T) {
		got, err := RenderDocVars("{owner} {task}", []string{"owner"}, map[string]string{
			"owner": "Eva",
			"task":  "T-125",
		})
		if got != "" {
			t.Fatalf("an undeclared slot returned %q, want an empty output", got)
		}
		if err == nil || err.Error() != "document cannot be rendered: it uses {task} which this document does not declare" {
			t.Fatalf("undeclared slot error = %v", err)
		}
	})

	t.Run("all missing values are named in document order and nothing is rendered", func(t *testing.T) {
		got, err := RenderDocVars("{owner} {task} {owner}", []string{"owner", "task"}, nil)
		if got != "" {
			t.Fatalf("missing values returned %q, want an empty output", got)
		}
		if err == nil || err.Error() != "document cannot be rendered: no value was supplied for {owner}, {task} — nothing was sent" {
			t.Fatalf("missing value error = %v", err)
		}
	})
}

func TestDocVarNameList(t *testing.T) {
	for _, tc := range []struct {
		name  string
		names []string
		want  string
	}{
		{name: "no names", names: nil, want: ""},
		{name: "two names", names: []string{"owner", "task"}, want: "{owner}, {task}"},
		{name: "an empty name keeps its braces", names: []string{""}, want: "{}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := docVarNameList(tc.names); got != tc.want {
				t.Fatalf("docVarNameList(%#v) = %q, want %q", tc.names, got, tc.want)
			}
		})
	}
}
