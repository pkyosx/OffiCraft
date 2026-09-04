import { describe, it, expect } from "vitest";
import { diffLines, splitLines, type DiffRow } from "./lineDiff";

describe("splitLines", () => {
  it("treats a trailing newline as a terminator, not a blank final line", () => {
    expect(splitLines("a\nb\n")).toEqual(["a", "b"]);
  });

  it("keeps a genuinely blank last line", () => {
    expect(splitLines("a\n\n")).toEqual(["a", ""]);
  });

  it("returns no lines for an empty document", () => {
    expect(splitLines("")).toEqual([]);
  });

  it("splits CRLF and LF alike, leaving no carriage returns behind", () => {
    expect(splitLines("a\r\nb\nc")).toEqual(["a", "b", "c"]);
  });
});

describe("diffLines", () => {
  it("reports every line as context and no hunks when the sides are identical", () => {
    const result = diffLines("a\nb\nc", "a\nb\nc");
    expect(result.status).toBe("diffed");
    expect(result.rows).toEqual([
      { kind: "context", text: "a", beforeLine: 1, afterLine: 1 },
      { kind: "context", text: "b", beforeLine: 2, afterLine: 2 },
      { kind: "context", text: "c", beforeLine: 3, afterLine: 3 },
    ]);
    expect(result.hunks).toEqual([]);
  });

  it("emits only the changed middle line as removed-then-added", () => {
    expect(diffLines("a\nb\nc", "a\nB\nc").rows).toEqual([
      { kind: "context", text: "a", beforeLine: 1, afterLine: 1 },
      { kind: "removed", text: "b", beforeLine: 2, afterLine: null },
      { kind: "added", text: "B", beforeLine: null, afterLine: 2 },
      { kind: "context", text: "c", beforeLine: 3, afterLine: 3 },
    ]);
  });

  it("keeps surrounding lines as context for a pure insertion", () => {
    expect(diffLines("a\nc", "a\nb\nc").rows).toEqual([
      { kind: "context", text: "a", beforeLine: 1, afterLine: 1 },
      { kind: "added", text: "b", beforeLine: null, afterLine: 2 },
      { kind: "context", text: "c", beforeLine: 2, afterLine: 3 },
    ]);
  });

  it("keeps surrounding lines as context for a pure deletion", () => {
    expect(diffLines("a\nb\nc", "a\nc").rows).toEqual([
      { kind: "context", text: "a", beforeLine: 1, afterLine: 1 },
      { kind: "removed", text: "b", beforeLine: 2, afterLine: null },
      { kind: "context", text: "c", beforeLine: 3, afterLine: 2 },
    ]);
  });

  it("reports an empty before side as pure addition", () => {
    const result = diffLines("", "x\ny");
    expect(result.beforeLineCount).toBe(0);
    expect(result.afterLineCount).toBe(2);
    expect(result.rows).toEqual([
      { kind: "added", text: "x", beforeLine: null, afterLine: 1 },
      { kind: "added", text: "y", beforeLine: null, afterLine: 2 },
    ]);
  });

  it("reports an emptied after side as pure removal", () => {
    expect(diffLines("x\ny", "").rows).toEqual([
      { kind: "removed", text: "x", beforeLine: 1, afterLine: null },
      { kind: "removed", text: "y", beforeLine: 2, afterLine: null },
    ]);
  });

  it("sees CRLF and LF text with the same content as unchanged", () => {
    const result = diffLines("a\r\nb\r\nc", "a\nb\nc");
    expect(result.rows.every((row) => row.kind === "context")).toBe(true);
    expect(result.hunks).toEqual([]);
  });

  it("sees a newly added trailing newline as unchanged, but a new blank line as added", () => {
    expect(diffLines("a\nb", "a\nb\n").hunks).toEqual([]);
    expect(diffLines("a\nb", "a\nb\n\n").rows).toEqual([
      { kind: "context", text: "a", beforeLine: 1, afterLine: 1 },
      { kind: "context", text: "b", beforeLine: 2, afterLine: 2 },
      { kind: "added", text: "", beforeLine: null, afterLine: 3 },
    ]);
  });

  it("collapses unchanged runs into one hunk bounded by the context radius", () => {
    const before = Array.from({ length: 20 }, (_, i) => `line${i + 1}`).join("\n");
    const after = before.replace("line10", "LINE10");
    const result = diffLines(before, after, { contextRadius: 2 });

    expect(result.hunks).toHaveLength(1);
    const hunk = result.hunks[0];
    expect(hunk.skippedBefore).toBe(7);
    expect(hunk.beforeStart).toBe(8);
    expect(hunk.beforeCount).toBe(5);
    expect(hunk.afterStart).toBe(8);
    expect(hunk.afterCount).toBe(5);
    expect(hunk.rows).toEqual([
      { kind: "context", text: "line8", beforeLine: 8, afterLine: 8 },
      { kind: "context", text: "line9", beforeLine: 9, afterLine: 9 },
      { kind: "removed", text: "line10", beforeLine: 10, afterLine: null },
      { kind: "added", text: "LINE10", beforeLine: null, afterLine: 10 },
      { kind: "context", text: "line11", beforeLine: 11, afterLine: 11 },
      { kind: "context", text: "line12", beforeLine: 12, afterLine: 12 },
    ]);
  });

  // The BOUNDARY of "distant" — the one line where merging flips to splitting.
  // A mutant sweep found this unguarded: widening the merge distance by one
  // left every other case in this file green, because they all sit far away
  // from the threshold. Two changes with exactly 2×radius unchanged lines
  // between them share a hunk (their context regions touch); one line more and
  // they cannot, so the reader gets two hunks and an explicit skipped count
  // instead of an unbroken block that hides how far apart they really were.
  it("merges two changes exactly 2×radius apart and splits them one line further", () => {
    const lines = (n: number) =>
      Array.from({ length: n }, (_, i) => `line${i + 1}`).join("\n");
    const radius = 2;

    // Changed at line 3 and line 3 + (2*radius) + 1 = line 8 ⇒ exactly 2*radius
    // (4) unchanged lines lie between them.
    const merged = diffLines(lines(20), lines(20).replace("line3", "L3").replace("line8", "L8"), {
      contextRadius: radius,
    });
    expect(merged.hunks).toHaveLength(1);
    expect(
      merged.hunks[0].rows.filter((row) => row.kind !== "context")
    ).toHaveLength(4); // both changes, each removed+added

    // One line further apart (line 3 and line 9 ⇒ 5 unchanged between) and the
    // same radius can no longer bridge them.
    const split = diffLines(lines(20), lines(20).replace("line3", "L3").replace("line9", "L9"), {
      contextRadius: radius,
    });
    expect(split.hunks).toHaveLength(2);
    expect(split.hunks[1].skippedBefore).toBeGreaterThan(0);
  });

  it("splits distant changes into separate hunks and counts the lines skipped between them", () => {
    const before = Array.from({ length: 30 }, (_, i) => `line${i + 1}`).join("\n");
    const after = before.replace("line3", "LINE3").replace("line25", "LINE25");
    const result = diffLines(before, after, { contextRadius: 1 });

    expect(result.hunks).toHaveLength(2);
    expect(result.hunks[0].skippedBefore).toBe(1);
    expect(result.hunks[0].beforeStart).toBe(2);
    expect(result.hunks[1].skippedBefore).toBe(19);
    expect(result.hunks[1].beforeStart).toBe(24);
    expect(result.hunks[1].rows.map((row) => row.kind)).toEqual([
      "context",
      "removed",
      "added",
      "context",
    ]);
  });

  it("returns one hunk holding every row when collapsing is turned off", () => {
    const before = Array.from({ length: 20 }, (_, i) => `line${i + 1}`).join("\n");
    const after = before.replace("line10", "LINE10");
    const result = diffLines(before, after, { collapseUnchanged: false });

    expect(result.hunks).toHaveLength(1);
    expect(result.hunks[0].rows).toEqual(result.rows);
    expect(result.hunks[0].skippedBefore).toBe(0);
  });

  // The threshold measures the part that still has to go through the table —
  // the shared head and tail are trimmed before it is consulted, so a pair that
  // is long but barely different is now diffed where it used to be refused.
  // Both halves of that sentence need a test; neither alone is the contract.
  it("refuses to diff when the part that actually differs is past the max-lines threshold", () => {
    const before = Array.from({ length: 5 }, (_, i) => `before${i + 1}`).join("\n");
    const after = Array.from({ length: 5 }, (_, i) => `after${i + 1}`).join("\n");
    const result = diffLines(before, after, { maxLines: 4 });

    expect(result.status).toBe("too-large");
    expect(result.rows).toEqual([]);
    expect(result.hunks).toEqual([]);
    expect(result.beforeLineCount).toBe(5);
    expect(result.afterLineCount).toBe(5);
  });

  it("diffs a pair far past the threshold when only its middle differs", () => {
    const head = Array.from({ length: 50 }, (_, i) => `head${i}`);
    const tail = Array.from({ length: 50 }, (_, i) => `tail${i}`);
    const result = diffLines(
      [...head, "old", ...tail].join("\n"),
      [...head, "new", ...tail].join("\n"),
      { maxLines: 4, collapseUnchanged: false }
    );

    expect(result.status).toBe("diffed");
    expect(result.rows).toHaveLength(102);
    expect(result.rows[0]).toEqual({
      kind: "context",
      text: "head0",
      beforeLine: 1,
      afterLine: 1,
    });
    expect(result.rows[50]).toEqual({
      kind: "removed",
      text: "old",
      beforeLine: 51,
      afterLine: null,
    });
    expect(result.rows[51]).toEqual({
      kind: "added",
      text: "new",
      beforeLine: null,
      afterLine: 51,
    });
    expect(result.rows[101]).toEqual({
      kind: "context",
      text: "tail49",
      beforeLine: 101,
      afterLine: 101,
    });
  });

  it("shifts but does not reshape the rows when identical head and tail lines are added to both sides", () => {
    const bare = diffLines("x\ny\nz", "x\nY\nz", { collapseUnchanged: false }).rows;
    const padded = diffLines("p1\np2\nx\ny\nz\ns1\ns2", "p1\np2\nx\nY\nz\ns1\ns2", {
      collapseUnchanged: false,
    }).rows;

    expect(padded).toHaveLength(bare.length + 4);
    expect(padded.slice(2, 2 + bare.length)).toEqual(
      bare.map((row) => ({
        ...row,
        beforeLine: row.beforeLine === null ? null : row.beforeLine + 2,
        afterLine: row.afterLine === null ? null : row.afterLine + 2,
      }))
    );
  });

  // The DEFAULT ceiling, pinned by behaviour rather than by reading the
  // constant back — an assertion phrased against `DEFAULT_MAX_LINES` would hold
  // for whatever value it happens to have and would pin nothing. 3,000 lines a
  // side with nothing in common is the case that USED to be refused out of hand
  // and is the point of the change; put the ceiling back where it was and this
  // is the test that says so.
  it("diffs a 3,000-line pair with nothing in common at the default ceiling", () => {
    const side = (tag: string) =>
      Array.from({ length: 3000 }, (_, i) => `${tag} ${i}`).join("\n");
    const result = diffLines(side("old"), side("new"));

    expect(result.status).toBe("diffed");
    expect(result.beforeLineCount).toBe(3000);
    expect(result.rows.filter((row) => row.kind === "removed")).toHaveLength(3000);
    expect(result.rows.filter((row) => row.kind === "added")).toHaveLength(3000);
  });

  // owner ④ — the character-level tint — is not computed here: wordDiff's
  // pairChangedRows does it, and it pairs a run of `removed` with the run of
  // `added` that FOLLOWS it. Myers' divide-and-conquer emits a changed block in
  // whatever order the recursion unwinds, so a block coming back as `+ - +`
  // pairs nothing and the tint disappears — on screen only, with every other
  // assertion in this file still green, because the rows themselves are all
  // correct and minimal. buildRows normalises the order; this is the assertion
  // that keeps it normalised, and it is the ONLY one that would notice.
  it("never lets an added row precede a removed one inside the same changed block", () => {
    const cases: Array<[string, string]> = [
      // Nothing in common: the shape that interleaved worst before normalising.
      [
        Array.from({ length: 54 }, (_, i) => `old ${i}`).join("\n"),
        Array.from({ length: 4 }, (_, i) => `new ${i}`).join("\n"),
      ],
      ["a\nb\nc\nd\ne", "1\n2\n3"],
      ["x", "b\na\na"],
      // Two symbols only — the repetitive shape where the two algorithms pick
      // different anchors, so the block boundaries are least predictable.
      [
        Array.from({ length: 40 }, (_, i) => (i % 2 ? "L0" : "L1")).join("\n"),
        Array.from({ length: 44 }, (_, i) => (i % 3 ? "L1" : "N0")).join("\n"),
      ],
    ];

    for (const [before, after] of cases) {
      const rows = diffLines(before, after, { collapseUnchanged: false }).rows;
      const interleavedAt = rows.findIndex(
        (row, i) => row.kind === "added" && rows[i + 1]?.kind === "removed"
      );
      expect({ before, interleavedAt }).toEqual({ before, interleavedAt: -1 });
    }
  });

  it("emits rows both sides can be read back out of, when head and tail are both trimmed", () => {
    const before = "same1\nsame2\ndrop\nkeep\nsame3\nsame4";
    const after = "same1\nsame2\nkeep\nadd\nsame3\nsame4";
    const rows = diffLines(before, after, { collapseUnchanged: false }).rows;

    expect(
      rows
        .filter((row) => row.kind !== "added")
        .map((row) => row.text)
        .join("\n")
    ).toBe(before);
    expect(
      rows
        .filter((row) => row.kind !== "removed")
        .map((row) => row.text)
        .join("\n")
    ).toBe(after);
    expect(rows.filter((row) => row.beforeLine !== null).map((row) => row.beforeLine)).toEqual([
      1, 2, 3, 4, 5, 6,
    ]);
    expect(rows.filter((row) => row.afterLine !== null).map((row) => row.afterLine)).toEqual([
      1, 2, 3, 4, 5, 6,
    ]);
  });

  it("still diffs when both sides sit exactly on the threshold", () => {
    const result = diffLines("a\nb", "a\nB", { maxLines: 2 });
    expect(result.status).toBe("diffed");
    expect(result.rows.map((row) => row.kind)).toEqual([
      "context",
      "removed",
      "added",
    ]);
  });
});

