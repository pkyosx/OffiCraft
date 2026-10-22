#!/usr/bin/env bash
# bin/tests/go-security-bump-guard.sh — hermetic tests for bin/go-security-bump.py.
# govulncheck, the go.dev release list and `go build` are replaced by fixtures, so
# the verdicts below do not move with the real vulnerability database.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
SCRIPT="$ROOT/bin/go-security-bump.py"
[[ -f "$SCRIPT" ]] || { echo "FATAL: $SCRIPT not found" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi; }
has()  { if grep -qF -- "$2" <<<"$3"; then ok "$1"; else bad "$1 (no '$2' in: $3)"; fi; }

WORK="$(mktemp -d -t oc-go-security-bump.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT
SHIMS="$WORK/shims"
mkdir -p "$SHIMS"

# govulncheck shim: prints $FIXTURE_DIR/<module dir name>.json, or a clean scan.
cat > "$SHIMS/govulncheck" <<'SH'
#!/usr/bin/env bash
f="$FIXTURE_DIR/$(basename "$PWD").json"
if [[ -f "$f" ]]; then cat "$f"; else
  printf '{"config":{"go_version":"%s","scan_level":"symbol"}}\n' "$SCAN_GO_VERSION"
fi
SH
# go shim: `go build ... -o OUT ./...` writes bytes derived from the module source.
cat > "$SHIMS/go" <<'SH'
#!/usr/bin/env bash
out=""; while [[ $# -gt 0 ]]; do [[ "$1" == "-o" ]] && out="$2"; shift; done
{ echo fake-officraft; cat go.mod main.go; } > "$out"; chmod +x "$out"
SH
chmod +x "$SHIMS/govulncheck" "$SHIMS/go"

new_repo() { # new_repo DIR GOVERSION
  local d="$1" v="$2"
  mkdir -p "$d/bin" "$d/.github/workflows" "$d/docs/guide" "$d/dist/officraft"
  cp "$SCRIPT" "$ROOT/bin/check-officraft-dist" "$d/bin/"
  for m in server/ocserverd cli/ocagent cli/ocwarden cli/officraft; do
    mkdir -p "$d/$m"; printf 'module %s\n\ngo %s\n' "$(basename "$m")" "$v" > "$d/$m/go.mod"
  done
  echo 'package main' > "$d/cli/officraft/main.go"
  cat > "$d/.github/workflows/ci.yml" <<YML
      # Pinned to what the repo develops against (go.mod says $v).
      - uses: actions/setup-go@v6
        with:
          go-version: '$v'
      - uses: actions/setup-go@v6
        with:
          go-version: '$v'
YML
  printf '> 版本依據：**Go 1.26** 取自 repo 的 `go.mod`（`go %s`）；\n' "$v" > "$d/docs/guide/install.md"
  printf '# build record\n\nSource commit: `0000000` (the source snapshot)\n' > "$d/dist/officraft/BUILD.md"
  (cd "$d" && git init -q && git add -A && git -c user.name=t -c user.email=t@t commit -qm init)
}

finding() { # finding OSV FIXED MODULE [FUNCTION]
  local fn=""; [[ -n "${4:-}" ]] && fn=",\"function\":\"$4\""
  printf '{"finding":{"osv":"%s","fixed_version":"%s","trace":[{"module":"%s","version":"v1.26.4"%s}]}}\n' "$1" "$2" "$3" "$fn"
}

releases() { # releases VERSION...
  local items=() v
  for v in "$@"; do items+=("{\"version\":\"go$v\",\"stable\":true}"); done
  (IFS=,; echo "[${items[*]}]") > "$WORK/releases.json"
}

scan() { # scan REPO FIXTURE_DIR — sets RC, OUT, ERR
  OUT="$(cd "$1" && PATH="$SHIMS:$PATH" FIXTURE_DIR="$2" SCAN_GO_VERSION="${SCAN_GO_VERSION:-go1.26.4}" \
         GO_RELEASES_URL="file://$WORK/releases.json" python3 bin/go-security-bump.py scan 2>"$WORK/err")"
  RC=$?; ERR="$(cat "$WORK/err")"
}

echo "== bin/go-security-bump.py =="
REPO="$WORK/repo"; new_repo "$REPO" 1.26.4
CFG='{"config":{"go_version":"go1.26.4","scan_level":"symbol"}}'
releases 1.27.3 1.26.9 1.26.10 1.25.12

# ── scan ────────────────────────────────────────────────────────────────────
mkdir -p "$WORK/clean"
scan "$REPO" "$WORK/clean"
check "scan of a clean repo exits 0" "0" "$RC"
check "scan of a clean repo plans nothing" "" "$OUT"

F="$WORK/fixable"; mkdir -p "$F"
{ echo "$CFG"; finding GO-2026-6809 v1.26.10 stdlib crypto/x509.Verify; finding GO-2026-6603 v1.26.9 stdlib net/http.Serve; } > "$F/ocagent.json"
{ echo "$CFG"; finding GO-2026-6603 v1.26.9 stdlib net/http.Serve; finding GO-2026-7000 v1.26.6 toolchain cmd/go.Run; } > "$F/ocserverd.json"
scan "$REPO" "$F"
check "scan with fixable stdlib and toolchain findings exits 0" "0" "$RC"
check "scan with fixable findings plans a bump to the newest patch of the same minor, listing every vuln once" \
  '{"from": "1.26.4", "to": "1.26.10", "vulns": ["GO-2026-6603", "GO-2026-6809", "GO-2026-7000"]}' "$OUT"

F="$WORK/unreached"; mkdir -p "$F"
{ echo "$CFG"; finding GO-2026-6809 v1.26.10 stdlib; finding GO-2026-9001 v0.3.1 golang.org/x/net golang.org/x/net/html.Parse; } > "$F/ocserverd.json"
scan "$REPO" "$F"
check "scan with only unreached stdlib and third-party findings exits 0" "0" "$RC"
check "scan with only unreached stdlib and third-party findings plans nothing" "" "$OUT"

F="$WORK/unfixed"; mkdir -p "$F"
{ echo "$CFG"; finding GO-2026-8000 "" stdlib os.Open; } > "$F/ocwarden.json"
scan "$REPO" "$F"
check "scan with a reached finding that has no fixed release exits 0" "0" "$RC"
check "scan with a reached finding that has no fixed release plans nothing" "" "$OUT"
has "scan with a reached finding that has no fixed release names it" "no fixed release yet: GO-2026-8000" "$ERR"

F="$WORK/nextminor"; mkdir -p "$F"
{ echo "$CFG"; finding GO-2026-8100 v1.27.4 stdlib net/http.Serve; } > "$F/officraft.json"
releases 1.27.4 1.26.10
scan "$REPO" "$F"
check "scan whose fix exists only in the next minor exits 1" "1" "$RC"
has "scan whose fix exists only in the next minor says a minor upgrade is needed" \
  "go 1.26.10 is the newest 1.26 release but does not fix GO-2026-8100; a minor-version upgrade is needed" "$ERR"

releases 1.28.0 1.27.4
scan "$REPO" "$WORK/fixable"
check "scan when the pinned minor left the release list exits 1" "1" "$RC"
has "scan when the pinned minor left the release list says so" "go 1.26 is no longer in the supported release list" "$ERR"

releases 1.27.3 1.26.10
SCAN_GO_VERSION=go1.26.10 scan "$REPO" "$WORK/clean"
check "scan run on a toolchain other than the pinned go exits 1" "1" "$RC"
has "scan run on a toolchain other than the pinned go names both versions" \
  "server/ocserverd was scanned with go1.26.10, but the repo pins go 1.26.4" "$ERR"

# ── apply ───────────────────────────────────────────────────────────────────
(cd "$REPO" && python3 bin/go-security-bump.py apply 1.26.4 1.26.10 2>"$WORK/err"); RC=$?
check "apply 1.26.4 -> 1.26.10 exits 0" "0" "$RC"
check "apply rewrites every go.mod" \
  "go 1.26.10|go 1.26.10|go 1.26.10|go 1.26.10" \
  "$(cd "$REPO" && for m in server/ocserverd cli/ocagent cli/ocwarden cli/officraft; do sed -n 3p "$m/go.mod"; done | paste -sd'|' -)"
check "apply rewrites every setup-go pin and the go.mod comment in ci.yml" \
  "      # Pinned to what the repo develops against (go.mod says 1.26.10).
      - uses: actions/setup-go@v6
        with:
          go-version: '1.26.10'
      - uses: actions/setup-go@v6
        with:
          go-version: '1.26.10'" "$(cat "$REPO/.github/workflows/ci.yml")"
check "apply rewrites the install guide" '> 版本依據：**Go 1.26** 取自 repo 的 `go.mod`（`go 1.26.10`）；' "$(cat "$REPO/docs/guide/install.md")"

DRIFT="$WORK/drift"; new_repo "$DRIFT" 1.26.4
printf '> Go version: 1.26.4\n' > "$DRIFT/docs/guide/install.md"
(cd "$DRIFT" && python3 bin/go-security-bump.py apply 1.26.4 1.26.10 2>"$WORK/err"); RC=$?
check "apply on a file whose Go version spelling drifted exits 1" "1" "$RC"
has "apply on a file whose Go version spelling drifted names the file" "docs/guide/install.md: no Go version found to rewrite" "$(cat "$WORK/err")"

LEFT="$WORK/leftover"; new_repo "$LEFT" 1.26.4
printf "          go-version: '1.26.4'\n          go-version: \"1.26.4\"\n" >> "$LEFT/.github/workflows/ci.yml"
(cd "$LEFT" && python3 bin/go-security-bump.py apply 1.26.4 1.26.10 2>"$WORK/err"); RC=$?
check "apply leaving an unknown spelling of the old version behind exits 1" "1" "$RC"
has "apply leaving an unknown spelling behind lists where" '.github/workflows/ci.yml:9:          go-version: "1.26.4"' "$(cat "$WORK/err")"

# ── anchor ──────────────────────────────────────────────────────────────────
(cd "$REPO" && PATH="$SHIMS:$PATH" python3 bin/go-security-bump.py anchor 965a416055bd 2>"$WORK/err"); RC=$?
check "anchor exits 0" "0" "$RC"
check "anchor stamps the source commit into BUILD.md" 'Source commit: `965a416` (the source snapshot)' "$(sed -n 3p "$REPO/dist/officraft/BUILD.md")"
check "anchor records the rebuilt binary" \
  "$(printf 'fake-officraft\nmodule officraft\n\ngo 1.26.10\npackage main\n' | shasum -a 256 | awk '{print $1}')  officraft" \
  "$(cat "$REPO/dist/officraft/binary.sha256")"
(cd "$REPO" && bash bin/check-officraft-dist); RC=$?
check "anchor leaves a dist that bin/check-officraft-dist accepts" "0" "$RC"

echo "go-security-bump: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
