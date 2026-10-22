# officraft TCC anchor build record

Source commit: `965a416` (the source snapshot before this binary/provenance commit)

Build command:

```sh
(cd cli/officraft && go build -trimpath -buildvcs=false -ldflags='-s -w' -o ../../dist/officraft/officraft ./...)
find cli/officraft \( -type f -o -type l \) \
  \( -name '*.go' -o -name '*.s' -o -name 'go.mod' -o -name 'go.sum' \) \
  ! -name '*_test.go' | LC_ALL=C sort | xargs shasum -a 256 | shasum -a 256   # -> source.sha256
(cd dist/officraft && shasum -a 256 officraft > binary.sha256)
```

Both flags are load-bearing, and neither is cosmetic:

- `-trimpath` keeps the build directory out of the bytes, so the same source
  built in two places produces the same binary.
- `-buildvcs=false` keeps the git revision, build time and dirty-tree flag out.
  Without it the anchor's bytes change on **every commit** even when the source
  is untouched, and a reviewer rebuilding from a fresh clone can never reproduce
  the committed bytes. Measured, not assumed: with `-trimpath` alone, the same
  source built inside the repo and outside it differed.

`bin/build-bindist` builds the shipped anchor with exactly these flags, so the
committed copy and the shipped copy are the same bytes.

Two records, because either one alone can be satisfied by a lie:

- `source.sha256` — the aggregate hash of every non-test source file in the
  module: `.go`, `.s`, `go.mod`, `go.sum`, symlinks included, with the file list
  riding inside the digest so adding or renaming a file counts as a change. CI recomputes it, so changing the source without
  refreshing this record fails.
- `binary.sha256` — the hash of the executable sitting next to it. Without it,
  refreshing `source.sha256` by hand (or replacing the executable outright)
  leaves CI perfectly green.

Verify the executable against its record at any time:

```sh
(cd dist/officraft && shasum -a 256 -c binary.sha256)
```

CI runs `bin/check-officraft-dist`, which checks both.
