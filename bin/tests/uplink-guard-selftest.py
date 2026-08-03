#!/usr/bin/env python3
"""Known-bad-example positive control for bin/uplink-guard.py.

The guard enumerates every callsite under `cli/**` that can put a body on the
wire and requires each one to be claimed by a row in `cli/uplinks.json`. The
failure mode of a scanner like that is not "it breaks" — it is that **it quietly
stops seeing things and stays green**. Three review rounds each found one more
way past it, and every single one of them was rc=0: a new module, a second call
through an already-listed helper, `http.Post`, a hand-built `&http.Request{}`, a
row pointed at a harmless line, a retired anchor left behind as a comment, a
backtick string that swallowed the scanner's view of an entire function.

So this file is the other half of the guard: **one fixture per known bypass, and
the scanner must name each one.** It is the same shape `cli/ocwarden`'s
`scanProcessStarters` uses (`cli/CLAUDE.md` documents why), and the reason that
one has survived two rounds of the same class of bug.

Two properties make it a control rather than decoration:

  * the clean copy is asserted GREEN first — otherwise every case below could be
    "caught" for some unrelated reason and the whole file would prove nothing;
  * each case names what it expects in the failure output, so a guard that goes
    red for the wrong reason does not count as catching it.

Run: python3 bin/tests/uplink-guard-selftest.py
"""
import json, os, shutil, subprocess, sys, tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
GUARD = ROOT / "bin/uplink-guard.py"

# Each case: (name, what it plants, which file the guard must name).
# `plant` gets the staged repo root and returns nothing; it may write Go files
# under cli/ and/or edit cli/uplinks.json.

def add_go(root, rel, body):
    path = root / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body)

def edit_manifest(root, mutate):
    path = root / "cli/uplinks.json"
    doc = json.loads(path.read_text())
    mutate(doc["uplinks"])
    path.write_text(json.dumps(doc, indent=2, ensure_ascii=False) + "\n")

CASES = []

def case(name, expect):
    def register(fn):
        CASES.append((name, fn, expect))
        return fn
    return register

# ── the send API the enumeration used to be blind to ──────────────────────────
# Every one of these was rc=0 against the regex version: its patterns were
# lower-case and keyed on http.NewRequest, so stdlib's capitalised helpers and a
# hand-built request were invisible.

@case("stdlib http.Post", "cli/ocagent/selftest_post.go:6")
def _(root):
    add_go(root, "cli/ocagent/selftest_post.go", '''package main

import ("bytes"; "net/http")

func selftestPost(base string, raw []byte) {
	_, _ = http.Post(base+"/api/monitoring/telemetry", "application/json", bytes.NewReader(raw))
}
''')

@case("(*http.Client).Post and http.PostForm", "cli/ocagent/selftest_clientpost.go:7")
def _(root):
    add_go(root, "cli/ocagent/selftest_clientpost.go", '''package main

import ("bytes"; "net/http"; "net/url")

func selftestClientPost(base string, raw []byte) {
	c := &http.Client{}
	_, _ = c.Post(base+"/api/monitoring/telemetry", "application/json", bytes.NewReader(raw))
	_, _ = http.PostForm(base+"/api/agent/context", url.Values{"a": {"b"}})
}
''')

@case("hand-built &http.Request{} handed to Do", "cli/ocagent/selftest_handbuilt.go:7")
def _(root):
    add_go(root, "cli/ocagent/selftest_handbuilt.go", '''package main

import ("bytes"; "io"; "net/http"; "net/url")

func selftestHandBuilt(base string, raw []byte) {
	u, _ := url.Parse(base + "/api/monitoring/telemetry")
	req := &http.Request{Method: http.MethodPost, URL: u, Body: io.NopCloser(bytes.NewReader(raw))}
	_, _ = http.DefaultClient.Do(req)
}
''')

@case("send inside a one-line func body", "cli/ocagent/selftest_oneline.go:5")
def _(root):
    # The old rule skipped any line starting with `func`, to avoid counting the
    # shared seams' own declarations. A whole function body on that line went
    # with it.
    add_go(root, "cli/ocagent/selftest_oneline.go", '''package main

import ("bytes"; "net/http")

func selftestOneLine(base string, raw []byte) { r, _ := http.NewRequest(http.MethodPost, base+"/api/agent/context", bytes.NewReader(raw)); _, _ = http.DefaultClient.Do(r) }
''')

