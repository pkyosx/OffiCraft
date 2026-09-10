package main

import "testing"

func TestCanonicalSemver(t *testing.T) {
	for _, tc := range []struct {
		name  string
		label string
		want  string
		ok    bool
	}{
		{name: "keeps the v prefix", label: "v1.2.3", want: "v1.2.3", ok: true},
		{name: "adds the v prefix", label: "1.2.3", want: "v1.2.3", ok: true},
		{name: "keeps prerelease and build metadata", label: "1.2.3-rc.1+build.7", want: "v1.2.3-rc.1+build.7", ok: true},
		{name: "rejects an empty label", label: "", ok: false},
		{name: "accepts a short semver label", label: "1.2", want: "v1.2", ok: true},
		{name: "rejects a nonversion label", label: "release", ok: false},
		{name: "rejects a second v prefix", label: "vv1.2.3", ok: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, ok := canonicalSemver(tc.label)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("canonicalSemver(%q) = %q, %v; want %q, %v", tc.label, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestReleaseIsNewer(t *testing.T) {
	for _, tc := range []struct {
		name      string
		candidate string
		running   string
		want      bool
	}{
		{name: "higher patch is newer", candidate: "v1.2.4", running: "1.2.3", want: true},
		{name: "equal versions are not newer", candidate: "1.2.3", running: "v1.2.3", want: false},
		{name: "lower version is not newer", candidate: "1.2.2", running: "1.2.3", want: false},
		{name: "release outranks its prerelease", candidate: "1.2.3", running: "1.2.3-rc.1", want: true},
		{name: "invalid candidate is refused", candidate: "latest", running: "1.2.3", want: false},
		{name: "invalid running version is refused", candidate: "1.2.4", running: "dev", want: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := releaseIsNewer(tc.candidate, tc.running); got != tc.want {
				t.Fatalf("releaseIsNewer(%q, %q) = %v, want %v", tc.candidate, tc.running, got, tc.want)
			}
		})
	}
}

func TestSemverOutranks(t *testing.T) {
	for _, tc := range []struct {
		name      string
		candidate string
		incumbent string
		want      bool
	}{
		{name: "higher release displaces incumbent", candidate: "v1.2.4", incumbent: "1.2.3", want: true},
		{name: "equal release does not displace incumbent", candidate: "1.2.3", incumbent: "v1.2.3", want: false},
		{name: "lower release does not displace incumbent", candidate: "1.2.2", incumbent: "1.2.3", want: false},
		{name: "invalid candidate never wins", candidate: "latest", incumbent: "1.2.3", want: false},
		{name: "valid candidate displaces invalid incumbent", candidate: "1.2.3", incumbent: "latest", want: true},
		{name: "both invalid labels do not produce a winner", candidate: "latest", incumbent: "nightly", want: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := semverOutranks(tc.candidate, tc.incumbent); got != tc.want {
				t.Fatalf("semverOutranks(%q, %q) = %v, want %v", tc.candidate, tc.incumbent, got, tc.want)
			}
		})
	}
}
