// components/DiffView.tsx — the shared diff surface, in the shape of a GitHub
// pull request's "Files changed" (T-40f0, owner 2026-07-31 rc-b69722f81136 ①).
//
// Purely presentational: it owns nothing but "show me how these two texts
// differ". It takes the two TEXTS rather than a precomputed LineDiffResult
// because the pairing is one-to-one — every caller that has a `before`/`after`
// pair would otherwise have to import lib/lineDiff, remember which options the
// cockpit standardises on, and memoise the call itself.
//
// The six things the owner ruled on, and where each lives:
//   ① UNIFIED IS THE DEFAULT (old above, new below, each row full width); the
//      two-column view stays as a switchable second option. His reason for the
//      default: long Chinese sentences get squeezed into a gutter in two
//      columns. `mode` state + `.diff-view__modes`.
//   ② WHOLE-ROW COLOUR is the recognition channel — a removed row is red edge
//      to edge, an added row green — not a glyph at the start of the line.
//      diff-view.css tints the row AND its number/marker cells.
//   ③ TWO NUMBER COLUMNS: the old version's line number and the current one's.
//      Unchanged lines carry both; a changed line leaves its missing side blank.
//   ④ CHARACTER-LEVEL TINT when one row only had a few characters edited
//      (lib/wordDiff.ts). Rows are paired positionally inside one change block.
//   ⑤ THE -/+ GUTTER STAYS, as a secondary cue (the owner did not take it away)
//      — and it carries an accessible name, so the fact the colour conveys is
//      also available to a reader who gets no colour at all.
//   ⑥ NO COLLAPSE, NO EXPAND — 「showing entire content」. Every unchanged line
//      is rendered, so `collapseUnchanged` is NOT a caller option here and
//      `DiffViewOptions` exposes only the refusal ceiling. lib/lineDiff keeps
//      its collapse machinery (it is that module's own, tested, API).
//      🔴 What ENFORCES ⑥ is that this surface renders `result.rows` — always
//      the full edit script — and never `result.hunks`. The
//      `collapseUnchanged: false` argument below is BELT, NOT BRACES: collapsing
//      only ever shaped `hunks`, which nothing here reads, so flipping that
//      argument to `true` changes not one rendered row. Measured, not reasoned:
//      flipping it leaves all 15 tests in DiffView.test.tsx green; rendering
//      `hunks.flatMap(h => h.rows)` instead reddens the no-collapse test at
//      once. Do not read the argument as the guarantee — if you are looking for
//      the line that must not move, it is `result.rows` in the render.
//
// A "too-large" diff and an all-context diff are DIFFERENT screens: the first
// says the comparison was refused, the second says the two versions match.
// Collapsing them into one blank panel would hide a refusal behind good news.

import { useMemo, useState } from "react";
import { useI18n } from "../i18n";
import { diffLines, type DiffRow, type DiffRowKind } from "../lib/lineDiff";
import { diffWords, pairChangedRows, type WordSegment } from "../lib/wordDiff";
import "./diff-view.css";

// An empty cell collapses the row's height, so the context marker and a
// blank line both render a non-breaking space rather than nothing.
const NBSP = "\u00a0";

const MARKER: Record<DiffRowKind, string> = {
  context: NBSP,
  added: "+",
  removed: "-",
};

/** 單欄 (unified, the default) vs 兩欄對照 (split). */
export type DiffMode = "unified" | "split";

const MODES: readonly DiffMode[] = ["unified", "split"];

/** What a host may still tune. Deliberately NOT `LineDiffOptions`: collapsing
 * is off by contract here (owner ⑥), so offering the knob would advertise a
 * behaviour this surface refuses to have. */
export interface DiffViewOptions {
  /** Per-side line ceiling above which the comparison is refused outright. */
  maxLines?: number;
}

/** One row of the two-column view. A replacement puts its `-` and its `+` on
 * the SAME visual row; a pure deletion or insertion fills one side only. */
interface SplitRow {
  left: number | null;
  right: number | null;
}

function buildSplitRows(
  rows: DiffRow[],
  pairedTo: Map<number, number>
): SplitRow[] {
  const partnerOf = new Map<number, number>();
  for (const [added, removed] of pairedTo) partnerOf.set(removed, added);

  const out: SplitRow[] = [];
  rows.forEach((row, index) => {
    if (row.kind === "context") {
      out.push({ left: index, right: index });
    } else if (row.kind === "removed") {
      out.push({ left: index, right: partnerOf.get(index) ?? null });
    } else if (!pairedTo.has(index)) {
      // An insertion with no `-` to sit beside — its left half stays empty.
      out.push({ left: null, right: index });
    }
    // A paired `+` was already emitted alongside its `-`.
  });
  return out;
}

