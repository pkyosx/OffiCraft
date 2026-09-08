// Skeleton generated from server/ocserverd/doc_split.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestDocRendered(t *testing.T) {
	const marker = "<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->"
	for _, tc := range []struct {
		name string
		text string
		join string
		want string
	}{
		{
			name: "the marker disappears and the caller's separator joins the halves",
			text: "read-only head\n\n" + marker + "\n\neditable body",
			join: "\n",
			want: "read-only head\neditable body",
		},
		{
			name: "an empty join keeps the two halves adjacent",
			text: "head\n\n" + marker + "\n\nbody",
			join: "",
			want: "headbody",
		},
		{
			name: "a markerless document is returned unchanged",
			text: "head\nbody",
			join: "\n\n",
			want: "head\nbody",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DocRendered(tc.text, tc.join); got != tc.want {
				t.Fatalf("DocRendered(%q, %q) = %q, want %q", tc.text, tc.join, got, tc.want)
			}
		})
	}
}

func TestDocBodyVarRefusal(t *testing.T) {
	const marker = "<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->"
	got := docBodyVarRefusal("〈停止〉", []string{"note", "task_no"})
	want := "the 〈停止〉 uses {note}, {task_no} below the line `" + marker +
		"` — the editable half carries no variables at all, because nothing fills them there and they would reach an agent with the braces still in them. " +
		"Put facts that vary in the read-only head, or write them out. Nothing was written."
	if got != want {
		t.Fatalf("docBodyVarRefusal returned %q, want %q", got, want)
	}
}
