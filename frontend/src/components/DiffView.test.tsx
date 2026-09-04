// DiffView — the GitHub "Files changed" shaped diff surface (T-40f0).
//
// The owner ruled on six things; four of them are FACTS ABOUT THIS COMPONENT and
// each gets its own test here, deliberately NOT a snapshot (a snapshot goes red
// for every one of them at once and tells you nothing about which):
//   1. 單欄 is the DEFAULT and 兩欄對照 is reachable — `renders unified by
//      default…` + `switches to the two-column view…`
//   2. WHOLE-ROW colour — `tints the entire row…`
//   3. TWO number columns — asserted cell by cell in the unified test
//   4. CHARACTER-level tint — `marks only the characters that changed…`
// Plus the two the owner explicitly did NOT want: no collapsing
// (`renders every unchanged line…`), and the -/+ gutter kept as a second cue
// (`labels the marker cell…`).
//
// RED LINE, pinned twice below: the two states that LOOK blank must stay
// distinguishable. "the two versions are identical" and "the comparison was
// refused because the document is too large" are different screens, and a
// refusal must never be able to wear the identical-versions message.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import { DiffView, type DiffViewOptions } from "./DiffView";

function renderDiff(before: string, after: string, options?: DiffViewOptions) {
  return render(
    <I18nProvider>
      <DiffView before={before} after={after} options={options} />
    </I18nProvider>
  );
}

/** Every `<td>` of every row, in render order. */
function rowCells(container: HTMLElement): string[][] {
  return Array.from(container.querySelectorAll(".diff-view__row")).map((row) =>
    Array.from(row.querySelectorAll("td")).map((td) => td.textContent ?? "")
  );
}

/** The context marker — a non-breaking space, so the row keeps its height. */
const NBSP = "\u00a0";

const NUMBERED = (count: number) =>
  Array.from({ length: count }, (_, i) => `line ${i + 1}`).join("\n");

/** A pair sharing NOTHING at either end. `diffLines` trims the common head and
 * tail before it measures the remainder against `maxLines`, so two runs of the
 * SAME lines — which is what `NUMBERED` gives you — no longer reach the refusal
 * however long they are: only the part that still has to be compared counts.
 * Any fixture that means to exercise the refusal has to differ throughout. */
const UNRELATED = (count: number, tag: string) =>
  Array.from({ length: count }, (_, i) => `${tag} ${i + 1}`).join("\n");

