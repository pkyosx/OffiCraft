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

# source_files DIR [PATHSPECS…] — NUL-separated paths under DIR, relative to DIR,
# of every file git does not consider disposable: tracked files PLUS untracked
# ones that are not ignored, MINUS everything .gitignore covers.
#
# 🔴 `--others --exclude-standard` IS LOAD-BEARING, not tidiness (added in review,
# 2026-08-27). Restricting the corpus to `--cached` alone is a real loss of reach
# and the wrong one: it blinds the guard to a file that exists on disk, is about
# to be committed, and has not been `git add`-ed yet — which is the state a
# working copy is in for most of the time anyone is editing it, and precisely
# when this guard is supposed to speak. MEASURED: a new untracked
# `src/components/SneakyPreview.tsx` containing `<Lightbox …/>` was caught by the
# old `find` walk, and was NOT caught by the cached-only version.
#
# Nothing is given up by widening it, because build output is not merely
# untracked, it is IGNORED — `frontend/.gitignore` names `playwright/.cache` and
# `dist-paint-guard/`, so `--exclude-standard` drops it. MEASURED on a built
# working copy: `--cached` and `--cached --others --exclude-standard` both return
# 480 files, and neither contains any of the 17 generated assets.
#
# So the filter is not "committed" — it is "git thinks a human is responsible for
# this file", and the two assertions below pin both halves of that.
source_files() {
  local dir="$1"; shift
  ( cd "$dir" && git ls-files -z --cached --others --exclude-standard -- "$@" 2>/dev/null )
}

# scan_component DIR — every `<Lightbox` occurrence in DIR's production sources,
# as `relative/path:line:text`. Production = tracked .ts/.tsx that is not a test.
# `grep -H` is load-bearing in both scans: grep omits the filename when xargs
# hands it a single file, which would break the path:line positive controls for
# a reason that has nothing to do with what is being asserted. `/dev/null` is
# load-bearing too: it guarantees grep always has at least one FILE argument, so
# an empty corpus can never turn into a grep that sits reading stdin.
scan_component() {
  source_files "$1" \
      '*.ts' '*.tsx' \
      ':(exclude)*.test.ts' ':(exclude)*.test.tsx' \
      ':(exclude)*.spec.ts' ':(exclude)*.spec.tsx' \
    | ( cd "$1" && xargs -0 grep -H -n -F '<Lightbox' /dev/null 2>/dev/null )
}

# scan_class DIR — every `.chat__lightbox*` RULE DECLARATION in DIR's tracked
# stylesheets.
scan_class() {
  source_files "$1" '*.css' \
    | ( cd "$1" && xargs -0 grep -H -n -E '^[[:space:]]*\.chat__lightbox' /dev/null 2>/dev/null )
}

# count_files DIR — how many files the scans above actually looked at.
count_files() {
  source_files "$1" '*.ts' '*.tsx' '*.css' | tr -cd '\0' | wc -c | tr -d ' '
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

# ── (0b) 🔴 the corpus is SOURCE, not build output ───────────────────────────
# The assertion that this guard reads what humans wrote. It is stated over the
# REAL tree (not just the scratch control) because the polluting directories only
# exist on a machine that has actually built the frontend — which is where the
# false red happened. On a never-built checkout this is trivially true; on a
# built one it is the whole ballgame.
BUILT="$(source_files "$FE" '*.ts' '*.tsx' '*.css' \
  | tr '\0' '\n' \
  | grep -E '(^|/)(dist[^/]*|node_modules|\.cache|build|coverage|out)/' || true)"
if [[ -z "$BUILT" ]]; then
  ok "the corpus contains no build output (nothing under dist*/, node_modules/, .cache/, build/, coverage/, out/)"
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
# …and the work-in-progress file: real, un-added, NOT ignored.
cp "$WORK/src/components/Planted.tsx" "$WORK/src/components/SneakyPreview.tsx"

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

# NEGATIVE control: the same scratch tree with the violations removed must come
# back clean. Without this, a scan that reports every file would satisfy every
# positive control above and still be worthless.
git -C "$WORK" rm -q --cached "src/components/Planted.tsx" "src/components/planted.css" >/dev/null 2>&1
rm "$WORK/src/components/Planted.tsx" "$WORK/src/components/planted.css" "$WORK/src/components/SneakyPreview.tsx"
if [[ -z "$(scan_component "$WORK")" && -z "$(scan_class "$WORK")" ]]; then
  ok "negative control: a clean tree reports nothing"
else
  bad "negative control: a clean tree still reported hits — the scan matches indiscriminately"
fi

echo
echo "[lightbox-retired-guard] $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
