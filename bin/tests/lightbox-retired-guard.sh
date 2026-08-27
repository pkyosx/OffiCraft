#!/usr/bin/env bash
# Keeps the retired <Lightbox> image overlay from coming back (T-f014).
#
# ── the failure mode this guard exists to stop ────────────────────────────────
# The cockpit had TWO full-size overlays for the same job. `MarkdownPreviewOverlay`
# (`.md-preview*`) grew a header with the filename, a share link, a download and
# a close button; `Lightbox` (`.chat__lightbox*`) was a bare backdrop with an ×.
# Which one a user got depended on which call site their click travelled through.
# Worse, the divergence rotted silently: `AttachmentStrip` stopped reading its
# `onOpenImage` prop, so FIVE call sites went on passing a handler into a
# component that ignored it while mounting a second overlay that could never
# open. Nothing was red — an unused prop and an unreachable component are both
# perfectly type-correct.
#
# T-f014 deleted the component and its stylesheet block. This guard is what stops
# the next person from re-introducing a second image surface: reach for a
# `<Lightbox>` again and CI says no, in the same breath as pointing at the shell
# that already exists.
#
# ── WHAT THIS GUARD ASSERTS ──────────────────────────────────────────────────
#   1. `<Lightbox` appears ZERO times in production frontend source.
#   2. No stylesheet under frontend/ declares a `.chat__lightbox*` RULE — the
#      block whose 40 lines were deleted from office.css. Scoped to rule
#      declarations (a line whose first non-space is the selector) rather than
#      any mention, because the surrounding prose has to stay free to explain
#      what was retired and why; the assertion is about shipped CSS.
#   3. The corpus it searched is NON-EMPTY and really is the frontend tree. A
#      grep over a mistyped path returns zero matches and would otherwise be
#      reported as a pass — the classic "green because nothing was checked".
#   4. The corpus is SOURCE — what git holds a human responsible for — and not
#      build output. Both halves are asserted: an ignored file with the violation
#      in it is NOT reported, and an untracked, un-ignored one IS.
#   5. POSITIVE CONTROL: a planted `<Lightbox .../>` in a scratch copy of the
#      tree is found, AT THE PLANTED PATH AND LINE. Asserting only "the scan
#      failed" is not enough — a scan that failed for an unrelated reason (bad
#      regex, missing tool, wrong directory) would score as a working guard.
#      This repo has shipped that bug twice, so the control matches the exact
#      `path:line` it planted, not merely a non-zero exit.
#   6. NEGATIVE CONTROL, both kinds: a clean tree reports nothing, AND an
#      identical violation sitting in build output is NOT reported.
#   7. REACH: an identical violation in an untracked, un-ignored file IS
#      reported. Without this one, (4) is satisfiable by a corpus that has
#      quietly shrunk to "whatever is staged right now".
#
# ── HOW IT SEARCHES: 🔴 VERSION CONTROL IS THE FILTER ────────────────────────
# T-1a7d, 2026-08-27. This guard used to walk the tree with `find`, pruning
# `node_modules`, `dist` and `.git` BY DIRECTORY NAME. That is a deny-list, and a
# deny-list of names is exactly the wrong shape for "is this a file a human
# wrote": it has to be extended every time somebody adds an output directory,
# and nobody remembers to. MEASURED on a working copy that had been built
# (2026-08-27, frontend/): the old walk read 497 files where `git ls-files` reads
# 480. The 17 it should never have opened were
#   • `dist-paint-guard/assets/index-*.css`  — 🔴 `-name dist` does NOT match
#     `dist-paint-guard`, so the one directory the deny-list was written for did
#     not even cover its own siblings; and
#   • `playwright/.cache/assets/*.css` ×16   — never in the prune list at all.
# Those are Vite's compiled bundles. They contain whatever the SOURCE contained,
# so a rule retired from `office.css` months ago can still be sitting in a stale
# bundle on somebody's laptop, and this guard would report the retired class as
# "back" — a red with no commit behind it, in a check that has nothing to do with
# building the frontend. It is a LOCAL-ONLY false red by construction: CI gives
# every job a fresh checkout and the `bin-guards` job never runs Vite, so the
# cloud has never seen it and never will. That is precisely what makes it
# corrosive — it only ever wastes the time of whoever is standing at the machine.
#
# The corpus is therefore `git ls-files --cached --others --exclude-standard`: an
# allow-list of the files git holds a human responsible for — committed OR merely
# written and not ignored — which needs no maintenance when the next output
# directory appears. ⚠️ The `--others` half is not decoration: a first draft of
# this used `--cached` alone, and MEASURED, that silently stopped seeing a new
# file that had been written but not yet `git add`-ed, which is the state a
# working copy spends most of its editing life in. Ignoring is what excludes
# build output, and .gitignore already covers it.
# It is still a QUERY over the tree and deliberately NOT an enumerated list of
# files — a list is a promise that it is complete, it silently stops covering
# files added after it was written, and the next reader trusts it and skips
# looking. The only thing named by path here is the ONE surviving overlay, and
# that is named so a violation can point at it.
#
# If this ever runs somewhere that is not a git checkout, `git ls-files` returns
# nothing and assertion (3) FAILS LOUDLY rather than passing on an empty corpus.
# That is the fail-closed direction, and it is asserted, not hoped for.
#
# ── WHAT A GREEN DOES NOT MEAN ───────────────────────────────────────────────
# It does not mean there is only one image preview surface. Someone can write a
# second overlay under a different name and different classes tomorrow; this
# guard only knows the two names the retired one used. Nor does it check that
# the surviving overlay is any good — that is what the vitest suite and the CT
# visual guards are for. It is a tripwire on a specific regression, not a proof
# of the design.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
FE="$ROOT/frontend"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