// The previous implementation, kept verbatim as a reference oracle. It walks a
// full O(n*m) suffix-indexed LCS table, so it is only usable on small inputs —
// which is exactly why it was replaced.
function lcsLengths(a: string[], b: string[]): Uint32Array {
  const width = b.length + 1;
  const table = new Uint32Array((a.length + 1) * width);
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      table[i * width + j] =
        a[i] === b[j]
          ? table[(i + 1) * width + (j + 1)] + 1
          : Math.max(table[(i + 1) * width + j], table[i * width + (j + 1)]);
    }
  }
  return table;
}

function legacyBuildRows(a: string[], b: string[]): DiffRow[] {
  const table = lcsLengths(a, b);
  const width = b.length + 1;
  const rows: DiffRow[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) {
      rows.push({ kind: "context", text: a[i], beforeLine: i + 1, afterLine: j + 1 });
      i++;
      j++;
    } else if (
      i < a.length &&
      (j === b.length || table[(i + 1) * width + j] >= table[i * width + (j + 1)])
    ) {
      rows.push({ kind: "removed", text: a[i], beforeLine: i + 1, afterLine: null });
      i++;
    } else {
      rows.push({ kind: "added", text: b[j], beforeLine: null, afterLine: j + 1 });
      j++;
    }
  }
  return rows;
}

