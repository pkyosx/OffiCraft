package main

// doc_vars_t3201_test.go — the three duties of the {name} mechanism, each able
// to go red on its own: what a document DECLARES, what a write is REFUSED for,
// and what a render REFUSES to send.

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
		{"none", "沒有任何變數的一句話", nil},
		{"first appearance order, deduped", "{b} 然後 {a} 再一次 {b}", []string{"b", "a"}},
		{"adjacent slots are two slots", "{a}{b}", []string{"a", "b"}},
		{"an unclosed brace is not a slot", "{a 沒有收尾", nil},
		{"a lone closing brace is not a slot", "收尾} 而已", nil},
		{"empty name is a slot with an empty name", "{}", []string{""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DocVarsIn(c.text); !reflect.DeepEqual(got, c.want) {
				t.Errorf("DocVarsIn(%q) = %#v, want %#v", c.text, got, c.want)
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
		// nil is the opt-out the three pre-T-3201 documents rely on: their
		// seeds carry JSON examples this syntax cannot tell from a variable.
		{"nil declared validates nothing", `{"id": "<attachment id>"}`, nil, nil},
		// A non-nil EMPTY slice is the zero-variable rule the owner-editable
		// body half will declare once the read-only head is split off.
		{"empty declared allows none", "{task_no}", []string{}, []string{"task_no"}},
		{"declared name passes", "{task_no}", []string{"task_no"}, nil},
		{"one letter off is undeclared", "{task_nu}", []string{"task_no"}, []string{"task_nu"}},
		{"reports every offender", "{a} {b}", []string{"b"}, []string{"a"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DocVarsUndeclared(c.text, c.declared); !reflect.DeepEqual(got, c.want) {
				t.Errorf("DocVarsUndeclared(%q, %#v) = %#v, want %#v", c.text, c.declared, got, c.want)
			}
		})
	}
}

func TestRenderDocVars(t *testing.T) {
	declared := []string{"task_no", "status"}

	t.Run("substitutes every slot", func(t *testing.T) {
		got, err := RenderDocVars("任務 {task_no} 已結束（{status}）。", declared,
			map[string]string{"task_no": "T-3201", "status": "done"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "任務 T-3201 已結束（done）。"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("the same name substitutes at every occurrence", func(t *testing.T) {
		got, err := RenderDocVars("{task_no}/{task_no}", declared, map[string]string{"task_no": "T-1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "T-1/T-1"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	// 🔴 THE WHOLE POINT OF THE MECHANISM. A name nothing fills must stop the
	// send, not go out with its braces on and not quietly become "".
	t.Run("a declared name with no value refuses instead of sending", func(t *testing.T) {
		got, err := RenderDocVars("任務 {task_no} 已結束（{status}）。", declared,
			map[string]string{"task_no": "T-3201"})
		if err == nil {
			t.Fatalf("expected a refusal, got %q", got)
		}
		if got != "" {
			t.Errorf("a refused render must return no text, got %q", got)
		}
	})

	t.Run("an undeclared name refuses instead of sending", func(t *testing.T) {
		got, err := RenderDocVars("任務 {task_nu} 已結束。", declared, map[string]string{"task_nu": "T-3201"})
		if err == nil {
			t.Fatalf("expected a refusal, got %q", got)
		}
		if got != "" {
			t.Errorf("a refused render must return no text, got %q", got)
		}
	})

	// An empty value is a VALUE. The rule is about a name nobody supplied, not
	// about a name whose answer happens to be blank (an absent handover note).
	t.Run("an empty value is supplied, not missing", func(t *testing.T) {
		got, err := RenderDocVars("備註：{note}", []string{"note"}, map[string]string{"note": ""})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "備註："; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}