# The one overlay that survived — named so a failure can say where to go instead.
SURVIVOR="frontend/src/components/MarkdownPreviewOverlay.tsx"

# ── THE CORPUS: A POSITIVE SHAPE, NOT A LIST OF THINGS TO SKIP ───────────────
# 🔴 THIRD TIME ON THE SAME BUG, so it is spelled out. Twice now this guard has
# defined its corpus by naming things to EXCLUDE, and twice a generated file
# walked straight past the names:
#   • `find … -name dist -prune`  did not cover `dist-paint-guard/`.
#   • `--exclude-standard` alone   did not cover `frontend/recon-out/`, because
#     .gitignore there is scoped to `recon-out/*.png` ON PURPOSE (the directory
#     also holds tracked notes — read frontend/.gitignore, it says so), so the
#     generated CSS beside them is untracked AND un-ignored. MEASURED on the
#     commit that claimed to have fixed this: a stale bundle dropped in
#     `frontend/recon-out/` brought the original false red back word for word —
#     `.chat__lightbox styling is back … recon-out/index-STALE.css:1`.
# "Not ignored" is still a deny-list; it is just written in another file. A
# deny-list of names cannot answer "did a human write this", and every time one
# is patched with one more name, the next generated directory walks past it.
#
# So the corpus is now stated the other way round: THESE ARE THE PLACES SOURCE
# LIVES. Anything outside them is out, without anyone having to have predicted
# it. `recon-out/`, `vite-out/`, and whatever the next tool invents are excluded
# because they were never included, which is the only form of exclusion that
# does not need maintaining.
#
# The obvious objection to an allow-list is that it goes stale silently — a new
# source directory appears, nobody adds it here, and the guard quietly scans
# less while still reporting green. That is a real risk and it is why the
# COVERAGE assertion below exists: every TRACKED source file under frontend/
# must fall inside one of these roots. A tracked file is by definition one a
# human committed, so anything tracked and outside the roots is either a new
# source directory (add it here — the failure names it) or something that should
# not have been committed. Generated output is never tracked, so it can never
# trip that check. The list is therefore maintained BY A RED, not by memory.
SCAN_ROOTS=(src visual-guards paint-guards scripts playwright)

# scan_specs EXT… -> git pathspecs covering every source root × every extension,
# plus top-level config files (vite.config.ts and friends are source too).
scan_specs() {
  local r e
  for e in "$@"; do
    for r in "${SCAN_ROOTS[@]}"; do printf '%s\n' "$r/*$e"; done
    printf '%s\n' ":(glob)*$e"   # (glob) so * stops at /, i.e. top level only
  done
}