function mulberry32(seed: number): () => number {
  let t = seed >>> 0;
  return () => {
    t = (t + 0x6d2b79f5) >>> 0;
    let x = Math.imul(t ^ (t >>> 15), 1 | t);
    x = (x + Math.imul(x ^ (x >>> 7), 61 | x)) ^ x;
    return ((x ^ (x >>> 14)) >>> 0) / 4294967296;
  };
}

type Pair = { before: string; after: string; shape: string };

/** 500+ pairs spanning the shapes that stress an edit-script search differently:
 * empty sides, identity, no common subsequence at all, heavy repetition (where
 * many distinct scripts tie for shortest), single-line edits, a long common
 * frame around a tiny middle, and changes sprayed across the whole file. */
function randomPairs(): Pair[] {
  const rand = mulberry32(0x5eed1234);
  const pick = (n: number) => Math.floor(rand() * n);
  const pairs: Pair[] = [
    { before: "", after: "", shape: "both-empty" },
    { before: "", after: "a\nb\nc", shape: "before-empty" },
    { before: "a\nb\nc", after: "", shape: "after-empty" },
  ];

  const shapes = [
    "identical",
    "disjoint",
    "repetitive",
    "one-line",
    "long-frame",
    "scattered",
    "one-side-empty",
  ];
  for (let round = 0; round < 72; round++) {
    for (const shape of shapes) {
      const size = 1 + pick(60);
      const alphabet = shape === "repetitive" ? 2 : 12;
      const base: string[] = [];
      for (let i = 0; i < size; i++) base.push(`L${pick(alphabet)}`);
      let after = base.slice();

      if (shape === "identical") {
        // leave it
      } else if (shape === "disjoint") {
        after = [];
        for (let i = 0; i < 1 + pick(60); i++) after.push(`Z${pick(alphabet)}`);
      } else if (shape === "one-line") {
        const at = pick(base.length);
        after[at] = `M${pick(alphabet)}`;
      } else if (shape === "long-frame") {
        const head: string[] = [];
        for (let i = 0; i < 40; i++) head.push(`H${i}`);
        const tail: string[] = [];
        for (let i = 0; i < 40; i++) tail.push(`T${i}`);
        base.length = 0;
        base.push(...head, `mid${pick(3)}`, ...tail);
        after = [...head, `mid${pick(3)}`, `mid${pick(3)}`, ...tail];
      } else if (shape === "one-side-empty") {
        after = [];
      } else {
        // "repetitive" and "scattered" both get random splices throughout.
        const edits = 1 + pick(shape === "scattered" ? 12 : 5);
        for (let e = 0; e < edits; e++) {
          if (after.length === 0 || rand() < 0.4) {
            after.splice(pick(after.length + 1), 0, `N${pick(alphabet)}`);
          } else if (rand() < 0.5) {
            after.splice(pick(after.length), 1);
          } else {
            after[pick(after.length)] = `N${pick(alphabet)}`;
          }
        }
      }
      pairs.push({ before: base.join("\n"), after: after.join("\n"), shape });
    }
  }
  return pairs;
}