/** The line's text, with the characters that moved wrapped for tinting. An
 * empty line still renders one NBSP so the row keeps its height. */
function LineText({
  text,
  segments,
  kind,
}: {
  text: string;
  segments?: WordSegment[];
  kind: DiffRowKind;
}) {
  if (!segments) return <>{text || NBSP}</>;
  return (
    <>
      {segments.map((seg, i) =>
        seg.changed ? (
          <span key={i} className={`diff-view__word diff-view__word--${kind}`}>
            {seg.text}
          </span>
        ) : (
          <span key={i}>{seg.text}</span>
        )
      )}
    </>
  );
}

export function DiffView({
  before,
  after,
  beforeLabel,
  afterLabel,
  options,
  onOpenSide,
  mode: modeProp,
  onModeChange,
  testId = "diff-view",
}: {
  /** The historical version — the `-` side. */
  before: string;
  /** The current stored content — the `+` side. */
  after: string;
  beforeLabel?: string;
  afterLabel?: string;
  options?: DiffViewOptions;
  /** Open ONE side on its own (owner 2026-09-03, c-944088dceab0: 「兩份應該都
   * 要是連結」). Optional because the two hosts differ: the attachment overlay
   * holds both sides' text and can show either alone, while the document
   * history screen is already looking at the document the sides belong to —
   * there is nowhere for it to go. Omitted, the headings stay plain text, which
   * is what every existing call site gets. */
  onOpenSide?: (side: "before" | "after") => void;
  /** 單欄 / 兩欄對照, HOISTED. Uncontrolled by default — every existing call site
   * keeps its own state and its own 單欄 default (owner ①).
   *
   * The overlay controls it for ONE reason, found by measuring: opening a side
   * on its own unmounts this component, so an uncontrolled mode came back as
   * 單欄 and the reader who was in 兩欄對照 got silently moved. Nothing warned
   * them, and "the button I pressed stopped being pressed" is the same class of
   * quiet wrongness as the off-screen column this round started with. */
  mode?: DiffMode;
  onModeChange?: (mode: DiffMode) => void;
  testId?: string;
}) {
  const { t, msg } = useI18n();
  const [uncontrolledMode, setUncontrolledMode] = useState<DiffMode>("unified");
  const mode = modeProp ?? uncontrolledMode;
  /* Controlled means CONTROLLED: when the host owns `mode`, this writes nothing
   * of its own. Keeping a shadow copy would leave two answers to "which layout
   * am I in" that agree only while the host happens to echo every change back —
   * exactly the half-truth this component is being fixed for. */
  const setMode = (next: DiffMode) => {
    if (modeProp === undefined) setUncontrolledMode(next);
    onModeChange?.(next);
  };
  const { maxLines } = options ?? {};
  const result = useMemo(
    // owner ⑥. Says what this surface wants and skips building hunks nobody
    // reads — but it is NOT what holds the contract; rendering `rows` below is
    // (see ⑥ in the header: flipping this flag reddens nothing).
    () => diffLines(before, after, { collapseUnchanged: false, maxLines }),
    [before, after, maxLines]
  );

  // Which characters moved, per row index. Only rows that were PAIRED (a `-`
  // and the `+` that replaced it) can have this: a line that was purely added
  // or purely deleted has nothing to compare against inside itself.
  const wordSegments = useMemo(() => {
    const pairedTo = pairChangedRows(result.rows);
    const segments = new Map<number, WordSegment[]>();
    for (const [added, removed] of pairedTo) {
      const pair = diffWords(result.rows[removed].text, result.rows[added].text);
      if (!pair.highlighted) continue;
      segments.set(removed, pair.before);
      segments.set(added, pair.after);
    }
    return { pairedTo, segments };
  }, [result]);

  const kindLabel: Record<DiffRowKind, string> = {
    context: t.diff.contextLine,
    added: t.diff.addedLine,
    removed: t.diff.removedLine,
  };

  if (result.status === "too-large") {
    return (
      <div className="diff-view" data-testid={testId}>
        <p className="diff-view__notice" data-testid="diff-view-too-large">
          {msg.diffTooLarge(
            Math.max(result.beforeLineCount, result.afterLineCount)
          )}
        </p>
      </div>
    );
  }

  // Asked of the ROWS: with collapsing off an identical pair still produces a
  // full run of context rows, so "no hunks" is not the question.
  if (result.rows.every((row) => row.kind === "context")) {
    return (
      <div className="diff-view" data-testid={testId}>
        <p className="diff-view__notice" data-testid="diff-view-empty">
          {t.diff.noChanges}
        </p>
      </div>
    );
  }

  const addedCount = result.rows.filter((r) => r.kind === "added").length;
  const removedCount = result.rows.filter((r) => r.kind === "removed").length;

  /* The heading over one side. A BUTTON when the host gave us somewhere to go,
   * a plain span otherwise — never a button that does nothing, which is the
   * failure this whole round is about (a mode switch that looked live and
   * wasn't). Same text either way: the affordance changes, the wording does
   * not. */
  const sideHeading = (side: "before" | "after", label: string) => {
    const className = `diff-view__label diff-view__label--${side}`;
    const glyph = (
      <span aria-hidden="true">{side === "before" ? MARKER.removed : MARKER.added}</span>
    );
    if (onOpenSide === undefined) {
      return (
        <span className={className} data-testid={`diff-view-side-${side}`}>
          {glyph}
          {label}
        </span>
      );
    }
    return (
      <button
        type="button"
        className={`${className} diff-view__label--link`}
        data-testid={`diff-view-side-${side}`}
        title={t.diff.openSide(label)}
        onClick={() => onOpenSide(side)}
      >
        {glyph}
        {label}
      </button>
    );
  };

  const numberCell = (row: DiffRow | undefined, side: "before" | "after") => (
    <td className="diff-view__ln">
      {(side === "before" ? row?.beforeLine : row?.afterLine) ?? ""}
    </td>
  );

  const markerCell = (row: DiffRow | undefined) => (
    <td
      className="diff-view__marker"
      aria-label={row ? kindLabel[row.kind] : undefined}
    >
      {row ? MARKER[row.kind] : NBSP}
    </td>
  );

  const textCell = (row: DiffRow | undefined, index: number | null) => (
    <td className="diff-view__text" data-cell-kind={row ? row.kind : "blank"}>
      {row && index !== null ? (
        <LineText
          text={row.text}
          segments={wordSegments.segments.get(index)}
          kind={row.kind}
        />
      ) : (
        NBSP
      )}
    </td>
  );

  return (
    <div className="diff-view" data-testid={testId} data-mode={mode}>
      <div className="diff-view__head">
        {sideHeading("before", beforeLabel ?? t.diff.beforeLabel)}
        {sideHeading("after", afterLabel ?? t.diff.afterLabel)}
        <span className="diff-view__stats">
          <span
            className="diff-view__stat diff-view__stat--added"
            data-testid="diff-view-stat-added"
            title={t.diff.addedLine}
          >
            {`+${addedCount}`}
          </span>
          <span
            className="diff-view__stat diff-view__stat--removed"
            data-testid="diff-view-stat-removed"
            title={t.diff.removedLine}
          >
            {`-${removedCount}`}
          </span>
        </span>
        {/* Says out loud that nothing is hidden — this surface used to fold
          * distant unchanged runs away behind an @@ separator, and a reader who
          * remembers that has to be told it no longer does (owner ⑥). */}
        <span className="diff-view__whole" data-testid="diff-view-whole-note">
          {t.diff.wholeDocNote}
        </span>
        <div
          className="diff-view__modes"
          role="group"
          aria-label={t.diff.viewLabel}
        >
          {MODES.map((which) => (
            <button
              key={which}
              type="button"
              className={`diff-view__mode${mode === which ? " diff-view__mode--on" : ""}`}
              data-testid={`diff-view-mode-${which}`}
              aria-pressed={mode === which}
              onClick={() => setMode(which)}
            >
              {which === "unified" ? t.diff.viewUnified : t.diff.viewSplit}
            </button>
          ))}
        </div>
      </div>
      <div className="diff-view__scroll">
        <table
          className={`diff-view__table${mode === "split" ? " diff-view__table--split" : ""}`}
          aria-label={t.diff.ariaLabel}
        >
          <tbody>
            {mode === "unified"
              ? result.rows.map((row, index) => (
                  <tr
                    key={index}
                    className={`diff-view__row diff-view__row--${row.kind}`}
                    data-testid="diff-view-row"
                    data-kind={row.kind}
                  >
                    {numberCell(row, "before")}
                    {numberCell(row, "after")}
                    {markerCell(row)}
                    {textCell(row, index)}
                  </tr>
                ))
              : buildSplitRows(result.rows, wordSegments.pairedTo).map(
                  (split, index) => {
                    const left =
                      split.left === null ? undefined : result.rows[split.left];
                    const right =
                      split.right === null
                        ? undefined
                        : result.rows[split.right];
                    return (
                      <tr
                        key={index}
                        className="diff-view__row diff-view__row--split"
                        data-testid="diff-view-split-row"
                        data-left-kind={left ? left.kind : "blank"}
                        data-right-kind={right ? right.kind : "blank"}
                      >
                        {numberCell(left, "before")}
                        {markerCell(left)}
                        {textCell(left, split.left)}
                        {numberCell(right, "after")}
                        {markerCell(right)}
                        {textCell(right, split.right)}
                      </tr>
                    );
                  }
                )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