@case("a new module that sends, declared but with no wire test", "own no *_wire_test.go")
def _(root):
    add_go(root, "cli/ocnew/selftest_module.go", '''package main

import ("bytes"; "net/http")

func selftestNewModule(base, route string, raw []byte) {
	req, _ := http.NewRequest(http.MethodPost, base+route, bytes.NewReader(raw))
	_, _ = (&http.Client{}).Do(req)
}
''')
    # Declared properly, so this cannot go red for being unclaimed — the only
    # thing left to catch it is "a module that sends must own a wire test".
    edit_manifest(root, lambda rows: rows.extend([
        {"id": "selftest-newmod-build", "source": "cli/ocnew/selftest_module.go",
         "callsite": "req, _ := http.NewRequest(http.MethodPost, base+route, bytes.NewReader(raw))",
         "kind": "seam", "seam_reason": "builder"},
        {"id": "selftest-newmod-send", "source": "cli/ocnew/selftest_module.go",
         "callsite": "_, _ = (&http.Client{}).Do(req)",
         "kind": "seam", "seam_reason": "sender"},
    ]))

# ── ways to make a row look honest while the real send goes unclaimed ─────────

@case("a row pointed at a line that sends nothing", "does not cover any send")
def _(root):
    # The real send IS claimed correctly here, so this cannot go red for being
    # unclaimed. The only thing left to catch the second row is the rule that an
    # anchor has to cover an actual send — which is what makes a row evidence
    # rather than a sentence.
    add_go(root, "cli/ocwarden/selftest_laundered.go", '''package main

const selftestLaunderedPath = telemetryPath

func selftestLaundered(s *codexSession, payload map[string]any) {
	s.post(selftestLaunderedPath, payload)
}
''')
    edit_manifest(root, lambda rows: rows.extend([
        {"id": "selftest-laundered-real", "source": "cli/ocwarden/selftest_laundered.go",
         "callsite": "s.post(selftestLaunderedPath, payload)",
         "kind": "skip", "skip_reason": "claimed correctly, so it is not what this case is about"},
        {"id": "selftest-laundered", "source": "cli/ocwarden/selftest_laundered.go",
         "callsite": 'const selftestLaunderedPath = telemetryPath',
         "kind": "skip", "skip_reason": "internal bookkeeping, nothing goes on the wire"},
    ]))

@case("one row absorbing two sends that share a line", "covers 2 sends at once")
def _(root):
    # gofmt does not split this line, so a claim keyed on the LINE covers both
    # sends with one row — the second uplink then never gets compared by anyone.
    add_go(root, "cli/ocwarden/selftest_twoonaline.go", '''package main

func selftestTwoOnALine(s *codexSession, firstPath, secondPath string, payload map[string]any) {
	s.post(firstPath, payload); s.post(secondPath, payload)
}
''')
    edit_manifest(root, lambda rows: rows.append({
        "id": "selftest-twoonaline", "source": "cli/ocwarden/selftest_twoonaline.go",
        "callsite": "s.post(firstPath, payload); s.post(secondPath, payload)",
        "kind": "skip", "skip_reason": "bookkeeping only",
    }))

@case("two uplinks sharing one piece of wire-test evidence", "same wire-test evidence")
def _(root):
    # A distinct wire_case, so the slot rule (same wire test + producer run + route)
    # does not fire first — the borrowed EVIDENCE is what this case is about, and a
    # fixture that reddens for a different reason proves nothing about its own rule.
    edit_manifest(root, lambda rows: rows.append(dict(
        next(r for r in rows if r["id"] == "warden-heartbeat"),
        id="selftest-borrowed", source="cli/ocwarden/main.go",
        wire_case="selftest-borrowed-run",
        callsite="status, body := post(telemetryPath, payload)")))

@case("an anchor that matches two places names neither", "needs exactly 1")
def _(root):
    edit_manifest(root, lambda rows: next(
        r for r in rows if r["id"] == "codex-identity").__setitem__(
            "callsite", 's.post("/api/monitoring/telemetry", map[string]any{'))

@case("a real uplink declared as out-of-scope", "names an API path in its own source line")
def _(root):
    # The only opt-out this gate has. Nothing can machine-check whether a written
    # reason is TRUE, so the check is on what the code says: a line that spells an
    # API path is sending to that API, whatever the row claims.
    add_go(root, "cli/ocwarden/selftest_optout.go", '''package main

func selftestOptOut(s *codexSession, payload map[string]any) {
	s.post("/api/selftest-opted-out", payload)
}
''')
    edit_manifest(root, lambda rows: rows.append({
        "id": "selftest-optout", "source": "cli/ocwarden/selftest_optout.go",
        "callsite": 's.post("/api/selftest-opted-out", payload)',
        "kind": "skip",
        "skip_reason": "Derived locally from numbers another report already carries, so there is nothing new on the wire.",
    }))