describe("buildRows via diffLines, against the previous LCS implementation", () => {
  const pairs = randomPairs();

  it("covers at least 500 random pairs across every shape", () => {
    expect(pairs.length).toBeGreaterThanOrEqual(500);
  });

  it("emits rows both original sides can be reconstructed from", () => {
    for (const { before, after, shape } of pairs) {
      const rows = diffLines(before, after, { collapseUnchanged: false }).rows;
      const rebuilt = (skip: string) =>
        rows
          .filter((row) => row.kind !== skip)
          .map((row) => row.text)
          .join("\n");
      expect(rebuilt("added"), shape).toBe(before);
      expect(rebuilt("removed"), shape).toBe(after);
    }
  });

  it("numbers both sides consecutively from 1", () => {
    for (const { before, after, shape } of pairs) {
      const rows = diffLines(before, after, { collapseUnchanged: false }).rows;
      const seq = (side: "beforeLine" | "afterLine") =>
        rows.map((row) => row[side]).filter((n): n is number => n !== null);
      expect(seq("beforeLine"), shape).toEqual(
        splitLines(before).map((_, index) => index + 1)
      );
      expect(seq("afterLine"), shape).toEqual(splitLines(after).map((_, index) => index + 1));
    }
  });

  it("produces an edit script exactly as short as the LCS one", () => {
    for (const { before, after, shape } of pairs) {
      const rows = diffLines(before, after, { collapseUnchanged: false }).rows;
      const oracle = legacyBuildRows(splitLines(before), splitLines(after));
      const changed = (rs: DiffRow[]) => rs.filter((row) => row.kind !== "context").length;
      expect(changed(rows), `${shape}: ${JSON.stringify({ before, after })}`).toBe(
        changed(oracle)
      );
    }
  });

  // Observation only: two different shortest scripts are both correct, so this
  // reports the agreement rate rather than asserting on it.
  it("reports how often the row-by-row script matches the LCS one", () => {
    let identical = 0;
    let firstDivergence: string | null = null;
    for (const { before, after } of pairs) {
      const rows = diffLines(before, after, { collapseUnchanged: false }).rows;
      const oracle = legacyBuildRows(splitLines(before), splitLines(after));
      if (JSON.stringify(rows) === JSON.stringify(oracle)) {
        identical++;
      } else if (firstDivergence === null) {
        firstDivergence = JSON.stringify({ before, after });
      }
    }
    const pct = ((identical / pairs.length) * 100).toFixed(1);
    console.log(
      `row-for-row identical with the LCS oracle: ${identical}/${pairs.length} (${pct}%)` +
        (firstDivergence === null ? "" : `; first divergence: ${firstDivergence}`)
    );
    expect(identical).toBeGreaterThan(0);
  });
});
