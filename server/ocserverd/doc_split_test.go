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

func TestDocSplitHeadBody(t *testing.T) {
	const marker = "<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->"

	t.Run("a marked document returns the exact halves and reports that it was split", func(t *testing.T) {
		head, body, split := DocSplitHeadBody("head\n\n" + marker + "\n\nbody")
		if head != "head" || body != "body" || !split {
			t.Fatalf("DocSplitHeadBody(marked) = (%q, %q, %v), want (head, body, true)", head, body, split)
		}
	})

	t.Run("a markerless document stays whole and reports no split", func(t *testing.T) {
		const text = "head\nbody"
		head, body, split := DocSplitHeadBody(text)
		if head != text || body != "" || split {
			t.Fatalf("DocSplitHeadBody(markerless) = (%q, %q, %v), want (%q, empty, false)", head, body, split, text)
		}
	})
}

func TestDocJoinHeadBody(t *testing.T) {
	const marker = "<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->"

	joined := DocJoinHeadBody("generated head", "owner body")
	want := "generated head\n\n" + marker + "\n\nowner body"
	if joined != want {
		t.Fatalf("DocJoinHeadBody = %q, want %q", joined, want)
	}
	head, body, split := DocSplitHeadBody(joined)
	if head != "generated head" || body != "owner body" || !split {
		t.Fatalf("joined document split = (%q, %q, %v), want original halves", head, body, split)
	}
}