@case("retired anchor left behind as a comment", "selftest-commented")
def _(root):
    add_go(root, "cli/ocwarden/selftest_comment.go", '''package main

func selftestCommented(s *codexSession, payload map[string]any) {
	// s.post("/api/selftest-retired", payload) — retired, kept for history
	s.post("/api/selftest-brandnew", payload)
}
''')
    edit_manifest(root, lambda rows: rows.append({
        "id": "selftest-commented", "source": "cli/ocwarden/selftest_comment.go",
        "callsite": 's.post("/api/selftest-retired", payload)',
        "kind": "skip", "skip_reason": "retired",
    }))

# ── ways to make the scanner stop seeing real code ────────────────────────────
# Both were rc=0 against the regex version, and both are SILENT: the scanner
# reports nothing missing because it never saw the send at all.

@case("backtick raw string containing a comment opener",
      "cli/ocwarden/selftest_rawstring.go:6")
def _(root):
    add_go(root, "cli/ocwarden/selftest_rawstring.go", '''package main

var selftestOpen = `/*`

func selftestRawString(s *codexSession, payload map[string]any) {
	s.post("/api/selftest-hidden-by-rawstring", payload)
}

var selftestClose = `*/`
''')

@case("interpreted string containing a line-comment opener",
      "cli/ocwarden/selftest_slashstring.go:4")
def _(root):
    # The send is on the SAME line as the string, because the bypass being pinned is
    # "the `//` inside a string blanks the rest of ITS OWN line". With the two on
    # separate lines this fixture was vacuous: a naive regex lexer (verified by
    # swapping one in) leaves the send visible anyway, so the case passed without
    # exercising the rule it names.
    add_go(root, "cli/ocwarden/selftest_slashstring.go", '''package main

func selftestSlashString(s *codexSession, payload map[string]any) {
	tag := "a//b"; s.post("/api/selftest-hidden-by-string", payload)
}
''')

# ── a comment must never be able to satisfy the scanner ───────────────────────

# ── the per-row checks: the half that actually compares against the wire ──────
# These reddened nothing when the entire block was deleted, which made the most
# valuable check in the file — "the route still points at the DTO this uplink was
# written for" — deletable in silence.

@case("a route repointed at a different DTO", "requestBody $ref is")
def _(root):
    spec = root / "spec/openapi.json"
    doc = json.loads(spec.read_text())
    ref = doc["paths"]["/api/monitoring/telemetry"]["post"]["requestBody"]["content"]["application/json"]["schema"]
    ref["$ref"] = "#/components/schemas/AgentContextIngestDTO"
    spec.write_text(json.dumps(doc, indent=2, ensure_ascii=False))

@case("an uplink whose route is not in the spec at all", "is not in OpenAPI")
def _(root):
    spec = root / "spec/openapi.json"
    doc = json.loads(spec.read_text())
    del doc["paths"]["/api/reply-cards"]["post"]
    spec.write_text(json.dumps(doc, indent=2, ensure_ascii=False))

@case("a row claiming to be JSON without naming its schema", "needs its OpenAPI request_schema")
def _(root):
    edit_manifest(root, lambda rows: next(
        r for r in rows if r["id"] == "warden-heartbeat").pop("request_schema"))

@case("wire-test evidence that no longer exists", "wire-test evidence")
def _(root):
    edit_manifest(root, lambda rows: next(
        r for r in rows if r["id"] == "warden-heartbeat").__setitem__(
            "wire_needle", "func ThisAssertionWasDeletedLongAgo"))

# ── rules that had NO positive control until independent review deleted them ──
# Each of these was verified by mutation: with the rule removed from the guard, the
# selftest stayed fully green. A rule nothing reddens for is a rule the next edit
# can drop for free, which is how the scan narrows in silence.

@case("a stale allowlist entry whose route now exists", "allowlist entry is stale")
def _(root):
    edit_manifest(root, lambda rows: next(
        r for r in rows if r["id"] == "ocagent-presence").__setitem__(
            "path", "/api/monitoring/telemetry"))

@case("a row with a kind the guard does not know", "kind must be json, read, seam, or skip")
def _(root):
    edit_manifest(root, lambda rows: next(
        r for r in rows if r["id"] == "warden-heartbeat").__setitem__("kind", "jsonish"))

