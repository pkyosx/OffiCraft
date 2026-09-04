// lib/lineDiff.ts — a hand-rolled, dependency-free line-level diff producing a
// git-style unified model (context / added / removed rows carrying the 1-based
// line number on each side).
//
// Hand-rolled on purpose, like components/Markdown.tsx: the repo ships no diff
// library, and the doc-history view needs exactly one thing — "which lines of
// this revision differ from the previous one" — which Myers' 1986 difference
// algorithm fits in under 300 lines with no supply-chain surface and no bundle
// cost.

export type DiffRowKind = "context" | "added" | "removed";

export interface DiffRow {
  kind: DiffRowKind;
  text: string;
  /** 1-based line number on the `before` side, null for an added line. */
  beforeLine: number | null;
  /** 1-based line number on the `after` side, null for a removed line. */
  afterLine: number | null;
}

/** One contiguous block of rows the UI should render, git `@@` style. */
export interface DiffHunk {
  rows: DiffRow[];
  /** Unchanged lines collapsed away immediately before this hunk — the number
   * the `@@`-style separator reports. 0 means the hunk starts at the top. */
  skippedBefore: number;
  /** 1-based first/last-inclusive extents, 0 when the hunk touches neither
   * side (only possible for an empty hunk, which is never emitted). */
  beforeStart: number;
  beforeCount: number;
  afterStart: number;
  afterCount: number;
}

export interface LineDiffResult {
  /** "too-large" means nothing was diffed — see `maxLines`. */
  status: "diffed" | "too-large";
  /** The full ordered edit script. Empty when status is "too-large". */
  rows: DiffRow[];
  /** `rows` grouped for display. With collapsing off this is a single hunk
   * holding every row; with it on, only changes plus their context radius. An
   * identical pair yields no hunks at all. */
  hunks: DiffHunk[];
  beforeLineCount: number;
  afterLineCount: number;
}

export interface LineDiffOptions {
  /** Drop unchanged runs further than `contextRadius` from any change. */
  collapseUnchanged?: boolean;
  contextRadius?: number;
  /** Per-side line ceiling above which the diff is refused. */
  maxLines?: number;
}

const DEFAULT_CONTEXT_RADIUS = 3;

// A ceiling still exists, but it now guards TIME, not memory: the search costs
// O((N+M)*D), so a pair with nothing in common still degrades toward quadratic
// even though the working set stays at 16(N+M)+24 bytes (~586 KB at 18k lines a
// side, where the old table wanted 1.34 GB).
//
// 20,000 is GitHub's own published per-file diff limit, and the measurements say
// it is affordable for the input this actually sees: two versions of ONE file,
// ~1% of the lines changed, which lands in tens of milliseconds at 20k lines a
// side.
//
// The pathological input — two 20,000-line documents with NOTHING in common —
// stalls for SECONDS, and that is accepted rather than solved. No absolute
// figure is quoted here on purpose: two runs on this same machine measured
// 3.7 s and 5.7 s for the same input, so a number written into this comment is
// a measurement that rots (CLAUDE.md §4). Re-measure rather than cite.
//
// 🔴 AND THE DIFF IS ONLY HALF THE STALL. DiffView renders every row (no
// virtualisation — the owner's "do not collapse" ruling) and runs wordDiff over
// each paired change, so the pathological input also builds 40,000 rows
// synchronously before first paint, with no loading state and no cancel. That
// half has never been measured. Anyone raising this ceiling further owns that
// question first.
//
// The alternative — refusing at a lower ceiling — is what put the wall in front
// of dal.go, reconcile.go and wire.go, which is the whole reason this changed.
const DEFAULT_MAX_LINES = 20000;

/**
 * Split into lines losslessly. `\r\n` and `\n` both terminate a line, and a
 * trailing terminator does NOT produce a final empty line — but only one such
 * segment is dropped, so a genuinely blank last line ("a\n\n") survives.
 */
export function splitLines(text: string): string[] {
  if (text === "") return [];
  const lines = text.split(/\r\n|\n/);
  if (lines[lines.length - 1] === "") lines.pop();
  return lines;
}

/** The furthest-reaching D-path frontier plus the middle snake it met on. The
 * object is reused across the whole recursion so bisecting allocates nothing. */
