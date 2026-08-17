// HOTSPOT — T-6630 ④: the step note's entry is a small corner control, and
// pressing it must open the READER without collapsing the task.
//
// Owner 2026-08-16 (second acceptance round):「我覺得備註不是很常按,可以放在 step
// 的右下角,點開再跳出另一個 Modal 打開嗎?像是我們開 .md 檔那種方式,只是沒有下載
// 或分享連結的功能」. So the note is no longer disclosed inside the card at all:
// `.task-step__note-open` is a corner button and the text opens in the cockpit's
// existing `MarkdownPreviewOverlay`, fed `source` — which is what makes it a
// reader with no download and no share link.
//
// FILE RENAMED (was taskcard-note-toggle-hitarea.ct.spec.tsx). The subject is no
// longer a full-width toggle row; nothing else in the repo referenced the old
// name (checked: the only spec name mentioned in `src/` is the anchor guard's).
//
// WHY THE COMPLAINT BEHIND THIS FAMILY IS A HIT-AREA FACT AND NOT A TASTE ONE:
// the whole <article class="task-card"> carries role="button", and its click
// handler vetoes only hits that land on an interactive element (`closest("button,
// a, textarea, …")`). A control sitting inside that surface is an exempt island
// in a field where every other pixel collapses the ENTIRE task. Missing it by a
// few px does not do nothing; it does the other, bigger thing. That is why the
// entry must stay a real <button> and must keep a 44px touch height even though
// it reads small on screen.
//
// 🔴 WHY THE HIT-TEST PROBE TAKES ITS COORDINATES FROM THE BUTTON'S OWN BOX,
// and why that is NOT the zero-power mistake the previous revision of this file
// warned about. The rule is: probe the box of the region the design CLAIMS. The
// predecessor claimed 「應該要是整列」 — a whole ROW — so probing the button's own
// box there was circular (every point inside a 66×16 button answers as that
// button, so the probe stayed green on exactly the shape the ticket removed) and
// the probe box had to be the WRAP's width. This revision claims something
// different and smaller: the owner asked for 「一個小按鈕在右下角」, so the
// claimed region IS the button, and its own box is the honest probe box. What
// the probe still has real power over is whether anything OVERLAPS the entry —
// a sibling row, the step's own padding box, a hover layer, a badge that grows
// — because any of those returns "CARD" and turns a press into "collapse the
// whole task". Size adequacy is not this probe's job and is not claimed by it;
// it is measured separately, against the 44px floor, by the first test.
//
// WHY CT AND NOT JSDOM: every claim here is a box, a hit test, or a real click
// travelling through a portal. jsdom has no layout engine, so a jsdom version
// passes on a 0×0 control and cannot tell an overlay apart from a fragment.
//
// 🔴 WHAT THIS FILE DELIBERATELY DOES NOT MEASURE: how the entry LOOKS. No
// contrast assertion, no per-theme run, by owner ruling (2026-08-16):「不需要驗證
// 什麼顏色好不好,這種都是負責人一開始確認沒問題就好,我們不會去改這種東西」.
// Appearance is signed off once, by eye, by him. Everything below is SIZE, REACH
// and SEMANTICS.
//
// WHY THE STORY IS MOUNTED BARE (no `.app__main` / `.tasks` ancestor chain,
// unlike the anchor story): nothing here is a scroll or an available-width
// measurement. Every assertion is local to the control or to the overlay, and
// neither changes when the card gets a scrollport. The repo's warning about bare
// mounts under-reporting real layout applies to the overflow/scroll family —
// which is why the anchor guard does reproduce the chain.
//
// MUTANT REGISTER — five mutants, each planted IN PLACE on the declaration
// named, run against ALL THREE note guards, and observed. Counts below are this
// file's own (10 tests) and EXPIRE if the case list changes; re-plant and
// re-measure rather than editing the prose. The same five are registered with
// their own counts in taskcard-note-anchor.ct.spec.tsx (9 tests) and
// taskcard-note-disclosure.ct.spec.tsx (4 tests), so a mutant that this file
// does not catch can be checked against the file that does.
//   M1 · TaskCard.tsx — render the entry as a <div> instead of a <button>
//        (`<button type="button" className="task-step__note-open"` → `<div
//        className="task-step__note-open"`, closing tag to match)
//        ⇒ HERE 6 failed / 4 passed. Red at BOTH widths: "the note entry is a
//        real button with a 44px touch target" (tag DIV), "pressing the entry
//        opens the reader and leaves the task card expanded" (the card
//        collapses — the reported bug, made worse), and "the keyboard can reach
//        the entry and open the reader with it" (a <div> is not tabbable, so
//        focus never lands on it). Elsewhere: anchor 9/9 red, disclosure 1 red.
//   M2 · tasks.css `.task-step__note-open` — delete `min-height: 44px`
//        ⇒ HERE 2 failed / 8 passed: the touch-target test at both widths,
//        MEASURED height 29px against the 44 floor.
//        🔴 CHECKED FOR OVER-DETERMINATION BEFORE COUNTING IT: nothing else in
//        the rule pads the control to 44 — with the declaration gone the box
//        really is 29px, so the mutant moves the number this test reads.
//        (`.task-step__note-actions` sets only flex alignment.) Elsewhere:
//        anchor 9/9 green, disclosure 4/4 green — the 44px floor is THIS file's
//        property and no other guard would notice its loss.
//   M3 · TaskCard.tsx, the entry's onClick — open the reader AND shove the
//        scrollport (`const sc = document.querySelector(".tasks") …; if (sc)
//        sc.scrollTop += 120;`)
//   M4 · TaskCard.tsx, the overlay's onClose — shove on the way out instead
//        (same two lines, `-= 120`)
//        ⇒ M3 and M4 both 10 passed / 0 failed HERE, by construction: this file
//        mounts bare and has no `.tasks` at all. They are the anchor guard's
//        mutants (5 red each there) and are registered here only so nobody
//        reads this file's green as coverage of scroll behaviour.
//        ⚠️ Both were first planted with a non-null assertion (`…)!.scrollTop
//        += 120`), which THREW in this file's bare mount and reddened 6 tests
//        for the wrong reason — a crash, not the property under test. The
//        null-guarded form above is the honest mutant.
//   M5 · TaskCard.tsx — render the entry on every step, not only on one that
//        has a note (`{step.note && (` → `{true && (`)
//        ⇒ HERE 6 failed / 4 passed (both widths of "pressing the entry…",
//        "the keyboard can reach…" and "the reader offers no download…" — the
//        strict locator now resolves to two entries). It is the DISCLOSURE
//        guard that owns this property, though, and names it: 3 red there,
//        including "a step carrying a note must be taller than one that carries
//        none". Elsewhere: anchor 9/9 green.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardNoteDisclosureStory } from "./stories/TaskCardNoteDisclosureStory";

