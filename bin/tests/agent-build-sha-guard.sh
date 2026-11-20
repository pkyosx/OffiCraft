#!/usr/bin/env bash
# bin/tests/agent-build-sha-guard.sh — the ocagent build stamp is one contract
# written down in THREE places, and no Go test can see two of them (T-8f7d).
#
# WHY THIS EXISTS. A listener process holds the image it started with. Measured on
# this fleet: one listener sat on inode 65557770 while the file on disk turned over
# four times, so a FIXED bug was, for that agent, unfixed for its whole life — and
# "ship it and watch the fleet get better" returns a false negative. The
# connection line now names the ocagent that printed it, from a link-time stamp:
#
#   bin/build-bindist       go build -ldflags="… -X main.buildSHA=$OCAGENT_SHA"
#   cli/ocagent/main.go     var buildSHA string
#   cli/ocagent/listen_run.go   prints " [agent <sha>]" when it is non-empty
#
# 🔴 THE MISSING STAMP IS SILENT, AND IT IS THE DESIGNED-FOR CASE TOO. An empty
# buildSHA omits the segment — which is byte-identical to the honest "this build
# carries no stamp" that every `go build ./...` and every test binary produces. So
# dropping the -X flag, renaming the variable, or stamping a different package all
# degrade to exactly the behaviour the feature is supposed to have in a dev build.
# 🔴 AND EVERY GO TEST BINARY IS UNSTAMPED BY CONSTRUCTION, so no Go test can
# distinguish them: the suite stays green with the flag deleted. That is what this
# guard buys, and it is the whole reason it is a shell script.
#
# It also pins the ONE binary that must NOT be stamped, and this guard is the ONLY
# thing that does. cli/officraft is the one committed prebuilt
# (dist/officraft/officraft); its -trimpath -buildvcs=false keep the git revision
# out of the bits so the committed bytes are reproducible from a clean clone.
# 🔴 check-officraft-dist DOES NOT CATCH A STAMP — measured, rc=0 with all three
# stamped. It hashes cli/officraft's SOURCE files and dist/officraft/officraft's
# bytes; stamping changes no source file and it never looks at the bindist copy.
# What stamping actually does is split the SHIPPED anchor from the committed one
# (measured: bindist copy 710a2525… → d8fb53dd… while dist/ and binary.sha256 stay
# 710a2525…), and the bindist copy is what every install receives. Cases 4 and 9
# below compare the bytes of every anchor that ships.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"


# ruby is resolved the same way bin/tests/go-test-timeout-guard.sh and
# auto-beta-guard.sh do: the hosted macOS runner has system ruby, and case 10 needs
# a language that can read two binaries and search one for the whole of the other.
RUBY="$(command -v ruby 2>/dev/null || true)"
if [[ -z "$RUBY" ]]; then
  for cand in /usr/bin/ruby /opt/homebrew/bin/ruby /usr/local/bin/ruby; do
    [[ -x "$cand" ]] && { RUBY="$cand"; break; }
  done
fi

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

# ═════════════════════════════════════════════════════════════════════════════
# 🔴 BUILD IT AND LOOK AT THE BINARY. A text check over the build script or the
# ocagent source cannot see the ways this feature dies silently:
#   * a tree with no .git — the sha lookup came back empty and the build shipped
#     an unstamped fleet (fatal in build-bindist; case 8 proves it stays fatal);
#   * a SECOND, unstamped `go build` for ocagent appended after the stamped one —
#     the last write wins on disk, while a first-match text scan never sees it;
#   * a stamp fed from the wrong revision (HEAD~5, a stale variable) — shape-valid,
#     confidently naming a build this is not.
# So every case runs the real bin/build-bindist and reads the bytes it produced.
# ⚠️ SIDE EFFECT, ON PURPOSE: this rebuilds server/ocserverd/bindist/ (gitignored
# build output, which build-bindist rewrites from scratch anyway). ~1s. Anything
# cheaper would be back to grepping the script that is the thing under test.
echo
if ! command -v git >/dev/null 2>&1 || ! git -C "$ROOT" rev-parse --short HEAD >/dev/null 2>&1; then
  bad "cannot resolve HEAD: this guard has to compare the stamp against the tree being built, and without a sha it would silently check nothing"
