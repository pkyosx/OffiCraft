// MultiSelectFilter — the summary rule, tested on the SHARED component itself.
//
// WHY THIS FILE EXISTS AT ALL, given the page-level guards already cover it:
// those guards drive the dropdown through a PAGE, and each page only reaches
// some of the branches. The guard added with T-118 (TasksPage.jump.test.tsx)
// ticks the only option there is, so `present.length === options.length === 1`
// and the summary comes out of the 「exactly one → that option's label」 branch.
// A HALF-FIX — `present.length === 0 || (present.length === options.length &&
// present.length > 1)` — passes it while leaving the 「N ticked, all of them」
// case exactly as broken as before, and that case is the one 請示卡 hits when
// several people have opened cards. Independent review of ec5dd972 found this.
//
// The other half the page guards never touched is `constrained`, which drives
// the pill's active styling. Reverting that line alone left the WHOLE component
// suite green — nothing in the tree asserted the class.
//
// So: N > 1, and the class, tested where both pages inherit them at once.
import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";
import { MultiSelectFilter, type MultiSelectOption } from "./MultiSelectFilter";

const OPTIONS: MultiSelectOption[] = [
  { value: "mira", label: "Mira" },
  { value: "kyle", label: "Kyle" },
  { value: "penny", label: "Penny" },
];

function renderFilter(selected: string[]) {
  const onChange = vi.fn();
  const r = render(
    <MultiSelectFilter
      noun="負責人"
      allLabel="所有負責人"
      options={OPTIONS}
      selected={new Set(selected)}
      onChange={onChange}
      testId="ms"
    />
  );
  const trigger = () => r.getByTestId("ms");
  return {
    ...r,
    onChange,
    trigger,
    summary: () =>
      trigger().querySelector(".tasks__ms-summary")?.textContent ?? "",
    active: () => trigger().className.includes("tasks__ms-trigger--active"),
  };
}

describe("MultiSelectFilter 的摘要規則 (T-118, owner rc-33dfe1ff14cb)", () => {
  // 「有勾選的時候,就不要顯示所有人…完全沒勾跟有勾的情況本來就是不同的」
  //
  // An EMPTY set is a standing 「no constraint」 that keeps covering options that
  // appear later. A FULL set is a snapshot of the values that existed when the
  // ticks were made. Printing allLabel for both told a reader he had selected
  // everyone, and then silently excluded whoever loaded afterwards.
  it("🔴 ALL THREE ticked reads 「負責人 · 3」, not 「所有負責人」", () => {
    const f = renderFilter(["mira", "kyle", "penny"]);
    expect(
      f.summary(),
      "a full set is a snapshot, not the unconstrained state"
    ).toBe("負責人 · 3");
  });

  it("🔴 ALL THREE ticked lights the pill — a full set IS a constraint", () => {
    // The 清除篩選 button beside this pill keys on the page's own `anyFilter`
    // (size > 0), so a full set that did not light the pill put two controls in
    // one row making opposite claims about the same state.
    const f = renderFilter(["mira", "kyle", "penny"]);
    expect(
      f.active(),
      "the pill must render active whenever anything is ticked"
    ).toBe(true);
  });

  it("an EMPTY set is the one unconstrained state: allLabel, and no active pill", () => {
    const f = renderFilter([]);
    expect(f.summary()).toBe("所有負責人");
    expect(
      f.active(),
      "nothing ticked is the only state that must NOT light the pill"
    ).toBe(false);
  });

  // Rendered one at a time: two mounts in one test would put two elements on
  // the same testid and getByTestId would throw before asserting anything.
  it("a partial set of ONE keeps its label, and lights the pill", () => {
    // 🔴 NOT ["mira"]: mira is OPTIONS[0], so a summary that read
    // `options[0].label` instead of `present[0].label` would print the right
    // name anyway and this assertion would pass a broken component. Picking a
    // value that is NOT first is what makes it a test. (Independent review of
    // 910abfeb built exactly that mutant and it survived.)
    const f = renderFilter(["kyle"]);
    expect(f.summary()).toBe("Kyle");
    expect(f.active()).toBe(true);
  });

  it("a partial set of TWO reads 「負責人 · 2」, and lights the pill", () => {
    // 🔴 NOT ["mira", …]: mira is OPTIONS[0], so a summary that wrongly read
    // `options[0].label` instead of `present[0].label` would still print the
    // right name and this file would never notice. Start from the second one.
    const f = renderFilter(["kyle", "penny"]);
    expect(f.summary()).toBe("負責人 · 2");
    expect(f.active()).toBe(true);
  });

  it("a stale selected key never inflates the count nor fakes a constraint", () => {
    // `present` filters against the options that still exist, so every branch
    // below must key off `present`, never off the raw `selected` set.
    //
    // ⚠️ WHAT THIS DOES *NOT* CLAIM, stated because the earlier wording here
    // claimed it and it is false: it does NOT say a stale-only axis agrees with
    // the 清除篩選 button. On 任務頁 it does not. `passesExecutor` filters with
    // the UN-narrowed `appliedExecutor`, so a set holding nothing but a stale
    // key prints allLabel and leaves the pill dark (present.length === 0) while
    // the list narrows to zero rows and 清除篩選 sits right there with something
    // real to clear. That disagreement predates T-118 (`present` was already
    // narrowed at 59816a53; this package changed only the allLabel branch and
    // `constrained`), so it is out of this ticket's scope — but the comment must
    // not describe it as correct design. 請示卡 does not have it: openerOptions
    // back-fills an absent ticked id as a count-0 row, so there present.length
    // always equals openerFilter.size. Found by independent review of b8e88098.
    //
    // HOW A STALE KEY ARISES ON 任務頁, since it is NOT what you would guess:
    // NOT by that person's tasks being filtered away — executorOptions keeps a
    // ticked option whose count drops to 0, on purpose, or you could never
    // untick it. The one route is the ticked MEMBER leaving `memberOptions`
    // altogether (dismissed / soft-removed), which drops the row the tick
    // referred to. Written down because a reader who assumes the count-0 route
    // will conclude this is unreachable and delete the case. Found by
    // independent review of e744438b.
    //
    // 🔴 THREE CASES, AND ONLY THE LAST TWO REACH THE TEMPLATES. A stale key
    // ALONE takes the allLabel branch and never reaches 「· N」 or the single-name
    // branch at all, so asserting only that case left both unguarded: swapping
    // `present.length` for `selected.size` in the count template survived the
    // whole component suite, and swapping the `present.length === 1` GUARD for
    // `selected.size === 1` survived this file's first six assertions. Mixing a
    // stale key with real ones is what actually exercises them — with ONE real
    // option for the single-name branch, and with TWO for the count.
    const alone = renderFilter(["ghost"]);
    expect(alone.summary()).toBe("所有負責人");
    expect(alone.active()).toBe(false);
    alone.unmount();

    const one = renderFilter(["kyle", "ghost"]);
    expect(
      one.summary(),
      "one surviving option still prints its NAME — the guard counts present, not selected"
    ).toBe("Kyle");
    expect(one.active()).toBe(true);
    one.unmount();

    const mixed = renderFilter(["kyle", "penny", "ghost"]);
    expect(
      mixed.summary(),
      "the count is of options that still EXIST, not of the raw selection"
    ).toBe("負責人 · 2");
    expect(
      mixed.active(),
      "a surviving tick is a constraint, stale company or not"
    ).toBe(true);
  });
});
