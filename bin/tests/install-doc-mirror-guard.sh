#!/usr/bin/env bash
# bin/tests/install-doc-mirror-guard.sh — README.md and docs/guide/install.md
# carry the SAME install instructions word for word (owner ruling): the
# README for GitHub readers, the guide for the cockpit's 使用說明 and for Mira
# (get_doc). Edit one copy and forget the other, and a reader of one gets steps
# the other no longer gives. Nothing else compares the two files.
#
# WHAT IS COMPARED: from the line `### 系統需求` through the end of the
# `### 確認安裝成功` section (the next `###`/`##` heading or `---`), byte for byte.
# Each file's own material around that block is deliberately NOT compared.
#
# WHY HEADINGS AND NOT MARKER COMMENTS: the cockpit's Markdown renderer has no
# HTML/comment syntax, so a `<!-- BEGIN -->` marker would print as visible text
# in 使用說明.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

FILES=(README.md docs/guide/install.md)
START='### 系統需求'
LAST='### 確認安裝成功'

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

extract() {
  awk -v s="$START" -v l="$LAST" '
    $0 == s { on = 1 }
    on && seen && ($0 ~ /^### / || $0 ~ /^## / || $0 == "---") { exit }
    on && $0 == l { seen = 1 }
    on { print }
  ' "$1" | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}'
}

for f in "${FILES[@]}"; do
  [[ -f "$f" ]] || { echo "FAIL — $f is missing" >&2; exit 1; }
  for h in "$START" "$LAST"; do
    if ! grep -qxF -- "$h" "$f"; then
      echo "FAIL — $f has no line '$h'. The shared install block is delimited by that" >&2
      echo "       heading; renaming it in one file makes this guard compare nothing." >&2
      exit 1
    fi
  done
  out="$tmpdir/$(echo "$f" | tr / _)"
  extract "$f" > "$out"
  lines=$(wc -l < "$out")
  if [[ "$lines" -lt 40 ]]; then
    echo "FAIL — the install block extracted from $f is only $lines lines; the headings" >&2
    echo "       are probably out of order or the block was cut short." >&2
    exit 1
  fi
done

a="$tmpdir/README.md"; b="$tmpdir/docs_guide_install.md"
if ! diff -u "$a" "$b" > "$tmpdir/diff.txt"; then
  echo "FAIL — the install block differs between README.md and docs/guide/install.md." >&2
  echo "       Copy the block ($START … end of $LAST) verbatim into both files." >&2
  sed -n '1,60p' "$tmpdir/diff.txt" >&2
  exit 1
fi
echo "ok — README.md and docs/guide/install.md carry the same install block ($(wc -l < "$a" | tr -d ' ') lines)"