interface MiddleSnake {
  xStart: number;
  yStart: number;
  xEnd: number;
  yEnd: number;
  /** Edit distance of the region — 0 or 1 means the region needs no bisecting. */
  d: number;
}

/**
 * Myers 1986 §4b: run the greedy D-path search from both ends at once and stop
 * the moment the two frontiers overlap, which pins down one snake the optimal
 * script provably passes through. Only the two frontier vectors are kept, so the
 * whole search is O(N+M) space instead of the O(N*M) a full table costs; the
 * halves either side of the snake are then solved the same way.
 *
 * `vf`/`vr` are indexed by diagonal k = x - y biased by `offset`; the reverse
 * search runs in coordinates measured back from (n,m), so its diagonal k' maps
 * to the forward diagonal as k = delta - k' and the two meet when their reaches
 * sum to at least n.
 */
function findMiddleSnake(
  a: string[],
  b: string[],
  a0: number,
  n: number,
  b0: number,
  m: number,
  vf: Int32Array,
  vr: Int32Array,
  offset: number,
  out: MiddleSnake
): void {
  const delta = n - m;
  const odd = (delta & 1) !== 0;
  const dmax = Math.ceil((n + m) / 2);
  vf[offset + 1] = 0;
  vr[offset + 1] = 0;

  for (let d = 0; d <= dmax; d++) {
    for (let k = -d; k <= d; k += 2) {
      // Falling back to the k+1 frontier is a step down (an insertion); ties go
      // to k-1, the step right, so a replaced line reads "-old" then "+new".
      let x =
        k === -d || (k !== d && vf[offset + k - 1] < vf[offset + k + 1])
          ? vf[offset + k + 1]
          : vf[offset + k - 1] + 1;
      let y = x - k;
      const xStart = x;
      const yStart = y;
      while (x < n && y < m && a[a0 + x] === b[b0 + y]) {
        x++;
        y++;
      }
      vf[offset + k] = x;
      const kr = delta - k;
      if (odd && kr >= -(d - 1) && kr <= d - 1 && x + vr[offset + kr] >= n) {
        out.xStart = a0 + xStart;
        out.yStart = b0 + yStart;
        out.xEnd = a0 + x;
        out.yEnd = b0 + y;
        out.d = 2 * d - 1;
        return;
      }
    }

    for (let k = -d; k <= d; k += 2) {
      let x =
        k === -d || (k !== d && vr[offset + k - 1] < vr[offset + k + 1])
          ? vr[offset + k + 1]
          : vr[offset + k - 1] + 1;
      let y = x - k;
      const xStart = x;
      const yStart = y;
      while (x < n && y < m && a[a0 + n - 1 - x] === b[b0 + m - 1 - y]) {
        x++;
        y++;
      }
      vr[offset + k] = x;
      const kf = delta - k;
      if (!odd && kf >= -d && kf <= d && x + vf[offset + kf] >= n) {
        out.xStart = a0 + n - x;
        out.yStart = b0 + m - y;
        out.xEnd = a0 + n - xStart;
        out.yEnd = b0 + m - yStart;
        out.d = 2 * d;
        return;
      }
    }
  }
}

/** Emit the region a[a0, a0+n) vs b[b0, b0+m) in order, bisecting on its middle
 * snake. Recursion depth stays logarithmic in the edit distance because each
 * half carries at most half of it. */