@case("an allowlisted row carrying evidence it cannot have", "can only be filled in with something untrue")
def _(root):
    # The row this reproduces SHIPPED: a route that does not exist was made to name
    # a DTO and a wire test anyway, because the guard demanded both. The evidence
    # was necessarily false — the named test drove a different command entirely.
    edit_manifest(root, lambda rows: next(
        r for r in rows if r["id"] == "ocagent-presence").__setitem__(
            "request_schema", "#/components/schemas/AgentContextIngestDTO"))

@case("a JSON row sending to a route other than the one it claims", "the schema being compared is another route's")
def _(root):
    # The $ref is moved WITH the path, so the $ref comparison is satisfied and this
    # rule is the only thing left to catch it. Repointing the path alone reddens on
    # the $ref instead, which would make this fixture pass while proving nothing
    # about the rule it names.
    def repoint(rows):
        row = next(r for r in rows if r["id"] == "codex-reply-card")
        row["path"] = "/api/monitoring/telemetry"
        row["request_schema"] = "#/components/schemas/AgentTelemetryIngestDTO"
        # The evidence slot must stay unique or the shared-witness rule fires first.
        row["wire_needle"] = 'drive("reply-card", func() {'
    edit_manifest(root, repoint)

@case("a wire test that no longer performs the runtime join", "never calls manifestUplinkPaths")
def _(root):
    # The static half of this gate cannot tell whether a committed body was ever put
    # on the wire — everything it validates, it validates in one pass. The runtime
    # join in the wire tests is what answers that, so deleting the call has to be as
    # red as deleting a row.
    path = root / "cli/ocwarden/codex_uplink_wire_test.go"
    path.write_text(path.read_text().replace("manifestUplinkPaths(", "countOfNothing("))

@case("a sending module whose only wire test file is empty", "own no *_wire_test.go with a test function")
def _(root):
    # A filename is not a test. `printf 'package relay\\n' > x_wire_test.go` is 14
    # bytes and satisfied the earlier filename-only rule, which let a whole sending
    # module through — the original incident's own shape inside its own fix.
    add_go(root, "cli/ocrelay/selftest_relay.go", '''package main

import ("bytes"; "net/http")

func selftestRelay(base, route string, raw []byte) {
	req, _ := http.NewRequest(http.MethodPost, base+route, bytes.NewReader(raw))
	_, _ = (&http.Client{}).Do(req)
}
''')
    add_go(root, "cli/ocrelay/selftest_relay_wire_test.go", "package main\n")
    edit_manifest(root, lambda rows: rows.extend([
        {"id": "selftest-relay-build", "source": "cli/ocrelay/selftest_relay.go",
         "callsite": "req, _ := http.NewRequest(http.MethodPost, base+route, bytes.NewReader(raw))",
         "kind": "seam", "seam_reason": "builder"},
        {"id": "selftest-relay-send", "source": "cli/ocrelay/selftest_relay.go",
         "callsite": "_, _ = (&http.Client{}).Do(req)",
         "kind": "seam", "seam_reason": "sender"},
    ]))

@case("an uplink smuggled through a function value taken from a seam", "cli/ocwarden/selftest_alias.go:4")
def _(root):
    # The cheapest bypass found in any round: it reuses the repo's own seam, needs
    # no new import and no manifest edit at all, and compiles to a real send. Every
    # pattern keyed on `name(` sees nothing, because the call is spelled `emit(`.
    add_go(root, "cli/ocwarden/selftest_alias.go", '''package main

func selftestAlias(s *codexSession, payload map[string]any) {
	emit := s.post
	emit("/api/agent/context", payload)
}
''')

# The first version of the alias rule keyed on the right-hand side of `=`/`:=`. Go
# does not need an assignment to take a function value, and review shipped three live
# uplinks (three routes, real bodies on a real test server) past that version with
# ZERO manifest edits. One fixture per form, separately, so a rule that only covers
# some of them cannot pass by reddening on another.

@case("a seam handed to a function as an argument", "cli/ocwarden/selftest_argform.go:7")
def _(root):
    add_go(root, "cli/ocwarden/selftest_argform.go", '''package main

type selftestPoster = func(string, map[string]any)

func selftestApply(f selftestPoster, p map[string]any) { f("/api/agent/context", p) }

func selftestArgForm(s *codexSession, p map[string]any) { selftestApply(s.post, p) }
''')

@case("a seam handed back as a return value", "cli/ocwarden/selftest_returnform.go:5")
def _(root):
    add_go(root, "cli/ocwarden/selftest_returnform.go", '''package main

type selftestReturnPoster = func(string, map[string]any)

func selftestReturnForm(s *codexSession) selftestReturnPoster { return s.post }
''')

