#!/usr/bin/env bash
# officraft — the platforms member binaries are embedded for and release tarballs
# ship for (GOOS/GOARCH). Changing it changes the release asset set and what
# servers can serve.
OC_MEMBER_PLATFORMS="darwin/arm64 linux/amd64 linux/arm64"

# platform_file_tokens <os-arch> — every token `file -b` must print for a
# binary of that platform, one per line.
platform_file_tokens() {
  case "$1" in
    darwin-arm64) printf '%s\n' "Mach-O" "arm64" "executable" ;;
    linux-amd64)  printf '%s\n' "ELF 64-bit" "x86-64" "executable" ;;
    linux-arm64)  printf '%s\n' "ELF 64-bit" "ARM aarch64" "executable" ;;
    *) return 1 ;;
  esac
}

# binary_platform_mismatch <file> <os-arch> — prints what `file -b` says when
# <file> is not a binary of that platform; prints nothing when it is.
# Each token is matched on its own, not as an ordered glob: `file -b` on a Go
# arm64 binary says "Mach-O 64-bit executable arm64", architecture last, and
# file(1)'s word order is not a contract.
binary_platform_mismatch() {
  local desc tok
  desc="$(file -b "$1")"
  while IFS= read -r tok; do
    [[ "$desc" == *"$tok"* ]] || { printf '%s\n' "$desc"; return; }
  done < <(platform_file_tokens "$2")
}
