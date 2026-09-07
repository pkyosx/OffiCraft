package main

import "testing"

func TestNamespaceFromEnv(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{"unset is the main instance", "", "", ""},
		{"lowercase word", "lab", "lab", ""},
		{"digits and dashes", "a-b-9", "a-b-9", ""},
		{"sixteen characters is the limit", "abcdefghijklmnop", "abcdefghijklmnop", ""},
		{"seventeen characters", "abcdefghijklmnopq", "", `OC_NAMESPACE must match [a-z0-9-]{1,16}, got: "abcdefghijklmnopq"`},
		{"uppercase", "Lab", "", `OC_NAMESPACE must match [a-z0-9-]{1,16}, got: "Lab"`},
		{"underscore", "la_b", "", `OC_NAMESPACE must match [a-z0-9-]{1,16}, got: "la_b"`},
		{"dot", "la.b", "", `OC_NAMESPACE must match [a-z0-9-]{1,16}, got: "la.b"`},
		{"slash", "a/b", "", `OC_NAMESPACE must match [a-z0-9-]{1,16}, got: "a/b"`},
		{"whitespace only", " ", "", `OC_NAMESPACE must match [a-z0-9-]{1,16}, got: " "`},
	}
	for _, c := range cases {
		env := func(k string) string {
			if k == "OC_NAMESPACE" {
				return c.raw
			}
			return ""
		}
		got, err := namespaceFromEnv(env)
		if got != c.want {
			t.Errorf("%s: namespace = %q, want %q", c.name, got, c.want)
		}
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: err = %v, want nil", c.name, err)
		case c.wantErr != "" && (err == nil || err.Error() != c.wantErr):
			t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
		}
	}
}

func TestWardenLabelFor(t *testing.T) {
	cases := []struct {
		ns   string
		want string
	}{
		{"", "com.officraft.ocwarden"},
		{"lab", "com.officraft.ocwarden.lab"},
		{"a-b-9", "com.officraft.ocwarden.a-b-9"},
	}
	for _, c := range cases {
		if got := wardenLabelFor(c.ns); got != c.want {
			t.Errorf("wardenLabelFor(%q) = %q, want %q", c.ns, got, c.want)
		}
	}
}

func TestOfficraftRootFor(t *testing.T) {
	cases := []struct {
		home string
		ns   string
		want string
	}{
		{"/Users/eva", "", "/Users/eva/.officraft"},
		{"/Users/eva", "lab", "/Users/eva/.officraft-lab"},
		{"/Users/eva/", "lab", "/Users/eva/.officraft-lab"},
		{"", "lab", ".officraft-lab"},
	}
	for _, c := range cases {
		if got := officraftRootFor(c.home, c.ns); got != c.want {
			t.Errorf("officraftRootFor(%q, %q) = %q, want %q", c.home, c.ns, got, c.want)
		}
	}
}

func TestTmuxSocketFor(t *testing.T) {
	cases := []struct {
		ns   string
		want string
	}{
		{"", "officraft"},
		{"lab", "officraft-lab"},
	}
	for _, c := range cases {
		if got := tmuxSocketFor(c.ns); got != c.want {
			t.Errorf("tmuxSocketFor(%q) = %q, want %q", c.ns, got, c.want)
		}
	}
}

func TestTokfileFor(t *testing.T) {
	cases := []struct {
		home string
		ns   string
		want string
	}{
		{"/Users/eva", "", "/Users/eva/.officraft/warden/exec-warden.tok"},
		{"/Users/eva", "lab", "/Users/eva/.officraft-lab/warden/exec-warden.tok"},
	}
	for _, c := range cases {
		if got := tokfileFor(c.home, c.ns); got != c.want {
			t.Errorf("tokfileFor(%q, %q) = %q, want %q", c.home, c.ns, got, c.want)
		}
	}
}