describe("DiffView", () => {
  beforeEach(() => localStorage.clear());

  // ── ① 單欄 is the default; ③ two number columns ───────────────────────────
  it("renders unified by default: one full-width row per line, both sides' line numbers", () => {
    const { container } = renderDiff(
      "alpha\nbravo\ncharlie",
      "alpha\nBRAVO\ncharlie"
    );

    expect(
      container.querySelector(".diff-view")?.getAttribute("data-mode")
    ).toBe("unified");
    expect(
      container.querySelectorAll(".diff-view__mode--on")[0]?.textContent
    ).toBe(zh.diff.viewUnified);
    // No two-column rows exist while unified is on.
    expect(
      container.querySelectorAll('[data-testid="diff-view-split-row"]')
    ).toHaveLength(0);

    // ③ The removed line is #2 on the before side and nowhere on the after
    // side; the added line is the mirror; unchanged lines carry BOTH. Getting
    // these backwards is the classic unified-diff bug, so they are asserted
    // cell by cell.
    expect(rowCells(container)).toEqual([
      ["1", "1", NBSP, "alpha"],
      ["2", "", "-", "bravo"],
      ["", "2", "+", "BRAVO"],
      ["3", "3", NBSP, "charlie"],
    ]);
  });

  // ── ① 兩欄對照 stays reachable as the second option ────────────────────────
  it("switches to the two-column view and back, pairing the replaced line with its replacement", () => {
    const { container, getByTestId } = renderDiff(
      "alpha\nbravo\ncharlie",
      "alpha\nBRAVO\ncharlie"
    );

    fireEvent.click(getByTestId("diff-view-mode-split"));

    expect(
      container.querySelector(".diff-view")?.getAttribute("data-mode")
    ).toBe("split");
    // Three visual rows, not four: the `-` and the `+` that replaced it share
    // one row — that is what "side by side" means. Six cells per row: number,
    // gutter and text for each side.
    const split = Array.from(
      container.querySelectorAll('[data-testid="diff-view-split-row"]')
    );
    expect(split).toHaveLength(3);
    expect(rowCells(container)).toEqual([
      ["1", NBSP, "alpha", "1", NBSP, "alpha"],
      ["2", "-", "bravo", "2", "+", "BRAVO"],
      ["3", NBSP, "charlie", "3", NBSP, "charlie"],
    ]);
    // The sides are labelled per row so the tint can live on the halves.
    expect(split[1].getAttribute("data-left-kind")).toBe("removed");
    expect(split[1].getAttribute("data-right-kind")).toBe("added");

    // …and back to the default, which must still be the one-column view.
    fireEvent.click(getByTestId("diff-view-mode-unified"));
    expect(
      container.querySelector(".diff-view")?.getAttribute("data-mode")
    ).toBe("unified");
    expect(rowCells(container)[1]).toEqual(["2", "", "-", "bravo"]);
  });

  it("puts an insertion with nothing to pair against on the right half alone", () => {
    // The shape the pairing rule has to get right: a pure insertion has no `-`
    // to sit beside, so its left half is empty rather than borrowing a
    // neighbouring deletion.
    const { container, getByTestId } = renderDiff("alpha", "alpha\nbravo");
    fireEvent.click(getByTestId("diff-view-mode-split"));
    expect(rowCells(container)).toEqual([
      ["1", NBSP, "alpha", "1", NBSP, "alpha"],
      ["", NBSP, NBSP, "2", "+", "bravo"],
    ]);
  });

  // ── ② the whole row is coloured, not a glyph at its start ─────────────────
  it("tints the entire row — the number gutter included — and keeps the two kinds apart", () => {
    // jsdom does not resolve `color-mix`, so the CT guard
    // (visual-guards/diff-view.ct.spec.tsx) owns the "these two fills really
    // look different" half. What is checkable HERE is that the tint lives on the
    // ROW element and that every cell — both number columns and the gutter — is
    // inside it, which is what "edge to edge" requires.
    const { container } = renderDiff("bravo", "BRAVO");
    const removed = container.querySelector<HTMLElement>(
      '[data-kind="removed"]'
    )!;
    const added = container.querySelector<HTMLElement>('[data-kind="added"]')!;
    expect(removed.classList.contains("diff-view__row--removed")).toBe(true);
    expect(added.classList.contains("diff-view__row--added")).toBe(true);
    expect(removed.className).not.toBe(added.className);
    expect(removed.tagName).toBe("TR");
    expect(removed.querySelectorAll("td")).toHaveLength(4);
    expect(removed.querySelectorAll(".diff-view__ln")).toHaveLength(2);
    expect(removed.querySelector(".diff-view__marker")).toBeTruthy();
  });

  // ── ⑤ the gutter stays, as the non-colour cue ─────────────────────────────
  it("labels the marker cell so added/removed is not carried by colour alone", () => {
    const { container } = renderDiff("bravo", "BRAVO");
    const marker = (kind: string) =>
      container
        .querySelector(`[data-kind="${kind}"] .diff-view__marker`)
        ?.getAttribute("aria-label");
    expect(marker("removed")).toBe(zh.diff.removedLine);
    expect(marker("added")).toBe(zh.diff.addedLine);
    expect(
      container.querySelector('[data-kind="added"] .diff-view__marker')
        ?.textContent
    ).toBe("+");
  });

  // ── ④ character-level tint ────────────────────────────────────────────────
  it("marks only the characters that changed when a line was edited in place", () => {
    const { container } = renderDiff(
      "- 對 owner 預設中文，講功能與取捨。",
      "- 對 owner 預設中文，講功能與影響、取捨。"
    );

    const marked = (kind: string) =>
      Array.from(
        container.querySelectorAll(
          `[data-kind="${kind}"] .diff-view__word--${kind}`
        )
      ).map((n) => n.textContent);

    // The insertion is named exactly, and nothing else on the row is: a rule
    // that tinted the whole line would return the entire sentence here.
    expect(marked("added")).toEqual(["影響、"]);
    // The removed side of the pair shares every character, so it has nothing
    // marked — the surrounding text must NOT be reported as changed.
    expect(marked("removed")).toEqual([]);
    // The full text is still there, character for character — the tint wraps
    // the run, it does not replace or drop it.
    expect(
      container.querySelector('[data-kind="added"] .diff-view__text')
        ?.textContent
    ).toBe("- 對 owner 預設中文，講功能與影響、取捨。");
  });

  it("marks nothing inside a line that shares nothing with the one it replaced", () => {
    // The row colour already says "this whole line changed". Tinting every
    // character on top of it would make this indistinguishable from the case
    // above, and telling the two apart is the entire point of the feature.
    const { container } = renderDiff(
      "alpha\n舊的第二行\ncharlie",
      "alpha\nBRAVO\ncharlie"
    );
    expect(container.querySelectorAll(".diff-view__word")).toHaveLength(0);
    expect(rowCells(container)[1]).toEqual(["2", "", "-", "舊的第二行"]);
  });

  it("marks nothing on a line that was only added, with no counterpart to compare", () => {
    const { container } = renderDiff("alpha", "alpha\nbravo");
    expect(container.querySelectorAll(".diff-view__word")).toHaveLength(0);
  });

  // ── ⑥ no collapsing, no expanding: the whole document is on screen ────────
  it("renders every unchanged line, with no collapse separator anywhere", () => {
    const before = NUMBERED(20);
    const after = before.replace("line 15", "line 15 edited");
    const { container, queryByTestId, getByTestId } = renderDiff(before, after);

    // 20 before-side lines + the one added line = 21 rows. A surface that
    // folded the distant unchanged runs away would render about eight.
    expect(container.querySelectorAll(".diff-view__row")).toHaveLength(21);
    // The FIRST row is line 1 — the old behaviour started at line 12.
    expect(rowCells(container)[0]).toEqual(["1", "1", NBSP, "line 1"]);
    // …and the last is line 20, so nothing was trimmed from the tail either.
    expect(rowCells(container)[20]).toEqual(["20", "20", NBSP, "line 20"]);
    // The retired separator is gone, not merely empty.
    expect(queryByTestId("diff-view-skip")).toBeNull();
    expect(container.textContent).not.toContain("@@");
    // And the surface says out loud that nothing is hidden.
    expect(getByTestId("diff-view-whole-note").textContent).toBe(
      zh.diff.wholeDocNote
    );
  });

  it("counts the added and removed lines in its header", () => {
    const { getByTestId } = renderDiff(
      "alpha\nbravo\ncharlie",
      "alpha\nBRAVO\nBRAVISSIMO\ncharlie"
    );
    expect(getByTestId("diff-view-stat-added").textContent).toBe("+2");
    expect(getByTestId("diff-view-stat-removed").textContent).toBe("-1");
  });

  // ── RED LINE: the two blank-looking states stay distinguishable ───────────
  it("says the two versions are IDENTICAL rather than drawing an empty diff", () => {
    const { container, getByTestId, queryByTestId } = renderDiff(
      "alpha\nbravo",
      "alpha\nbravo"
    );
    expect(getByTestId("diff-view-empty").textContent).toBe(zh.diff.noChanges);
    expect(container.querySelectorAll(".diff-view__row")).toHaveLength(0);
    // It must not be mistakeable for the refusal.
    expect(queryByTestId("diff-view-too-large")).toBeNull();
  });

  it("says the comparison was REFUSED for size, never that the versions match", () => {
    const { container, getByTestId, queryByTestId } = renderDiff(
      UNRELATED(5, "old"),
      UNRELATED(6, "new"),
      { maxLines: 2 }
    );
    // The longer side's count is named — a bare "too long" leaves the owner
    // unable to tell how far past the ceiling the document is.
    expect(getByTestId("diff-view-too-large").textContent).toBe(
      `${zh.diff.tooLargeLead}6${zh.diff.tooLargeTail}`
    );
    expect(container.querySelectorAll(".diff-view__row")).toHaveLength(0);
    // A refusal must never read as "the two versions match" — this is the
    // assertion that keeps a refusal from hiding behind good news, and it is
    // the exact mirror of the one in the test above.
    expect(queryByTestId("diff-view-empty")).toBeNull();
    expect(container.textContent).not.toContain(zh.diff.noChanges);
  });

  it.each([
    ["zh", zh],
    ["en", en],
  ])("resolves its strings from the %s dictionary", (language, dict) => {
    localStorage.setItem("oc.language", language);

    const identical = renderDiff("same", "same");
    expect(identical.getByTestId("diff-view-empty").textContent).toBe(
      dict.diff.noChanges
    );
    identical.unmount();

    const refused = renderDiff(UNRELATED(5, "old"), UNRELATED(6, "new"), { maxLines: 2 });
    expect(refused.getByTestId("diff-view-too-large").textContent).toBe(
      `${dict.diff.tooLargeLead}6${dict.diff.tooLargeTail}`
    );
    refused.unmount();

    const { container, getByTestId } = renderDiff("bravo", "BRAVO");
    expect(
      container.querySelector(".diff-view__table")?.getAttribute("aria-label")
    ).toBe(dict.diff.ariaLabel);
    expect(
      container.querySelector(".diff-view__label--before")?.textContent
    ).toBe(`-${dict.diff.beforeLabel}`);
    expect(
      container.querySelector(".diff-view__label--after")?.textContent
    ).toBe(`+${dict.diff.afterLabel}`);
    // The layout switch and the "nothing folded" note are new strings — both
    // languages, or the switch is unreadable in one of them.
    expect(getByTestId("diff-view-mode-unified").textContent).toBe(
      dict.diff.viewUnified
    );
    expect(getByTestId("diff-view-mode-split").textContent).toBe(
      dict.diff.viewSplit
    );
    expect(getByTestId("diff-view-whole-note").textContent).toBe(
      dict.diff.wholeDocNote
    );
    expect(
      container.querySelector(".diff-view__modes")?.getAttribute("aria-label")
    ).toBe(dict.diff.viewLabel);
  });

  it("uses caller-supplied side labels over the dictionary defaults", () => {
    const { container } = renderDiff("bravo", "BRAVO", undefined);
    expect(
      container.querySelector(".diff-view__label--before")?.textContent
    ).toBe(`-${zh.diff.beforeLabel}`);

    const labelled = render(
      <I18nProvider>
        <DiffView
          before="bravo"
          after="BRAVO"
          beforeLabel="2026-07-30 14:02"
          afterLabel="現在"
        />
      </I18nProvider>
    );
    expect(
      labelled.container.querySelector(".diff-view__label--before")?.textContent
    ).toBe("-2026-07-30 14:02");
    expect(
      labelled.container.querySelector(".diff-view__label--after")?.textContent
    ).toBe("+現在");
  });
  /* 兩側標題可以點 (owner 2026-09-03, c-944088dceab0「兩份應該都要是連結」).
   *
   * The affordance is OPTIONAL on purpose, and that is the part worth pinning:
   * the document-history screen is already looking at the document both sides
   * belong to, so a link there would go nowhere. A heading must therefore be a
   * button exactly when the host handed over somewhere to go — never a button
   * that does nothing, which is the failure this whole round started from (a
   * mode switch that looked live and wasn't). */
  it("leaves the side headings as plain text when the host cannot open a side", () => {
    const { container } = renderDiff("alpha", "ALPHA");
    expect(container.querySelector("button.diff-view__label--link")).toBeNull();
    expect(
      container.querySelector(".diff-view__label--before")?.tagName
    ).toBe("SPAN");
  });

  it("turns each side heading into a button that opens THAT side", () => {
    const opened: string[] = [];
    const { getByTestId } = render(
      <I18nProvider>
        <DiffView
          before="alpha"
          after="ALPHA"
          beforeLabel="版本 #1"
          afterLabel="目前存檔內容"
          onOpenSide={(side) => opened.push(side)}
        />
      </I18nProvider>
    );

    const before = getByTestId("diff-view-side-before");
    const after = getByTestId("diff-view-side-after");
    expect(before.tagName).toBe("BUTTON");
    expect(after.tagName).toBe("BUTTON");
    // The label the reader reads is what the tooltip names — a heading that
    // says 「版本 #1」 must not offer to open something else.
    expect(before.getAttribute("title")).toBe(zh.diff.openSide("版本 #1"));
    expect(after.getAttribute("title")).toBe(zh.diff.openSide("目前存檔內容"));

    fireEvent.click(before);
    fireEvent.click(after);
    // Order matters: clicking the RED heading must not open the green side.
    expect(opened).toEqual(["before", "after"]);
  });
  /* Controlled means the HOST decides: the click is reported, and nothing moves
   * until the host says so.
   *
   * MUTANTS, both run: making the component read its own state INSTEAD of the
   * prop (and keep writing it) goes red here — `expected 'split' to be
   * 'unified'`. Leaving the stray local write in place ON ITS OWN does NOT, and
   * that is worth stating rather than hiding: while the prop is supplied the
   * shadow copy is never read, so it is a shape problem, not an observable one.
   * This guard pins what a reader can see. */
  it("does not change layout on its own while the host controls the mode", () => {
    const seen: string[] = [];
    const { getByTestId, container } = render(
      <I18nProvider>
        <DiffView
          before={"alpha\nbravo"}
          after={"alpha\nBRAVO"}
          mode="unified"
          onModeChange={(m) => seen.push(m)}
        />
      </I18nProvider>
    );

    fireEvent.click(getByTestId("diff-view-mode-split"));
    // The host heard about it…
    expect(seen).toEqual(["split"]);
    // …and nothing moved until the host says so.
    expect(container.querySelector(".diff-view")?.getAttribute("data-mode")).toBe(
      "unified"
    );
    expect(container.querySelector(".diff-view__row--split")).toBeNull();
  });
});
