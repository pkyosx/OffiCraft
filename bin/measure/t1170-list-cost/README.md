# T-1170 — what the three list answers cost, before and after

Measures the REAL responses of the three list endpoints against a REAL
`ocserverd`, on two worktrees, with one identical corpus.

```
bin/measure/t1170-list-cost/run.sh <before-worktree> <after-worktree> [outdir]
```

Each arm builds `ocserverd` from its worktree, migrates a throwaway SQLite, serves
on a kernel-assigned port, seeds the corpus through the public API, reads each
endpoint three times, and tears its own daemon down. `compare.py` then prints the
table — or refuses.

## What makes this evidence rather than two numbers

`compare.py` **exits non-zero** when the arms are not the same experiment: it
compares `_caps` and `_corpus_chars` field by field and refuses on any
difference.

That refusal exists because the first version of this harness did not have it.
It *printed* the caps and the corpus into each arm's JSON and carried a comment
claiming the two arms were pinned to each other — but nothing ever opened both
files, so nothing could ever disagree. A reviewer falsified it in one move:
raise `doc.cap_chars.duty` to 2000 on the after arm only (which doubles the
role-definition corpus, since the corpus is sized off the cap) and the run still
exited 0, printed no warning, and produced a flattering before/after story out
of two arms measuring different worlds.

A claim nothing can falsify is not evidence. `bin/tests/t1170-cost-compare-guard.sh`
replays that exact counterexample and fails if `compare.py` ever goes quiet
about it again.

## Two traps that produce a confident wrong number

**`wc -m` counts BYTES here.** This host has no `LANG` set. For the CJK corpus
that over-reports by roughly 3×, in the direction that flatters the change — and
it makes the "before" figure incomparable to the character cap it was supposedly
filling. Everything here counts Unicode code points (`len()` of a `str` decoded
from UTF-8), the unit the server's own caps are expressed in.

**`bin/build-webdist` builds the SPA in MOCK mode.** It runs `npm run build`,
where `USE_MOCK` defaults to true, so a cockpit built that way never reaches the
server at all — every screen "works" and none of it is evidence. To drive the
real thing, rebuild with `VITE_USE_MOCK=false` (see `e2e_test/setup.sh`).

## Reading the numbers

The corpus fills each capped document to **90% of the cap the server reports**.
That is close to the WORST case on purpose — the change is about what a list
answer costs when its documents are long, and a cap is where a long-lived
document ends up. The ratios are the ceiling the old shape had, **not** typical
load; `list_task_manuals` in particular improves by ~117× here because three
near-full manuals are exactly the shape that was being inlined.

Per-endpoint counts are read three times and the spread is reported.

⚠️ That spread is **read-to-read within one run**, and it is normally 0 — which
must not be read as "the figure is exact to the character". A **fresh run**
seeds fresh `updated_ts` floats, and their shortest round-trip repr can be a
digit longer, so the same measurement taken twice has been observed to differ by
about 1 (`list_task_manuals` has come out as both 719 and 720). Nothing here
measures that; treat the last digit as noise and do not quote these numbers to
the character.