const WIDTHS = [1280, 390];
const MIN_TOUCH = 44;
const ENTRY = "[data-testid='step-note-open']";

async function mountExpanded(mount: any, page: any, width: number) {
  await page.setViewportSize({ width, height: 1000 });
  const cmp = await mount(<TaskCardNoteDisclosureStory />);
  await cmp.locator(".task-card__head").first().click();
  await expect(cmp.locator(".task-card__workflow")).toBeVisible();
  return cmp;
}

for (const width of WIDTHS) {
  test(`[${width}] the note entry is a real button with a 44px touch target`, async ({
    mount,
    page,
  }) => {
    await mountExpanded(mount, page, width);

    const m = await page.evaluate(() => {
      const step = document.querySelector(
        "[data-step-id='s-note']"
      ) as HTMLElement;
      const el = step.querySelector(
        "[data-testid='step-note-open']"
      ) as HTMLElement;
      const r = el.getBoundingClientRect();
      return {
        tag: el.tagName,
        label: el.getAttribute("aria-label"),
        w: r.width,
        h: r.height,
        // 右下角: the entry sits at the END of its row, not stretched across it.
        // (「放在 step 的右下角」 — the previous ticket's full-width row is
        // exactly what this one replaced.)
        rowRight: (
          step.querySelector(".task-step__note-actions") as HTMLElement
        ).getBoundingClientRect().right,
        right: r.right,
        stepW: step.getBoundingClientRect().width,
      };
    });

    // A real <button>: the ONLY reason the card's `closest()` filter lets the
    // press through instead of collapsing the task.
    expect(m.tag, "the entry must be a real <button>").toBe("BUTTON");
    expect(m.label, "the entry must carry an aria-label").toBeTruthy();
    // A box a finger can find at all…
    expect(m.w * m.h, "the entry must occupy real pixels").toBeGreaterThan(0);
    // …and a thumb-sized one vertically. Small on screen is fine; small to the
    // thumb is what produced this ticket.
    expect(
      m.h,
      "a thumb has to be able to hit it"
    ).toBeGreaterThanOrEqual(MIN_TOUCH);
    // It is a CORNER control: flush with the right edge of its row, and not a
    // full-width band (that was the previous design, and the owner replaced it).
    expect(Math.abs(m.right - m.rowRight), "the entry must sit at the right edge").toBeLessThanOrEqual(1);
    expect(
      m.w / m.stepW,
      "the entry must be a small corner control, not the whole row"
    ).toBeLessThan(0.6);
  });

  test(`[${width}] pressing the entry opens the reader and leaves the task card expanded`, async ({
    mount,
    page,
  }) => {
    // 🔴 THE MOST IMPORTANT CLAIM IN THIS FILE. The whole card is role=button;
    // only interactive elements survive onCardToggleClick's closest() filter.
    const cmp = await mountExpanded(mount, page, width);
    await expect(page.locator(".md-preview")).toHaveCount(0);

    await cmp.locator(ENTRY).click();

    // The reader is open — and it is the portalled overlay, at document.body,
    // NOT a fragment inside the card (which is what would reflow the column).
    await expect(page.locator(".md-preview")).toBeVisible();
    expect(
      await page.evaluate(
        () =>
          document.querySelector(".md-preview")!.parentElement === document.body
      ),
      "the reader must be portalled to document.body"
    ).toBe(true);
    expect(
      await page.evaluate(
        () => !!document.querySelector(".md-preview")!.closest(".task-card")
      ),
      "the reader must not live inside the card"
    ).toBe(false);
    await expect(page.locator(".md-preview")).toContainText("handler 已完成");

    await expect(
      cmp.locator(".task-card__workflow"),
      "the card must NOT have collapsed — this is the bug the entry must not reproduce"
    ).toBeVisible();

    // …and closing the reader gives the card back untouched.
    await page.locator(".md-preview__close").click();
    await expect(page.locator(".md-preview")).toHaveCount(0);
    await expect(cmp.locator(".task-card__workflow")).toBeVisible();
  });

  test(`[${width}] every edge of the entry answers to the entry, not to the card`, async ({
    mount,
    page,
  }) => {
    // See the header for WHY the probe box is the button's own box here: the
    // region the design claims IS the button. What this catches is anything
    // OVERLAPPING it — the failure mode that turns a press into a collapse.
    await mountExpanded(mount, page, width);

    const hits = await page.evaluate(() => {
      const step = document.querySelector(
        "[data-step-id='s-note']"
      ) as HTMLElement;
      const el = step.querySelector(
        "[data-testid='step-note-open']"
      ) as HTMLElement;
      const r = el.getBoundingClientRect();
      const at = (x: number, y: number) => {
        const hit = document.elementFromPoint(x, y) as HTMLElement | null;
        if (!hit) return "NONE";
        if (hit.closest("[data-testid='step-note-open']")) return "ENTRY";
        // anything else in this card is the card's own toggle surface
        return hit.closest("[data-testid='task-card']") ? "CARD" : "OUTSIDE";
      };
      return {
        left: at(r.left + 2, r.top + r.height / 2),
        right: at(r.right - 2, r.top + r.height / 2),
        top: at(r.left + r.width / 2, r.top + 2),
        bottom: at(r.left + r.width / 2, r.bottom - 2),
        // POSITIVE CONTROL: "CARD" is a value this probe really can return —
        // 8px to the left of the entry is card surface, and a press there
        // collapses the task. Without this line the four ENTRYs above could be
        // the probe answering the same way everywhere.
        justOutside: at(r.left - 8, r.top + r.height / 2),
      };
    });
    expect(hits).toEqual({
      left: "ENTRY",
      right: "ENTRY",
      top: "ENTRY",
      bottom: "ENTRY",
      justOutside: "CARD",
    });
  });

  test(`[${width}] the keyboard can reach the entry and open the reader with it`, async ({
    mount,
    page,
  }) => {
    const cmp = await mountExpanded(mount, page, width);
    const entry = cmp.locator(ENTRY);

    const closed = await page.evaluate(() => {
      const el = document.querySelector(
        "[data-testid='step-note-open']"
      ) as HTMLElement;
      return {
        label: el.getAttribute("aria-label"),
        // A control that opens a modal must not announce a disclosure
        // relationship it no longer has: nothing expands in place any more.
        expanded: el.getAttribute("aria-expanded"),
        controls: el.getAttribute("aria-controls"),
        nestedInteractive: el.querySelectorAll(
          "button, a, input, select, textarea, [role='button']"
        ).length,
      };
    });
    expect(closed.label, "the entry must carry an aria-label").toBeTruthy();
    expect(
      closed.expanded,
      "nothing expands in place any more — no dangling aria-expanded"
    ).toBeNull();
    expect(closed.controls, "no dangling aria-controls").toBeNull();
    expect(closed.nestedInteractive).toBe(0);

    await entry.focus();
    expect(
      await page.evaluate(
        () => document.activeElement?.getAttribute("data-testid") ?? "NONE"
      ),
      "the entry must be reachable by keyboard"
    ).toBe("step-note-open");

    await page.keyboard.press("Enter");
    await expect(page.locator(".md-preview")).toBeVisible();
    await expect(
      cmp.locator(".task-card__workflow"),
      "Enter on the entry must not collapse the card"
    ).toBeVisible();
  });

  test(`[${width}] the reader offers no download and no share link`, async ({
    mount,
    page,
  }) => {
    // 「只是沒有下載或分享連結的功能」. This is not stripped here — the overlay is
    // fed `source` rather than a url, and download/share are the affordances it
    // only grows for stored bytes. The test pins the OUTCOME, so a caller that
    // ever starts passing a url reddens.
    const cmp = await mountExpanded(mount, page, width);
    await cmp.locator(ENTRY).click();
    await expect(page.locator(".md-preview")).toBeVisible();

    // POSITIVE CONTROL: the same enumeration DOES find the close button, so
    // "found nothing" below is not a broken selector.
    await expect(page.locator(".md-preview__close")).toHaveCount(1);
    await expect(page.locator(".md-preview__download")).toHaveCount(0);
    await expect(page.locator(".md-preview__share")).toHaveCount(0);
    // …and no download-shaped affordance under any other name.
    // 🔴 `a[download]`, NOT every `a[href]`: a note is agent-authored markdown
    // and routinely contains links (PRs, tickets). An independent review put a
    // link in the fixture's note and the `a[href]` form of this reddened on
    // LEGITIMATE CONTENT — a guard that fails on the data it is meant to carry
    // teaches the next reader to weaken it. What must not exist here is a way to
    // pull the bytes down or to mint a share link, and that is what is asserted.
    expect(
      await page.evaluate(
        () => ({
          download: document.querySelectorAll(".md-preview a[download]").length,
          shareish: document.querySelectorAll(
            ".md-preview [class*='share'], .md-preview [class*='download']"
          ).length,
        })
      ),
      "the reader must expose no download or share affordance"
    ).toEqual({ download: 0, shareish: 0 });
  });

  test(`[${width}] clicking the reader's backdrop closes it and leaves the card open`, async ({
    mount,
    page,
  }) => {
    // 🔴 THE COVERAGE HOLE AN INDEPENDENT REVIEW FOUND, now closed. The card
    // comment used to claim the portal was what protected it; a portal bubbles
    // along the REACT tree, and the overlay is rendered inside the <article>
    // that carries onCardToggleClick. What actually protects it is the filter's
    // `[role='dialog']` entry plus the panel's stopPropagation — and every test
    // here used to close the reader with `.md-preview__close`, a <button>, which
    // the filter exempts for a different reason. So deleting `[role='dialog']`
    // was a silent change: 23/23 green while a backdrop click closed the reader
    // AND collapsed the task under it. This test clicks the BACKDROP.
    await mountExpanded(mount, page, width);
    await page.locator("[data-testid='step-note-open']").first().click();
    await expect(page.locator(".md-preview")).toBeVisible();

    const box = (await page.locator(".md-preview").boundingBox())!;
    // 6px inside the overlay's own top-left corner: the backdrop, never the
    // panel (which is centred), and never any control.
    await page.mouse.click(box.x + 6, box.y + 6);

    await expect(page.locator(".md-preview")).toHaveCount(0);
    await expect(
      page.locator(".task-card__workflow"),
      "closing the reader by its backdrop must not collapse the task under it"
    ).toBeVisible();
  });
}