@case("a seam placed in a composite literal", "cli/ocwarden/selftest_sliceform.go:6")
def _(root):
    add_go(root, "cli/ocwarden/selftest_sliceform.go", '''package main

type selftestSlicePoster = func(string, map[string]any)

func selftestSliceForm(s *codexSession, p map[string]any) {
	for _, f := range []selftestSlicePoster{s.post} {
		f("/api/monitoring/telemetry", p)
	}
}
''')

@case("the runtime join replaced by a string that merely spells it", "never calls manifestUplinkPaths")
def _(root):
    # The join check reads the wire test's source. Looked for in the RAW text, a
    # string literal satisfies it — two lines retire the join while the guard keeps
    # reporting it is wired in. Same defect this file already fixed for anchors:
    # a scanner its own documentation can satisfy.
    path = root / "cli/ocwarden/codex_uplink_wire_test.go"
    text = path.read_text()
    start = text.index("\twant := manifestUplinkPaths(")
    end = text.index("\n\n", text.index("t.Fatalf", start))
    path.write_text(text[:start] + '\tvar _ = "manifestUplinkPaths(t, ...)" // the join, retired\n' + text[end:])

@case("a live uplink hidden behind allow_missing_spec", "the schema being compared is another route's")
def _(root):
    # allow_missing_spec used to be an unconditional exemption evaluated BEFORE the
    # callsite/path cross-check, so a real, spec'd, tested uplink could take it by
    # dropping its own path, $ref and wire test. Now the cross-check runs first, so
    # a callsite that spells its route forces that route to be declared — and a
    # declared route that exists in the spec reddens the stale-allowlist rule.
    # The declared path is one the spec does NOT have, so the stale-allowlist rule
    # cannot fire and the callsite/path cross-check is the only thing left. Declaring
    # a route that exists would redden on staleness instead and prove nothing about
    # the ordering this case exists for.
    def hide(rows):
        row = next(r for r in rows if r["id"] == "codex-reply-card")
        row["allow_missing_spec"] = "retired server-side"
        row["path"] = "/api/reply-cards-v2"
        for gone in ("request_schema", "wire_test", "wire_needle"):
            row.pop(gone, None)
    edit_manifest(root, hide)

@case("an uplink routed through a send helper whose NAME is not on any list", "cli/ocwarden/selftest_ctor.go:6")
def _(root):
    # The cheapest bypass of round six, in both modules: `httpPoster` contains no
    # `post` substring and `\bPost` does not match inside it, so a real send built by
    # the module's OWN constructor was invisible with the manifest untouched. The fix
    # is not another name — it is deriving sink names from the tree (any function
    # whose body reaches net/http becomes one).
    add_go(root, "cli/ocwarden/selftest_ctor.go", '''package main

import "net/http"

func selftestCtorLeak(cfg Config) {
	sender := httpPoster(&http.Client{Timeout: httpTimeout}, cfg.Base, cfg.Token)
	sender("/api/agent/context", map[string]any{"leaked": true})
}
''')

@case("the same, through the other module's request helper", "cli/ocagent/selftest_ctor.go:4")
def _(root):
    add_go(root, "cli/ocagent/selftest_ctor.go", '''package main

func selftestCtorContext(client httpClient, cfg Config) {
	httpRequest(client, "POST", cfg.Base+"/api/agent/context", cfg.Token, map[string]any{"leaked": true})
}
''')

@case("two uplinks sharing one slot in the runtime join", "same slot in the runtime join")
def _(root):
    # The join counts sends per (wire test, producer run, route). Two rows in one slot
    # are interchangeable to it, so one can be paid for by driving the other twice —
    # review did exactly that with a body that would 422 in production.
    add_go(root, "cli/ocwarden/selftest_sameslot.go", '''package main

func selftestSameSlot(s *codexSession) {
	s.post(telemetryPath, map[string]any{"smuggled_field": 1})
}
''')
    edit_manifest(root, lambda rows: rows.append(dict(
        next(r for r in rows if r["id"] == "codex-identity"),
        id="selftest-sameslot", source="cli/ocwarden/selftest_sameslot.go",
        callsite='s.post(telemetryPath, map[string]any{"smuggled_field": 1})',
        wire_needle="sort.Strings(bad)")))

@case("an allowlisted row that names no route at all", "must name the route it posts to")
def _(root):
    # Without this, allow_missing_spec is unconditional for every callsite whose path
    # arrives as a parameter — which is every row that goes through a seam.
    edit_manifest(root, lambda rows: next(
        r for r in rows if r["id"] == "ocagent-presence").pop("path"))