function bisect(
  a: string[],
  b: string[],
  a0: number,
  n: number,
  b0: number,
  m: number,
  vf: Int32Array,
  vr: Int32Array,
  offset: number,
  snake: MiddleSnake,
  rows: DiffRow[]
): void {
  if (n === 0) {
    for (let j = 0; j < m; j++) {
      rows.push({ kind: "added", text: b[b0 + j], beforeLine: null, afterLine: b0 + j + 1 });
    }
    return;
  }
  if (m === 0) {
    for (let i = 0; i < n; i++) {
      rows.push({ kind: "removed", text: a[a0 + i], beforeLine: a0 + i + 1, afterLine: null });
    }
    return;
  }

  findMiddleSnake(a, b, a0, n, b0, m, vf, vr, offset, snake);
  const { xStart, yStart, xEnd, yEnd, d } = snake;

  if (d > 1) {
    bisect(a, b, a0, xStart - a0, b0, yStart - b0, vf, vr, offset, snake, rows);
    for (let t = xStart; t < xEnd; t++) {
      rows.push({
        kind: "context",
        text: a[t],
        beforeLine: t + 1,
        afterLine: yStart + (t - xStart) + 1,
      });
    }
    bisect(a, b, xEnd, a0 + n - xEnd, yEnd, b0 + m - yEnd, vf, vr, offset, snake, rows);
    return;
  }

  // d <= 1: the region is a common prefix, at most one lone insert or delete,
  // then a common suffix — cheaper to lay out directly than to recurse into,
  // and recursing on d == 1 would not always shrink the problem.
  let p = 0;
  while (p < n && p < m && a[a0 + p] === b[b0 + p]) p++;
  for (let t = 0; t < p; t++) {
    rows.push({ kind: "context", text: a[a0 + t], beforeLine: a0 + t + 1, afterLine: b0 + t + 1 });
  }
  if (n > m) {
    rows.push({ kind: "removed", text: a[a0 + p], beforeLine: a0 + p + 1, afterLine: null });
  } else if (m > n) {
    rows.push({ kind: "added", text: b[b0 + p], beforeLine: null, afterLine: b0 + p + 1 });
  }
  const shift = n > m ? 1 : 0;
  for (let t = p; t < Math.min(n, m); t++) {
    rows.push({
      kind: "context",
      text: a[a0 + t + shift],
      beforeLine: a0 + t + shift + 1,
      afterLine: b0 + t + (1 - shift) + 1,
    });
  }
}

/** Bisecting can hand back a changed run interleaved ("+new -old +new"), which
 * is a correct shortest script but not the one this UI reads by. Within one
 * maximal run of non-context rows the removals cover a contiguous stretch of
 * `before` and the additions a contiguous stretch of `after`, so hoisting the
 * removals in front reorders nothing that is anchored: both sides still read
 * back in line order, the script keeps its length, and a replaced line reads
 * "-old" then "+new" the way every other path here already promises. */
function groupRemovalsFirst(rows: DiffRow[]): DiffRow[] {
  const out: DiffRow[] = [];
  let index = 0;
  while (index < rows.length) {
    if (rows[index].kind === "context") {
      out.push(rows[index]);
      index++;
      continue;
    }
    let end = index;
    while (end < rows.length && rows[end].kind !== "context") end++;
    for (const row of rows.slice(index, end)) if (row.kind === "removed") out.push(row);
    for (const row of rows.slice(index, end)) if (row.kind === "added") out.push(row);
    index = end;
  }
  return out;
}

function buildRows(a: string[], b: string[]): DiffRow[] {
  const rows: DiffRow[] = [];
  // Two Int32 frontiers of 2*(N+M)+3 cells each is the entire working set — the
  // one number that used to be (N+1)*(M+1).
  const offset = a.length + b.length + 1;
  const vf = new Int32Array(2 * offset + 1);
  const vr = new Int32Array(2 * offset + 1);
  const snake: MiddleSnake = { xStart: 0, yStart: 0, xEnd: 0, yEnd: 0, d: 0 };
  bisect(a, b, 0, a.length, 0, b.length, vf, vr, offset, snake, rows);
  return groupRemovalsFirst(rows);
}

function makeHunk(rows: DiffRow[], skippedBefore: number): DiffHunk {
  let beforeStart = 0;
  let beforeCount = 0;
  let afterStart = 0;
  let afterCount = 0;
  for (const row of rows) {
    if (row.beforeLine !== null) {
      if (beforeStart === 0) beforeStart = row.beforeLine;
      beforeCount++;
    }
    if (row.afterLine !== null) {
      if (afterStart === 0) afterStart = row.afterLine;
      afterCount++;
    }
  }
  return { rows, skippedBefore, beforeStart, beforeCount, afterStart, afterCount };
}

