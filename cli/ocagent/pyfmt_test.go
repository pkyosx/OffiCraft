package main

import (
	"encoding/json"
	"testing"
)

func TestPyBool(t *testing.T) {
	if got := pyBool(true); got != "True" {
		t.Fatalf("pyBool(true) = %q, want %q", got, "True")
	}
	if got := pyBool(false); got != "False" {
		t.Fatalf("pyBool(false) = %q, want %q", got, "False")
	}
}

func TestPyStr(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{"JSON null renders as Python's None", `null`, "None"},
		{"a string renders as itself, unquoted", `"already gone"`, "already gone"},
		{"an empty string renders as nothing", `""`, ""},
		{"a whole number renders without a decimal point", `42`, "42"},
		{"a fractional number keeps its fraction", `2.5`, "2.5"},
		{"a bool renders Go-style, not Python-style", `true`, "true"},
		{"an object falls back to Go's %v", `{"a":1}`, "map[a:1]"},
		{"an array falls back to Go's %v", `["x","y"]`, "[x y]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v any
			if err := json.Unmarshal([]byte(tc.json), &v); err != nil {
				t.Fatal(err)
			}
			if got := pyStr(v); got != tc.want {
				t.Fatalf("pyStr(%s) = %q, want %q", tc.json, got, tc.want)
			}
		})
	}
}