@case("comment mentioning a send is not a send", None)
def _(root):
    # Inverted: this one must stay GREEN. A guard that its own documentation can
    # satisfy has shipped in this repo before, and the fix for that (strip
    # comments) is exactly what the two cases above can break if someone
    # "simplifies" the lexer back into a regex.
    add_go(root, "cli/ocagent/selftest_docs.go", '''package main

// selftestDocs explains that we never post(anything) here, and points at
// https://example.com/api for the reason. Neither line is a callsite.
func selftestDocs() int { return 1 }
''')

def run_guard(root):
    result = subprocess.run([sys.executable, str(root / "bin/uplink-guard.py")],
                            capture_output=True, text=True,
                            env={**os.environ, "OC_UPLINKS_MANIFEST": str(root / "cli/uplinks.json")})
    return result.returncode, (result.stdout + result.stderr)

def stage(tmp):
    """A copy of just what the guard reads, so nothing here can touch the repo."""
    root = Path(tmp) / "repo"
    (root / "bin").mkdir(parents=True)
    (root / "spec").mkdir(parents=True)
    shutil.copy(GUARD, root / "bin/uplink-guard.py")
    shutil.copy(ROOT / "spec/openapi.json", root / "spec/openapi.json")
    shutil.copytree(ROOT / "cli", root / "cli")
    return root

# Deleting a case is the one edit this file cannot otherwise notice: CASES is
# appended to, main() only prints len(CASES), and a run with one fewer fixture reads
# exactly like a run with all of them. That is the same silent-narrowing failure the
# guard's own docstring accuses SINK_PATTERNS of.
#
# A floor (`len(CASES) >= N`) was tried and is too weak: it only catches NET deletion,
# so removing a fixture with teeth while adding a harmless one keeps the count and
# loses the coverage. A set of NAMES was tried next and is also too weak: `expect=None`
# means "this one must stay GREEN", so flipping a positive control's expectation to
# None retires the rule AND its control while the names and the count are untouched
# (review demonstrated it — the banner still read "each still caught"). So the
# EXPECTATION is committed too. Both directions are edits a reviewer sees.
# ── the bypass that was LIVE IN THIS REPO while the enumeration stopped one hop
# from net/http (four uplinks, cli/ocwarden/command.go) ───────────────────────
# Both fixtures below are the shape those four travelled: nothing about them
# mentions net/http, so a scan that only asks "does this function touch http"
# reports the file as sending nothing at all. The first is the cheap general form
# (one relay); the second is the exact one that was live — a constructor's result
# stored in a struct field of function type, called through the field.

@case("a send two relays deep — the shape four live uplinks hid behind",
      "cli/ocagent/selftest_relay.go:14")
def _(root):
    add_go(root, "cli/ocagent/selftest_relay.go", '''package main

import ("bytes"; "net/http")

func selftestRelaySend(base string, raw []byte) {
	_, _ = http.Post(base+"/api/monitoring/telemetry", "application/json", bytes.NewReader(raw))
}

func selftestRelayHop(base string, raw []byte) {
	selftestRelaySend(base, raw)
}

func selftestRelayCaller(base string, raw []byte) {
	selftestRelayHop(base, raw)
}
''')

@case("a reporter reached through a struct field of function type",
      "cli/ocagent/selftest_field.go:20")
def _(root):
    add_go(root, "cli/ocagent/selftest_field.go", '''package main

import ("bytes"; "net/http")

type selftestFieldDeps struct {
	Report func([]byte)
}

func newSelftestReporter(base string) func([]byte) {
	return func(raw []byte) {
		_, _ = http.Post(base+"/api/monitoring/telemetry", "application/json", bytes.NewReader(raw))
	}
}

func selftestFieldWire(base string) selftestFieldDeps {
	return selftestFieldDeps{Report: newSelftestReporter(base)}
}

func selftestFieldSend(deps selftestFieldDeps, raw []byte) {
	deps.Report(raw)
}
''')

# ── what independent review landed past the version that shipped before these ─────
# All three were rc=0 with a live JSON body on a real test server.

@case("a JSON body posted over a raw socket, with no net/http anywhere",
      "cli/ocagent/selftest_socket.go:6")
def _(root):
    # The HTTP/1.1 request line is just text. Every pattern keyed on net/http sees
    # nothing here, and review used exactly this to POST a real body with ZERO rows.
    add_go(root, "cli/ocagent/selftest_socket.go", '''package main

import ("fmt"; "io"; "net"; "strings")

func selftestSocket(addr, body string) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return
	}
	defer conn.Close()
	head := strings.Join([]string{
		"POST /api/monitoring/telemetry HTTP/1.1",
		"Host: " + addr,
		"Content-Type: application/json",
		fmt.Sprintf("Content-Length: %d", len(body)),
		"Connection: close", "", ""}, "\\r\\n")
	_, _ = io.WriteString(conn, head+body)
}
''')