else
  HEAD_SHA="$(git -C "$ROOT" rev-parse --short HEAD)"
  BUILD_LOG="$(mktemp -t oc-agent-sha-build.XXXXXX)"
  REBUILT_ANCHOR="$(mktemp -t oc-agent-sha-anchor.XXXXXX)"
  trap 'rm -f "$BUILD_LOG" "$REBUILT_ANCHOR"' EXIT
  if ! bash "$ROOT/bin/build-bindist" >"$BUILD_LOG" 2>&1; then
    bad "bin/build-bindist FAILED, so nothing below could be checked (last lines: $(tail -3 "$BUILD_LOG" | tr '\n' ' '))"
  else
    ok "bin/build-bindist runs clean, so the assertions below are about real output"

    # ── 7. the produced ocagent really carries THIS tree's sha ──────────────
    # 🔴 ASK THE BINARY, DO NOT GREP IT. This was `strings … | grep -c "$HEAD_SHA"`
    # and it was DECORATION in the tree shape CI uses. cli/ocagent is built without
    # -trimpath/-buildvcs=false, so Go auto-embeds `vcs.revision=<full sha>` — and
    # the short sha is a PREFIX of it, so in a plain clone the grep matches any
    # ocagent built from this tree, stamped or not. Review measured a second
    # unstamped build and a HEAD~5 stamp both passing while the shipped binary
    # reported `unstamped` and `a0d998a2`.
    # 🔴 IT LOOKED LOAD-BEARING TO ME BECAUSE I MEASURED IT IN A GIT WORKTREE,
    # where .git is a file, Go embeds no vcs settings, and the grep really does
    # return 0. One tree shape, generalised into a claim about all of them — and
    # the shape I did not test is the one actions/checkout produces.
    # `ocagent version` prints build.sha from the linked-in variable and nothing
    # else can supply it, so exact equality against it cannot be satisfied by
    # metadata that merely happens to contain the same characters.
    # ⚠️ THIS IS THE EMBED'S INPUT — the same gap case 10 closes one level up. The
    # ocagent a machine runs is the copy `go:embed all:bindist` (assets.go) baked
    # into ocserverd and serveBinary hands out (api_machines.go); this checks the
    # file build-bindist just wrote, before anything embeds it. The OUTPUT is
    # pinned in bin/release's verify_artifacts, which reads the packaged ocagent's
    # -ldflags with `go version -m` on the artifact about to be published. It lives
    # there rather than here because a real ocserverd only exists at release time —
    # not because of cost. See the comment beside it for the full reasoning.
    # Executed, so it has to be this host's build.
    AGENT_BIN="$ROOT/server/ocserverd/bindist/$(go env GOOS)-$(go env GOARCH)/ocagent"
    if [[ ! -f "$AGENT_BIN" ]]; then
      bad "bin/build-bindist produced no ocagent at $AGENT_BIN"
    # ⚠️ grep -c, NOT grep -q. Under `set -o pipefail`, grep -q exits the moment it
    # matches, strings takes SIGPIPE, and the PIPELINE reports 141 — so the
    # success case reads as failure. Measured here: -q said "not found" on a
    # binary where -c said 3.
    elif REPORTED="$("$AGENT_BIN" version 2>/dev/null | awk '/build\.sha:/{print $2}')" &&
         [[ "$REPORTED" == "$HEAD_SHA" ]]; then
      ok "the ocagent it produced REPORTS this tree's sha ($HEAD_SHA) from its own version command — asked, not grepped, so no auto-embedded vcs metadata can satisfy it"
    else
      bad "the ocagent bin/build-bindist just produced reports build.sha=\"${REPORTED:-<none>}\", not this tree's sha ($HEAD_SHA). Every listener from this build will print no [agent …] segment, or name a different build — and both are byte-indistinguishable from a dev build behaving correctly, which is the failure this whole feature exists to remove. Causes seen: an empty sha lookup (no .git), a second unstamped go build overwriting the first, or a stamp fed from the wrong revision"
    fi

    # ── 4. every non-darwin anchor is reproducible from source ──────────────
    # A stamp changes the anchor's bytes on every commit, and -trimpath keeps
    # -ldflags out of `go version -m`, so only the bytes can show it. Rebuilt with
    # dist/officraft/BUILD.md's flags; darwin's is the committed file (case 9).
    for anchor in "$ROOT"/server/ocserverd/bindist/*/officraft; do
      [[ -f "$anchor" ]] || { bad "bin/build-bindist produced no anchor under server/ocserverd/bindist/<os>-<arch>/"; continue; }
      plat="$(basename "$(dirname "$anchor")")"
      [[ "$plat" == darwin-arm64 ]] && continue
      if ! (cd "$ROOT/cli/officraft" && GOOS="${plat%-*}" GOARCH="${plat#*-}" CGO_ENABLED=0 \
            go build -trimpath -buildvcs=false -ldflags="-s -w" -o "$REBUILT_ANCHOR" ./...) >/dev/null 2>&1; then
        bad "could not rebuild the $plat anchor from cli/officraft to compare against"
      elif cmp -s "$anchor" "$REBUILT_ANCHOR"; then
        ok "the $plat anchor that ships is byte-identical to a fresh build of cli/officraft with BUILD.md's flags"
      else
        bad "the $plat anchor bin/build-bindist produced differs from a fresh build of cli/officraft with -trimpath -buildvcs=false -ldflags=\"-s -w\" — a stamp or extra flag makes its bytes change per commit, so no one can reproduce the anchor that ships"
      fi
    done

    # ── 9. the shipped anchors still equal the committed one ────────────────
    # The consequence case 4 is really about. check-officraft-dist cannot see this
    # (measured rc=0 with the anchor stamped): it compares dist/officraft/officraft
    # against its manifest, and never looks at the copy that actually ships.
    # 🔴 BOTH STAGED COPIES, and anchordist is the one that reaches machines.
    # An earlier version compared only bindist/officraft and its message claimed
    # "every install receives the bindist copy" — wrong: nothing serves that file.
    # build-bindist copies it on to cli/ocwarden/anchordist, which anchor_embed.go
    # go:embeds into ocwarden, and `ocwarden install` materialises THAT. Review
    # rebuilt anchordist separately and got a shipped anchor of aa576b9e… against a
    # committed 710a2525… with the guard still printing 10 ok.
    ANCHOR_COMMITTED="$ROOT/dist/officraft/officraft"
    if [[ ! -f "$ANCHOR_COMMITTED" ]]; then
      bad "cannot compare the shipped anchors: $ANCHOR_COMMITTED is missing"
    else
      B="$(shasum -a 256 "$ANCHOR_COMMITTED" | cut -d' ' -f1)"
      for staged in "$ROOT/server/ocserverd/bindist/darwin-arm64/officraft" "$ROOT/cli/ocwarden/anchordist/officraft"; do
        if [[ ! -f "$staged" ]]; then
          bad "cannot compare the shipped anchor against the committed one: $staged is missing"
          continue
        fi
        A="$(shasum -a 256 "$staged" | cut -d' ' -f1)"
        if [[ "$A" == "$B" ]]; then
          ok "$(basename "$(dirname "$staged")")/officraft is byte-identical to the committed dist/officraft/officraft (${A:0:12}…)"
        else
          bad "the anchor staged at $staged (${A:0:12}…) is not the committed one (${B:0:12}…). cli/ocwarden/anchordist is go:embedded into ocwarden and is what 'ocwarden install' materialises, so a divergence there means the TCC anchor machines actually run stopped being the one reviewers can reproduce — and check-officraft-dist returns 0 on exactly this, because it only ever compares the committed file to its own manifest"
        fi
      done

      # ── 10. the anchor is INSIDE the ocwarden that ships ──────────────────
      # 🔴 CASE 9 COMPARES THE EMBED'S INPUT, NOT ITS OUTPUT. It hashes files on
      # disk; what reaches a machine is the copy go:embed baked into ocwarden, and
      # the only thing connecting the two is the ORDER of two builds in
      # bin/build-bindist. Review reordered them — ocwarden built before the anchor
      # was staged — and measured a shipped ocwarden 1.77MB smaller (exactly the
      # anchor), embeddedAnchor() returning nil, `ocwarden install` refusing on
      # every new machine, and this guard printing 11 ok.
      # ⚠️ IT ALSO HID ITSELF: this script runs build-bindist up to three times, and
      # a later run staged the anchor the first run had missed. CI runs it once.
      # go:embed stores bytes verbatim, so the committed anchor's own bytes must be
      # findable inside the binary. That is the output, not the input.
      # 🔴 AND IT MUST BE THE WHOLE ANCHOR, NOT A SAMPLE OF IT. This read a 4096-byte
      # slice from the anchor's middle, which is a shape test, not an identity test:
      # measured, adding -X main.buildSHA to the cli/officraft line changes 100 bytes
      # out of 1,742,642 and NONE of them are in that window, so the probe returned
      # true for an anchor that was NOT the committed one. Case 9 catches that
      # particular mutant by hash, but the cell case 10 claims for itself — the
      # embed's OUTPUT — was green on a shipped ocwarden carrying the wrong anchor.
      # Containment of the ENTIRE file is the identity claim, and it costs 0.054s.
      # Every platform's ocwarden must carry ITS OWN platform's anchor: the
      # anchordist slot is reused per platform, so a warden compiled before its
      # anchor was staged embeds the previous platform's one instead. darwin's is
      # compared against the committed file itself.
      PLATFORM_DIRS=("$ROOT"/server/ocserverd/bindist/*/)
      if [[ ! -d "${PLATFORM_DIRS[0]}" ]]; then
        bad "bin/build-bindist produced no bindist/<os>-<arch>/ directory at all"
      fi
      for pdir in "${PLATFORM_DIRS[@]}"; do
        [[ -d "$pdir" ]] || continue
        plat="$(basename "$pdir")"
        WARDEN_BIN="${pdir}ocwarden"
        ANCHOR_WANT="${pdir}officraft"
        [[ "$plat" == darwin-arm64 ]] && ANCHOR_WANT="$ANCHOR_COMMITTED"
        if [[ -z "$RUBY" ]]; then
          bad "cannot check whether the shipped $plat ocwarden embeds the anchor: no ruby (the same dependency the ci.yml side already needs)"
        elif [[ ! -f "$WARDEN_BIN" ]]; then
          bad "bin/build-bindist produced no ocwarden at $WARDEN_BIN"
        elif [[ ! -s "$ANCHOR_WANT" ]]; then
          bad "bin/build-bindist produced no $plat anchor at $ANCHOR_WANT"
        elif "$RUBY" -e '
              anchor = File.binread(ARGV[0])
              warden = File.binread(ARGV[1])
              exit(anchor.bytesize > 0 && warden.include?(anchor) ? 0 : 1)
            ' "$ANCHOR_WANT" "$WARDEN_BIN"; then
          ok "the $plat ocwarden that ships contains its own platform's anchor IN FULL (all $(wc -c < "$ANCHOR_WANT" | tr -d ' ') bytes of ${ANCHOR_WANT#"$ROOT/"}, found inside the binary) — the embed's output, not the order the build stages its input in"
        else
          bad "the $plat ocwarden bin/build-bindist just produced does NOT contain ${ANCHOR_WANT#"$ROOT/"} in full. cli/ocwarden/anchordist is go:embedded into it and 'ocwarden install' refuses without an anchor, so the cockpit one-liner fails on every new $plat machine — and staging the file next to the build is not the same as the build having read it. Check the ORDER in bin/build-bindist: each platform's anchor must be staged BEFORE that platform's ocwarden is compiled"
        fi
      done

    # ⚠️ THE REMAINING CASES RUN LAST ON PURPOSE. They build twice more — once
    # refused, once to repair — and a LATER BUILD STAGES WHAT AN EARLIER ONE
    # MISSED. Cases 9 and 10 must read the FIRST build's output or this script
    # hides the defect it is looking for: measured, with the anchor staged after
    # ocwarden was compiled, the repair rebuild below fixed it and case 10 went
    # green on a shipped binary that had no anchor in it. CI runs build-bindist
    # once, so the first build is the one that matters.
    # ── 8. an empty sha must be FATAL, not a silent unstamped build ─────────
    if OCAGENT_SHA=" " bash "$ROOT/bin/build-bindist" >/dev/null 2>&1; then
      bad "bin/build-bindist accepted a BLANK OCAGENT_SHA and built anyway — that is the tarball/git-archive case, and it ships a fleet that cannot say which build it is while every text check here stays green"
    else
      ok "a blank OCAGENT_SHA is FATAL — a build that cannot name itself does not ship"
    fi
    # 🔴 AND IT MUST NOT HAVE DELETED ANYTHING ON ITS WAY OUT. bindist/.gitkeep is
    # TRACKED, and `rm -rf "$BINDIST"` used to run BEFORE the sha check, so a
    # refused build left the working tree with a deleted tracked file and only an
    # error message to explain it. That fix shipped with nothing guarding it:
    # review reverted it and this suite printed 11 ok, because THIS SCRIPT is both
    # what triggers the deletion (the failing build above) and what repairs it (the
    # rebuild below). Asserted here, before the repair, or it cannot be seen.
    if [[ -n "$(git -C "$ROOT" status --porcelain -- server/ocserverd/bindist/.gitkeep 2>/dev/null)" ]]; then
      bad "the refused build deleted the TRACKED file server/ocserverd/bindist/.gitkeep. A build that declines to run must not modify the tree on its way out — resolve the sha before anything destructive happens, not after"
    else
      ok "a refused build leaves the tree alone — the tracked bindist/.gitkeep survives it"
    fi
    bash "$ROOT/bin/build-bindist" >/dev/null 2>&1 || true
    fi
  fi
fi

echo "ocagent build-sha stamp contract tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