# source_files DIR EXT… [EXTRA PATHSPECS…] — NUL-separated paths under DIR,
# relative to DIR, of files inside the source roots that git does not consider
# disposable: tracked, PLUS untracked-and-not-ignored.
#
# The `--others` half is load-bearing and separate from the roots question: it is
# what lets the guard see a file that has been written but not yet `git add`-ed,
# which is the state a working copy spends most of its editing life in. MEASURED:
# a new untracked `src/components/SneakyPreview.tsx` containing `<Lightbox …/>`
# is invisible to a `--cached`-only corpus. Ignoring still excludes build output
# that lands INSIDE a source root; the roots exclude the rest.
source_files() {
  local dir="$1"; shift
  local exts=() extra=() sp
  while [[ $# -gt 0 && "$1" == .* ]]; do exts+=("$1"); shift; done
  extra=("$@")
  local specs=()
  while IFS= read -r sp; do specs+=("$sp"); done < <(scan_specs ${exts[@]+"${exts[@]}"})
  # ⚠️ `${a[@]+"${a[@]}"}`, not `"${a[@]}"`. Under `set -u` bash 3.2 — which IS
  # the /bin/bash on the macOS CI runner, while a developer box with Homebrew
  # bash 5 on PATH silently does the right thing — expanding an EMPTY array is
  # an "unbound variable" error, not an empty list. MEASURED the hard way: this
  # guard went green locally on bash 5.3 and failed `bin-guards` in CI with
  # `extra[@]: unbound variable` six times over. The `+` form is the portable
  # spelling and works on both.
  ( cd "$dir" && git ls-files -z --cached --others --exclude-standard -- \
      ${specs[@]+"${specs[@]}"} ${extra[@]+"${extra[@]}"} 2>/dev/null )
}

# all_tracked_sources DIR — every TRACKED source file under DIR, roots ignored.
# Only used by the coverage assertion, to catch the roots list going stale.
all_tracked_sources() {
  ( cd "$1" && git ls-files --cached -- '*.ts' '*.tsx' '*.css' 2>/dev/null )
}

# scan_component DIR — every `<Lightbox` occurrence in DIR's production sources,
# as `relative/path:line:text`. Production = tracked .ts/.tsx that is not a test.
# `grep -H` is load-bearing in both scans: grep omits the filename when xargs
# hands it a single file, which would break the path:line positive controls for
# a reason that has nothing to do with what is being asserted. `/dev/null` is
# load-bearing too: it guarantees grep always has at least one FILE argument, so
# an empty corpus can never turn into a grep that sits reading stdin.
scan_component() {
  source_files "$1" .ts .tsx \
      ':(exclude)*.test.ts' ':(exclude)*.test.tsx' \
      ':(exclude)*.spec.ts' ':(exclude)*.spec.tsx' \
    | ( cd "$1" && xargs -0 grep -H -n -F '<Lightbox' /dev/null 2>/dev/null )
}

# scan_class DIR — every `.chat__lightbox*` RULE DECLARATION in DIR's tracked
# stylesheets.
scan_class() {
  source_files "$1" .css \
    | ( cd "$1" && xargs -0 grep -H -n -E '^[[:space:]]*\.chat__lightbox' /dev/null 2>/dev/null )
}

# count_files DIR — how many files the scans above actually looked at.
count_files() {
  source_files "$1" .ts .tsx .css | tr -cd '\0' | wc -c | tr -d ' '
}

echo "[lightbox-retired-guard] frontend tree: $FE"

# ── (0) the corpus is real ───────────────────────────────────────────────────
if [[ -d "$FE/src/components" ]]; then
  ok "frontend/src/components exists (the tree being scanned is the real one)"
else
  bad "frontend/src/components is missing — every scan below would be a vacuous pass"
fi
FILES="$(count_files "$FE")"
CORPUS_OK=1
if [[ "${FILES:-0}" -ge 100 ]]; then
  ok "scan corpus is $FILES source files (non-empty)"
else
  CORPUS_OK=0
  bad "scan corpus is only ${FILES:-0} source file(s) — either this is not a git checkout or the pathspec is not reaching the tree; refusing to report a vacuous pass"
fi
if [[ -f "$ROOT/$SURVIVOR" ]]; then
  ok "the surviving overlay is present at $SURVIVOR"
else
  bad "$SURVIVOR is missing — retiring Lightbox with no replacement leaves NO image preview"
fi

# ── (0c) 🔴 THE ROOTS LIST HAS NOT GONE STALE ────────────────────────────────
# The price of an allow-list, paid here rather than in silence. Every TRACKED
# source file must be inside a scanned root; a tracked file is one a human
# committed, so anything outside is a source directory nobody added to
# SCAN_ROOTS. Generated output is never tracked and so can never reach this.
UNCOVERED="$(comm -23 \
  <(all_tracked_sources "$FE" | sort) \
  <(source_files "$FE" .ts .tsx .css | tr '\0' '\n' | sort) )"
if [[ "$CORPUS_OK" != "1" ]]; then
  bad "coverage check skipped — the corpus is already broken, so 'nothing uncovered' would mean nothing"
elif [[ -z "$UNCOVERED" ]]; then
  ok "the roots list covers every tracked source file under frontend/ (SCAN_ROOTS is not stale)"
else
  bad "these TRACKED source files are outside every scanned root — either add the directory to SCAN_ROOTS or they should not be committed; until then this guard is silently not looking at them:"
  printf '%s\n' "$UNCOVERED" | sed 's/^/         /'
fi

# ── (0b) the corpus is SOURCE, not build output ──────────────────────────────
# ⚠️ BELT AND BRACES, NOT THE DEFENCE. Since the corpus became a positive list
# of source roots, build output is excluded by never having been included, and
# this name-matching check is the second line, not the first. It is kept because
# it catches output that lands INSIDE a source root — but read it knowing that
# it is exactly the shape that failed twice (`dist` missing `dist-paint-guard`,
# `out` missing `recon-out`), and DO NOT respond to a leak by adding a name here.
BUILT="$(source_files "$FE" .ts .tsx .css \
  | tr '\0' '\n' \
  | grep -E '(^|/)(dist[^/]*|node_modules|\.cache|build|coverage|out)/' || true)"
if [[ "$CORPUS_OK" != "1" ]]; then
  bad "build-output check found nothing, but the corpus check above already failed — an empty corpus contains no build output either, which proves nothing"
elif [[ -z "$BUILT" ]]; then
  ok "the corpus contains no build output by name either (second line of defence; the roots are the first)"
else
  bad "the corpus reached into build output — a stale bundle can carry a retired rule and redden this guard with no commit behind it:"
  printf '%s\n' "$BUILT" | sed 's/^/         /'
fi

# ── (1) zero <Lightbox in production source ──────────────────────────────────
# 🔴 A VERDICT IS ONLY WORTH WHAT ITS CORPUS IS WORTH (added in review,
# 2026-08-27). If the floor above failed, these two scans looked at nothing, and
# "nothing was found" is then the literal definition of a vacuous pass. The run
# already exits 1 in that case, so nothing was unsafe — but these two LINES said
# `ok` while resting on an empty corpus, and a line that says `ok` for a bad
# reason is how the next reader learns to trust the wrong thing.
HITS="$(scan_component "$FE")"
if [[ "$CORPUS_OK" != "1" ]]; then
  bad "<Lightbox scan found nothing, but the corpus check above already failed — this is a vacuous pass, not a clean tree"
elif [[ -z "$HITS" ]]; then
  ok "no <Lightbox in production frontend source"
else
  bad "<Lightbox is back in production source — use $SURVIVOR instead:"
  printf '%s\n' "$HITS" | sed 's/^/         /'
fi

# ── (2) zero chat__lightbox anywhere under frontend/ ─────────────────────────
CLASSHITS="$(scan_class "$FE")"
if [[ "$CORPUS_OK" != "1" ]]; then
  bad ".chat__lightbox scan found nothing, but the corpus check above already failed — this is a vacuous pass, not a clean tree"
elif [[ -z "$CLASSHITS" ]]; then
  ok "no .chat__lightbox rule declared in any frontend stylesheet"
else
  bad ".chat__lightbox styling is back — that block was deleted with the component:"
  printf '%s\n' "$CLASSHITS" | sed 's/^/         /'
fi

# ── (3) controls, in a scratch git repo ──────────────────────────────────────
# The scratch tree is a real repository because the corpus is now defined by git:
# a control that ran against a plain directory would exercise a code path this
# guard no longer has, and would go on passing after the real scan broke.
# `--template=` keeps any user's init templates and hooks out of it.
#
# It carries FOUR planted violations, and the point of the case is that they are
# byte-identical to each other and land in three different git states:
#   tracked            src/components/Planted.tsx, planted.css   → MUST be found
#   untracked, live    src/components/SneakyPreview.tsx          → MUST be found
#   ignored (built)    dist-paint-guard/, playwright/.cache/     → MUST NOT be
# The middle row is the one added in review: a cached-only corpus dropped it, and
# an editor's working copy sits in exactly that state most of the time.
WORK="$(mktemp -d -t oc-lightbox-guard.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/src/components"
cat >"$WORK/src/components/Planted.tsx" <<'EOF'
// line 1
// line 2
export function Planted() {
  return <Lightbox src={null} onClose={() => {}} />;
}
EOF
# Pad the scratch tree so the corpus check would pass on it too, keeping the
# control a test of the MATCHING, not of the sizing.
for i in $(seq 1 4); do : > "$WORK/src/components/filler$i.ts"; done
cat >"$WORK/src/components/planted.css" <<'EOF'
.chat__lightbox { position: fixed; }
EOF
# The scratch tree models the real one: build output is IGNORED, not merely
# untracked. That is what makes "version control is the filter" true without
# costing the guard its reach over work in progress.
cat >"$WORK/.gitignore" <<'EOF'
dist-paint-guard/
playwright/.cache
EOF
mkdir -p "$WORK/dist-paint-guard/assets" "$WORK/playwright/.cache/assets"
cp "$WORK/src/components/Planted.tsx" "$WORK/dist-paint-guard/assets/bundle.tsx"
cp "$WORK/src/components/planted.css" "$WORK/playwright/.cache/assets/index-DEADBEEF.css"
# …and the work-in-progress file: real, un-added, NOT ignored, INSIDE a root.
cp "$WORK/src/components/Planted.tsx" "$WORK/src/components/SneakyPreview.tsx"
# 🔴 THE K5 DECOY — the one that brought the false red back. Untracked AND
# un-ignored, exactly like frontend/recon-out/'s generated CSS, and therefore
# invisible to both "is it committed" and "is it ignored". The ONLY thing that
# excludes it is that it does not live in a source root, which is what this
# control exists to pin.
mkdir -p "$WORK/recon-out"
cp "$WORK/src/components/planted.css" "$WORK/recon-out/index-STALE.css"

git -C "$WORK" init -q --template= 2>/dev/null
git -C "$WORK" add -f .gitignore src/components/Planted.tsx src/components/planted.css >/dev/null 2>&1
for i in $(seq 1 4); do git -C "$WORK" add -f "src/components/filler$i.ts" >/dev/null 2>&1; done

# The controls below are all of the form "X was not reported". Each one is only
# worth anything if X exists and really does contain the violation — otherwise
# the guard is being congratulated for a missing file.
if [[ -s "$WORK/dist-paint-guard/assets/bundle.tsx" && -s "$WORK/playwright/.cache/assets/index-DEADBEEF.css" ]] \
   && grep -qF '<Lightbox' "$WORK/dist-paint-guard/assets/bundle.tsx" \
   && grep -qE '^[[:space:]]*\.chat__lightbox' "$WORK/playwright/.cache/assets/index-DEADBEEF.css" \
   && [[ -n "$(git -C "$WORK" check-ignore dist-paint-guard/assets/bundle.tsx playwright/.cache/assets/index-DEADBEEF.css 2>/dev/null)" ]]; then
  ok "control setup: both build-output decoys exist, contain the violation, and are really git-ignored (so 'not reported' below is a filter working, not a file missing)"
else
  bad "control setup: the build-output decoys are missing, do not contain the violation, or are not actually ignored — the version-control assertions below would be vacuous"
fi
if [[ -s "$WORK/recon-out/index-STALE.css" ]] \
   && grep -qE '^[[:space:]]*\.chat__lightbox' "$WORK/recon-out/index-STALE.css" \
   && [[ -z "$(git -C "$WORK" check-ignore recon-out/index-STALE.css 2>/dev/null)" ]] \
   && [[ -z "$(git -C "$WORK" ls-files -- recon-out/index-STALE.css)" ]]; then
  ok "control setup: the out-of-root decoy exists, contains the rule, and is NEITHER tracked NOR ignored (so only the source roots can be excluding it)"
else
  bad "control setup: the out-of-root decoy is missing, clean, ignored, or tracked — the assertion below would be passing for the wrong reason"
fi
if [[ -s "$WORK/src/components/SneakyPreview.tsx" ]] \
   && grep -qF '<Lightbox' "$WORK/src/components/SneakyPreview.tsx" \
   && [[ -z "$(git -C "$WORK" ls-files -- src/components/SneakyPreview.tsx)" ]]; then
  ok "control setup: the work-in-progress violation exists, contains <Lightbox, and is genuinely UN-tracked (so finding it below means reach, not luck)"
else
  bad "control setup: the work-in-progress decoy is missing, empty, or was accidentally staged — the reach assertion below would prove nothing"
fi

CTRL="$(scan_component "$WORK")"
if [[ "$CTRL" == *"src/components/Planted.tsx:4:"* ]]; then
  ok "positive control: the planted <Lightbox is reported at src/components/Planted.tsx:4"
else
  bad "positive control did not name the planted path:line — the component scan is not matching what it claims (got: ${CTRL:-<nothing>})"
fi

CTRLCLASS="$(scan_class "$WORK")"
if [[ "$CTRLCLASS" == *"src/components/planted.css:1:"* ]]; then
  ok "positive control: the planted chat__lightbox rule is reported at src/components/planted.css:1"
else
  bad "positive control did not name the planted stylesheet path:line — the class scan is not matching what it claims (got: ${CTRLCLASS:-<nothing>})"
fi

# 🔴 REACH: the corpus must not shrink to "what somebody remembered to `git add`".
if [[ "$CTRL" == *"src/components/SneakyPreview.tsx:4:"* ]]; then
  ok "reach: an UNTRACKED, un-ignored source file is still scanned — the guard speaks before the file is staged, not after"
else
  bad "reach: the untracked src/components/SneakyPreview.tsx was NOT reported — the corpus has shrunk to staged work only, and the guard is now silent for most of the time anyone is editing (got: ${CTRL:-<nothing>})"
fi

# 🔴 THE ASSERTION THIS GUARD WAS MISSING. Everything above proves the scan
# WORKS; this proves it works on the right corpus. Both decoys are inside the
# scanned tree, both contain the exact violation, both are ignored.
if [[ "$CTRL" != *dist-paint-guard* && "$CTRL" != *".cache"* ]]; then
  ok "version control is the filter: the identical <Lightbox in dist-paint-guard/ was NOT reported"
else
  bad "the component scan reported a violation from build output — this is the false red T-1a7d fixed, come back (got: $CTRL)"
fi
if [[ "$CTRLCLASS" != *".cache"* && "$CTRLCLASS" != *dist-paint-guard* ]]; then
  ok "version control is the filter: the identical .chat__lightbox rule in playwright/.cache/assets/ was NOT reported"
else
  bad "the class scan reported a rule from a compiled bundle — this is the false red T-1a7d fixed, come back (got: $CTRLCLASS)"
fi

# 🔴 K5: THE ASSERTION THIS GUARD LOST AND GOT BACK. A generated file that is
# neither committed nor ignored must not be scanned. Before the source roots,
# this exact shape reproduced the original false red on the commit that claimed
# to have fixed it.
if [[ "$CTRLCLASS" != *recon-out* ]]; then
  ok "source roots are the filter: an identical .chat__lightbox rule in recon-out/ — untracked AND un-ignored — was NOT reported"
else
  bad "the class scan reported a rule from recon-out/, which is neither committed nor ignored — this is the T-1a7d false red for the THIRD time, in the same shape wearing different clothes (got: $CTRLCLASS)"
fi

# NEGATIVE control: the same scratch tree with the violations removed must come
# back clean. Without this, a scan that reports every file would satisfy every
# positive control above and still be worthless.
git -C "$WORK" rm -q --cached "src/components/Planted.tsx" "src/components/planted.css" >/dev/null 2>&1
rm "$WORK/src/components/Planted.tsx" "$WORK/src/components/planted.css" "$WORK/src/components/SneakyPreview.tsx" "$WORK/recon-out/index-STALE.css"
if [[ -z "$(scan_component "$WORK")" && -z "$(scan_class "$WORK")" ]]; then
  ok "negative control: a clean tree reports nothing"
else
  bad "negative control: a clean tree still reported hits — the scan matches indiscriminately"
fi

echo
echo "[lightbox-retired-guard] $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