@case("two uplinks behind a constructor named like an entry point",
      "cli/ocagent/selftest_entryname.go:17")
def _(root):
    # `run` is on the paperwork stop list, so no row is raised for CALLING it. That
    # must not stop the value it returns from becoming a sink: review shipped two
    # route-specific uplinks through `emit := run(base)` while the guard stayed green
    # and both routes collapsed into one generic seam row.
    add_go(root, "cli/ocagent/selftest_entryname.go", '''package main

import ("bytes"; "net/http")

func selftestBeacon(base string) func(string, []byte) {
	return func(path string, raw []byte) {
		_, _ = http.Post(base+path, "application/json", bytes.NewReader(raw))
	}
}

func run(base string) func(string, []byte) {
	return selftestBeacon(base)
}

func selftestStage(base string) {
	emit := run(base)
	emit("/api/monitoring/telemetry", []byte(`{"a":1}`))
	emit("/api/agent/context", []byte(`{"b":2}`))
}
''')
    edit_manifest(root, lambda rows: rows.extend([
        {"id": "selftest-entry-post", "source": "cli/ocagent/selftest_entryname.go",
         "callsite": '_, _ = http.Post(base+path, "application/json", bytes.NewReader(raw))',
         "kind": "seam", "seam_reason": "declared, so this case is not about it"},
        {"id": "selftest-entry-ctor", "source": "cli/ocagent/selftest_entryname.go",
         "callsite": "return selftestBeacon(base)",
         "kind": "skip", "skip_reason": "declared, so this case is not about it"},
    ]))

@case("a POST dressed as a read, behind a harmless call on the same line",
      "does not prove it reads")
def _(root):
    # Two bypasses in one line. kind=read is the only place a row may spell an API
    # path without being a JSON uplink, so it is where a POST would hide; and an
    # earlier version resolved "which function is this callsite calling?" by taking
    # the FIRST call on the line, which `selftestNoop();` alone was enough to defeat.
    add_go(root, "cli/ocagent/selftest_readmask.go", '''package main

const selftestVerbPost = "POST"

func selftestNoop() {}

func selftestSend(client httpClient, cfg Config, path string, payload any) (int, string) {
	return httpRequest(client, selftestVerbPost, cfg.Base+path, cfg.Token, payload, 0)
}

func selftestPushCard(client httpClient, cfg Config, payload any) (int, string) {
	selftestNoop()
	return selftestSend(client, cfg, "/api/reply-cards", payload)
}
''')
    edit_manifest(root, lambda rows: rows.extend([
        {"id": "selftest-readmask-seam", "source": "cli/ocagent/selftest_readmask.go",
         "callsite": "return httpRequest(client, selftestVerbPost, cfg.Base+path, cfg.Token, payload, 0)",
         "kind": "seam", "seam_reason": "declared, so this case is not about it"},
        {"id": "selftest-readmask", "source": "cli/ocagent/selftest_readmask.go",
         "callsite": 'return selftestSend(client, cfg, "/api/reply-cards", payload)',
         "kind": "read", "method": "get", "path": "/api/reply-cards",
         "read_reason": "claims to be a poll; it is a POST"},
    ]))

