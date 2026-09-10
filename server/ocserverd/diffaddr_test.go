// Skeleton generated from server/ocserverd/diffaddr.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"reflect"
	"testing"
)

func TestParseDiffSide(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want diffSide
		err  string
	}{
		{
			name: "a stored attachment id is accepted as an attachment side",
			raw:  "att-0123456789ab",
			want: diffSide{Raw: "att-0123456789ab", AttachmentID: "att-0123456789ab"},
		},
		{
			name: "a current document address is parsed into four fields",
			raw:  "doc:system/global/current/body",
			want: diffSide{Raw: "doc:system/global/current/body", Doc: &diffDocAddress{
				Kind: "system", Key: "global", At: "current", Field: "body",
			}},
		},
		{
			name: "a seed address is accepted",
			raw:  "doc:boot/sequence/seed/head",
			want: diffSide{Raw: "doc:boot/sequence/seed/head", Doc: &diffDocAddress{
				Kind: "boot", Key: "sequence", At: "seed", Field: "head",
			}},
		},
		{
			name: "a decimal revision id is accepted",
			raw:  "doc:role/assistant/42/definition-md",
			want: diffSide{Raw: "doc:role/assistant/42/definition-md", Doc: &diffDocAddress{
				Kind: "role", Key: "assistant", At: "42", Field: "definition-md",
			}},
		},
		{
			name: "an empty side explains the two accepted address shapes",
			raw:  "",
			err:  "a comparison side must name a stored attachment id (att-…) or a document (doc:<kind>/<key>/<at>/<field>)",
		},
		{
			name: "whitespace only is also an empty side",
			raw:  "   ",
			err:  "a comparison side must name a stored attachment id (att-…) or a document (doc:<kind>/<key>/<at>/<field>)",
		},
		{
			name: "an attachment prefix without its id is refused",
			raw:  "att-",
			err:  "'att-' is neither a stored attachment id (att- plus 12 hex digits) nor a document address (doc:<kind>/<key>/<at>/<field>)",
		},
		{
			name: "uppercase attachment hex is outside the id alphabet",
			raw:  "att-0123456789AB",
			err:  "'att-0123456789AB' is neither a stored attachment id (att- plus 12 hex digits) nor a document address (doc:<kind>/<key>/<at>/<field>)",
		},
		{
			name: "a document with the wrong number of segments is refused",
			raw:  "doc:system/global/current",
			err:  "'doc:system/global/current' is not a document address — it is doc:<kind>/<key>/<at>/<field>, where <at> is current, seed or a revision id",
		},
		{
			name: "an empty kind is named",
			raw:  "doc:/global/current/body",
			err:  "'doc:/global/current/body' leaves its kind empty",
		},
		{
			name: "an empty key is named",
			raw:  "doc:system//current/body",
			err:  "'doc:system//current/body' leaves its key empty",
		},
		{
			name: "an empty field is named",
			raw:  "doc:system/global/current/",
			err:  "'doc:system/global/current/' leaves its field empty",
		},
		{
			name: "a traversal key is refused by name",
			raw:  "doc:system/../current/body",
			err:  "'doc:system/../current/body' has a key that is not a usable address segment: '..'",
		},
		{
			name: "a percent escape is refused from the field segment",
			raw:  "doc:system/global/current/body%2F",
			err:  "'doc:system/global/current/body%2F' has a field that is not a usable address segment: 'body%2F'",
		},
		{
			name: "an unknown at value is refused",
			raw:  "doc:system/global/today/body",
			err:  "'doc:system/global/today/body' has an <at> of 'today' — it must be current, seed, or a revision id from list_document_history",
		},
		{
			name: "zero is not a revision id",
			raw:  "doc:system/global/0/body",
			err:  "'doc:system/global/0/body' has an <at> of '0' — it must be current, seed, or a revision id from list_document_history",
		},
		{
			name: "a padded address is not trimmed before validation",
			raw:  " doc:system/global/current/body ",
			err:  "' doc:system/global/current/body ' is neither a stored attachment id (att- plus 12 hex digits) nor a document address (doc:<kind>/<key>/<at>/<field>)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, errText := parseDiffSide(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseDiffSide(%q) side = %#v, want %#v", tc.raw, got, tc.want)
			}
			if errText != tc.err {
				t.Fatalf("parseDiffSide(%q) error = %q, want %q", tc.raw, errText, tc.err)
			}
		})
	}
}

func TestDiffAddrSegmentRefusal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		raw   string
		what  string
		value string
		want  string
	}{
		{
			name:  "an empty segment is reported separately",
			raw:   "doc:system//current/body",
			what:  "key",
			value: "",
			want:  "'doc:system//current/body' leaves its key empty",
		},
		{
			name:  "a dot segment is refused as traversal",
			raw:   "doc:system/./current/body",
			what:  "key",
			value: ".",
			want:  "'doc:system/./current/body' has a key that is not a usable address segment: '.'",
		},
		{
			name:  "a double dot segment is refused as traversal",
			raw:   "doc:system/../current/body",
			what:  "key",
			value: "..",
			want:  "'doc:system/../current/body' has a key that is not a usable address segment: '..'",
		},
		{
			name:  "a slash is not a usable segment character",
			raw:   "doc:system/key/current/body/x",
			what:  "field",
			value: "body/x",
			want:  "'doc:system/key/current/body/x' has a field that is not a usable address segment: 'body/x'",
		},
		{
			name:  "an allowed segment has no refusal",
			raw:   "doc:system/key/current/body",
			what:  "field",
			value: "body",
			want:  "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := diffAddrSegmentRefusal(tc.raw, tc.what, tc.value); got != tc.want {
				t.Fatalf("diffAddrSegmentRefusal(%q, %q, %q) = %q, want %q", tc.raw, tc.what, tc.value, got, tc.want)
			}
		})
	}
}