function collapse(rows: DiffRow[], contextRadius: number): DiffHunk[] {
  const changed = rows.map((row) => row.kind !== "context");
  if (!changed.includes(true)) return [];

  const hunks: DiffHunk[] = [];
  let cursor = 0; // first row not yet emitted or skipped
  let index = 0;
  while (index < rows.length) {
    if (!changed[index]) {
      index++;
      continue;
    }
    const start = Math.max(cursor, index - contextRadius);
    let end = index; // inclusive
    // Absorb the next change too when its leading context would touch this one.
    for (let k = index + 1; k < rows.length; k++) {
      if (changed[k]) {
        end = k;
      } else if (k - end > contextRadius * 2) {
        break;
      }
    }
    end = Math.min(rows.length - 1, end + contextRadius);
    hunks.push(makeHunk(rows.slice(start, end + 1), start - cursor));
    cursor = end + 1;
    index = end + 1;
  }
  return hunks;
}

/** Diff `before` against `after`, line by line. */
export function diffLines(
  before: string,
  after: string,
  options: LineDiffOptions = {}
): LineDiffResult {
  const {
    collapseUnchanged = true,
    contextRadius = DEFAULT_CONTEXT_RADIUS,
    maxLines = DEFAULT_MAX_LINES,
  } = options;

  const a = splitLines(before);
  const b = splitLines(after);

  // The identical head and tail never need the search at all: a leading line
  // equal to its counterpart is taken by the `a[i] === b[j]` branch of
  // buildRows anyway, and a trailing common run does the same from the other
  // end.
  //
  // WHAT TRIMMING PRESERVES, AND WHAT IT DOES NOT. The script stays exactly as
  // short, and both sides still reconstruct — those are the contract, and
  // lineDiff.test.ts asserts them. It does NOT preserve which row carries the
  // marker: when several lines hold the SAME text, trimming can move a deletion
  // onto a different one of them. An earlier version of this comment claimed
  // "changes not one emitted row … the differential test proves it"; T-59's
  // independent review ran that differential (3,000 markdown-shaped pairs,
  // trimming disabled in a copy) and got 672 divergent — 22% — and no such test
  // existed to contradict it. Both scripts are minimal and both reconstruct, so
  // the divergence is a tie-break among equal answers, not a wrong one. Do not
  // reintroduce a row-for-row claim, and do not treat row-for-row identity as
  // something a refactor here is allowed to rely on.
  //
  // Measured over 60 real commits here: the table shrinks ~100x at the median
  // (5 orders of magnitude on a one-line edit, 3x on a change spread over a
  // whole file), which is the difference between `maxLines` being a wall people
  // hit and one they do not.
  let head = 0;
  while (head < a.length && head < b.length && a[head] === b[head]) head++;
  let tail = 0;
  while (
    tail < a.length - head &&
    tail < b.length - head &&
    a[a.length - 1 - tail] === b[b.length - 1 - tail]
  ) {
    tail++;
  }
  const midA = a.slice(head, a.length - tail);
  const midB = b.slice(head, b.length - tail);

  // The ceiling exists to bound the table, so it is the MIDDLE that has to fit.
  // The reported counts stay whole-file: the refusal screen tells a reader how
  // big their file is, which is what they can act on.
  if (midA.length > maxLines || midB.length > maxLines) {
    return {
      status: "too-large",
      rows: [],
      hunks: [],
      beforeLineCount: a.length,
      afterLineCount: b.length,
    };
  }

  const rows: DiffRow[] = [];
  for (let k = 0; k < head; k++) {
    rows.push({ kind: "context", text: a[k], beforeLine: k + 1, afterLine: k + 1 });
  }
  for (const row of buildRows(midA, midB)) {
    rows.push({
      kind: row.kind,
      text: row.text,
      beforeLine: row.beforeLine === null ? null : row.beforeLine + head,
      afterLine: row.afterLine === null ? null : row.afterLine + head,
    });
  }
  for (let k = 0; k < tail; k++) {
    rows.push({
      kind: "context",
      text: a[a.length - tail + k],
      beforeLine: a.length - tail + k + 1,
      afterLine: b.length - tail + k + 1,
    });
  }
  const hunks = collapseUnchanged
    ? collapse(rows, Math.max(0, contextRadius))
    : rows.length > 0
      ? [makeHunk(rows, 0)]
      : [];

  return {
    status: "diffed",
    rows,
    hunks,
    beforeLineCount: a.length,
    afterLineCount: b.length,
  };
}