EXPECTED_CASES = frozenset([
    ('a JSON body posted over a raw socket, with no net/http anywhere', 'cli/ocagent/selftest_socket.go:6'),
    ('a POST dressed as a read, behind a harmless call on the same line', 'does not prove it reads'),
    ('two uplinks behind a constructor named like an entry point', 'cli/ocagent/selftest_entryname.go:17'),
    ('a reporter reached through a struct field of function type', 'cli/ocagent/selftest_field.go:20'),
    ('a send two relays deep — the shape four live uplinks hid behind', 'cli/ocagent/selftest_relay.go:14'),
    ('(*http.Client).Post and http.PostForm', 'cli/ocagent/selftest_clientpost.go:7'),
    ('a JSON row sending to a route other than the one it claims', "the schema being compared is another route's"),
    ('a live uplink hidden behind allow_missing_spec', "the schema being compared is another route's"),
    ('a new module that sends, declared but with no wire test', 'own no *_wire_test.go'),
    ('a real uplink declared as out-of-scope', 'names an API path in its own source line'),
    ('a route repointed at a different DTO', 'requestBody $ref is'),
    ('a row claiming to be JSON without naming its schema', 'needs its OpenAPI request_schema'),
    ('a row pointed at a line that sends nothing', 'does not cover any send'),
    ('a row with a kind the guard does not know', 'kind must be json, read, seam, or skip'),
    ('a seam handed back as a return value', 'cli/ocwarden/selftest_returnform.go:5'),
    ('a seam handed to a function as an argument', 'cli/ocwarden/selftest_argform.go:7'),
    ('a seam placed in a composite literal', 'cli/ocwarden/selftest_sliceform.go:6'),
    ('a sending module whose only wire test file is empty', 'own no *_wire_test.go with a test function'),
    ('a stale allowlist entry whose route now exists', 'allowlist entry is stale'),
    ('a wire test that no longer performs the runtime join', 'never calls manifestUplinkPaths'),
    ('an allowlisted row carrying evidence it cannot have', 'can only be filled in with something untrue'),
    ('an allowlisted row that names no route at all', 'must name the route it posts to'),
    ('an anchor that matches two places names neither', 'needs exactly 1'),
    ('an uplink routed through a send helper whose NAME is not on any list', 'cli/ocwarden/selftest_ctor.go:6'),
    ('an uplink smuggled through a function value taken from a seam', 'cli/ocwarden/selftest_alias.go:4'),
    ('an uplink whose route is not in the spec at all', 'is not in OpenAPI'),
    ('backtick raw string containing a comment opener', 'cli/ocwarden/selftest_rawstring.go:6'),
    ('comment mentioning a send is not a send', None),
    ('hand-built &http.Request{} handed to Do', 'cli/ocagent/selftest_handbuilt.go:7'),
    ('interpreted string containing a line-comment opener', 'cli/ocwarden/selftest_slashstring.go:4'),
    ('one row absorbing two sends that share a line', 'covers 2 sends at once'),
    ('retired anchor left behind as a comment', 'selftest-commented'),
    ('send inside a one-line func body', 'cli/ocagent/selftest_oneline.go:5'),
    ('stdlib http.Post', 'cli/ocagent/selftest_post.go:6'),
    ('the runtime join replaced by a string that merely spells it', 'never calls manifestUplinkPaths'),
    ("the same, through the other module's request helper", 'cli/ocagent/selftest_ctor.go:4'),
    ('two uplinks sharing one piece of wire-test evidence', 'same wire-test evidence'),
    ('two uplinks sharing one slot in the runtime join', 'same slot in the runtime join'),
    ('wire-test evidence that no longer exists', 'wire-test evidence'),
])

def main():
    failures = []
    registered = {(name, expect) for name, _, expect in CASES}
    if registered != EXPECTED_CASES:
        gone = sorted(EXPECTED_CASES - registered)
        added = sorted(registered - EXPECTED_CASES)
        print(f"[uplink-selftest] FAIL — the case set drifted from EXPECTED_CASES.\n"
              f"  removed: {gone}\n  added:   {added}\n"
              f"Each case is a bypass that was live at some point. Adding one? Add its "
              f"name above. Removing one? Say why in the same commit — a fixture that "
              f"quietly disappears takes its coverage with it and nothing else here "
              f"goes red.", file=sys.stderr)
        raise SystemExit(1)
    with tempfile.TemporaryDirectory() as tmp:
        clean = stage(tmp)
        rc, output = run_guard(clean)
        if rc != 0:
            print("[uplink-selftest] FAIL — the unmodified copy is already red, so every "
                  "case below would 'pass' for the wrong reason:\n" + output, file=sys.stderr)
            raise SystemExit(1)

    for name, plant, expect in CASES:
        with tempfile.TemporaryDirectory() as tmp:
            root = stage(tmp)
            plant(root)
            rc, output = run_guard(root)
            if expect is None:
                if rc != 0:
                    failures.append(f"{name}: expected GREEN, got rc={rc}\n{output}")
                continue
            if rc == 0:
                failures.append(f"{name}: the guard did NOT notice it (rc=0). This bypass "
                                f"was live once; if it is live again the gate is smaller "
                                f"than it reads.\n{output}")
            elif expect not in output:
                failures.append(f"{name}: the guard went red but never named {expect}, so it "
                                f"caught something else.\n{output}")

    if failures:
        print("[uplink-selftest] FAIL —\n\n" + "\n\n".join(failures), file=sys.stderr)
        raise SystemExit(1)
    print(f"[uplink-selftest] all green ({len(CASES)} known bypasses, each still caught)")

if __name__ == "__main__":
    main()
