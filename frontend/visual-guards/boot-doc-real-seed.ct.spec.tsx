// HOTSPOT — the boot-context block page rendering the REAL
// `seeds/system_interaction.md`, at phone widths.
//
// The gap this closes. `boot-doc-card.ct.spec.tsx` measures a hand-written
// eleven-line document. The system-interaction seed is the full document
// and carries what a synthetic fixture does not: fenced blocks, tables, long CJK
// headings, cited routes and ids. That page — the one an owner actually opens —
// is the one worth laying out.
//
// T-c33e: the page renders that document as ONE markdown block now instead of
// seventy-odd section rows. The claim being measured is UNCHANGED — no level of
// the chain may pan — and the chain is the same one, minus the rows that no
// longer exist.
//
// 🔴 MEASURE THE WHOLE CHAIN, NOT ONE ELEMENT. An element that refuses to
// shrink does not overflow ITSELF — it simply grows, and the width it took has
// to end up somewhere: an ancestor is dragged wide, and if any ancestor
// scrolls it absorbs the spill and every number above it reads 0. So every
// level from inside the card up to the scrolling element is measured, and
// `.settings` is named explicitly because it is `overflow-y: auto`, which the
// overflow spec coerces into `overflow-x: auto` — exactly the silent
// absorption that let an earlier page-only assertion sail over a broken phone
// (frontend/AGENTS.md 〈浮層寬度不可用 vw 夾〉, and the repo paid for it again
// in T-ee17).
//
// LEGITIMATE SCROLL REGIONS ARE EXEMPT, AND ONLY THOSE. `.doc-md pre` and
// `.doc-md table` declare `overflow-x: auto` on purpose (frontend/AGENTS.md
// 〈長 token 溢出〉 — flattening them to kill a page-level spill is the
// specific over-correction that section forbids). The subtree sweep therefore
// skips any element whose computed `overflow-x` is auto/scroll, and anything
// inside one. The NAMED chain above is not exempted from anything: a scroll
// region is allowed to hold wider content, it is not allowed to make the card
// or the page pan.
//
// Non-vacuity: the wait is on the seed's LAST heading, derived from the same
// bytes the mock serves, so a page that rendered a prefix fails rather than
// being measured.
//
// CONTROL: 1040 (the desktop content column's max width) is expected green and
// is NOT counted as coverage — it is there to say a fix did not simply move
// the breakage to desktop. The measured set is 320 / 375 / 390, matching this
// suite's existing widths.
//
// MEASURED (the pre-T-c33e sectioned page read 0 at every level of its own
// chain, and so does this one — the guard was written to find out, and that is
// still the answer):
//   drop `overflow-wrap: anywhere` from `.doc-md` (settings.css)
//     → RED at 320 only ("rendered document horizontal overflow … +45px"), and
//       the failing level is INSIDE the card while `.app__main`, `.app` and the
//       PAGE all read 0. A page-level assertion is green under this mutant.
//       That is the whole argument for walking the chain, measured rather than
//       asserted: `.settings` (overflow-y:auto) absorbs the spill. Green at
//       375/390/1040 — the pre-T-c33e sectioned version of this guard measured
//       the same +45 at the same width.
import { test, expect } from "@playwright/experimental-ct-react";
import {
  BootDocRealSeedStory,
  BootDocHeadSeedStory,
} from "./stories/BootDocRealSeedStory";

// 🔴 TWO SUBJECTS SINCE T-6f44, and the second is not padding. 系統互動 lost its
// read-only head (it was a title line, promoted into the body), and it was the
// only document this guard ever rendered — so the "read-only head" level of the
// chain would have been dropped from the suite entirely, retiring a protection
// because its fixture changed rather than because the risk did. 加速停止 still
// carries a head, so it keeps that level measured.
// ⚠️ THE STORY IS CHOSEN BY A LITERAL, NOT PASSED AS A COMPONENT. Playwright's
// CT transform extracts mount() arguments STATICALLY; handing it `<Story />`
// where Story is a parameter defeats that, and the fallback drags the whole
// import graph through Babel — which then chokes on the `?raw` markdown the
// story imports, with an error that points at this file and names the .md
// (measured: "Unexpected token (1:0)" on seeds/system_interaction.md).
function chainGuard(
  label: string,
  which: "system" | "head",
  expectHead: boolean
) {
for (const width of [320, 375, 390, 1040]) {
  test(`${label} @ width ${width}: no level of the chain pans`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1200 });
    const cmp =
      which === "head"
        ? await mount(<BootDocHeadSeedStory />)
        : await mount(<BootDocRealSeedStory />);

    // The page reads its document through the adapter, so the first paint is
    // empty. Wait on the rendered content itself rather than a timer — and
    // assert it against the story's derived expectation, so "found nothing to
    // check" can never read as "found nothing wrong".
    // 🔴 THE WAIT IS THE LAST HEADING, and that is the point: the page reads its
    // document through the adapter, so the first paint is empty, and waiting on
    // the last thing in the document says the WHOLE of it arrived rather than a
    // prefix. A count alone cannot say that.
    // ⚠️ `.doc-card__body > .doc-md`, not `.doc-md` — a boot-context document
    // renders a SECOND one above the editor for its read-only head (T-3201).
    // The head is measured too, one level down.
    await expect(cmp.locator(".doc-card__body > .doc-md")).toContainText(
      await cmp.getByTestId("story-last-heading").innerText()
    );
    const m = await page.evaluate((expectHead: boolean) => {
      const over = (el: Element) => el.scrollWidth - el.clientWidth;
      const scrolls = (el: Element) => {
        const ox = getComputedStyle(el).overflowX;
        return ox === "auto" || ox === "scroll";
      };
      const named: { where: string; over: number }[] = [];
      const push = (where: string, sel: string) => {
        const el = document.querySelector(sel);
        // -1 would PASS a `<= 1` assertion, so a level that vanished has to be
        // reported as a distinct, failing value rather than as an absence.
        named.push({ where, over: el ? over(el) : 9999 });
      };
      push("rendered document", ".doc-card__body > .doc-md");
      // The read-only half is inside the same card and is markdown too, so it
      // can pan the chain exactly as the body can (T-3201). It is a NAMED level
      // rather than left to the subtree sweep: a level that disappears has to
      // fail loudly, and the sweep reports absence as nothing at all.
      //
      // 🔴 WHETHER IT IS THERE AT ALL IS NOW PER-DOCUMENT (T-6f44). Four of the
      // ten lost their head — it was a title line and moved into the body — so
      // for those, "missing" is the correct state and the loud-failure sentinel
      // would be reporting the truth as a defect. The caller says which it
      // expects, and BOTH answers are asserted: a head that vanishes where one
      // is expected still fails loudly, and a head that reappears where none
      // should be fails too.
      if (expectHead) push("read-only head", ".doc-card__readonly-head");
      push("doc card head", ".doc-card__head");
      push("doc card note", ".doc-card__note");
      push("doc card body", ".doc-card__body");
      push("doc card", ".doc-card");
      push("settings surface", ".settings");
      push("app main column", ".app__main");
      push("app shell", ".app");
      const se = document.scrollingElement!;
      named.push({ where: "page", over: se.scrollWidth - se.clientWidth });

      // Worst offender anywhere under the card, for a message that names what
      // to cap — "something overflowed" alone is useless at this length.
      // Deliberate scroll regions and their descendants are skipped.
      let worst = { where: "(none)", over: 0 };
      const card = document.querySelector(".doc-card");
      if (card) {
        const walk = (el: Element) => {
          if (scrolls(el)) return;
          const o = over(el);
          if (o > worst.over) {
            const cls =
              typeof el.className === "string" && el.className.trim()
                ? "." + el.className.trim().split(/\s+/).join(".")
                : "";
            worst = { where: `${el.tagName.toLowerCase()}${cls}`, over: o };
          }
          for (const c of Array.from(el.children)) walk(c);
        };
        walk(card);
      }

      // The boxes the reader sees must also stay inside the viewport: a
      // scrollWidth check alone is satisfied by a card that grew and got
      // clipped by something upstream.
      const widthOf = (sel: string) =>
        document.querySelector(sel)?.getBoundingClientRect().width ?? -1;
      return {
        named,
        worst,
        boxes: {
          card: widthOf(".doc-card"),
          settings: widthOf(".settings"),
          main: widthOf(".app__main"),
        },
        headPresent: !!document.querySelector(".doc-card__readonly-head"),
      };
    }, expectHead);

    for (const { where, over } of m.named) {
      expect(
        over,
        `${where} horizontal overflow at ${width}px — widest offender under the card: ${m.worst.where} (+${m.worst.over}px)`
      ).toBeLessThanOrEqual(1);
    }
    expect(
      m.worst.over,
      `something under the card is wider than itself at ${width}px: ${m.worst.where}`
    ).toBeLessThanOrEqual(1);
    for (const [name, w] of Object.entries(m.boxes)) {
      expect(w, `${name} box width at ${width}px`).toBeGreaterThan(0);
      expect(w, `${name} must stay within the viewport`).toBeLessThanOrEqual(
        width + 1
      );
    }

    // Both directions of the head's presence, so neither "it vanished" nor "it
    // came back" can pass quietly.
    expect(
      m.headPresent,
      expectHead
        ? `${label} must render a read-only head — the document declares one, and a card that stopped printing it hides the half the owner may not edit`
        : `${label} must NOT render a read-only head — this document has none since T-6f44, and an empty panel is a divider under a heading that is not there`
    ).toBe(expectHead);
  });
}
}

chainGuard("the real system_interaction seed", "system", false);
chainGuard("the real accelerated_stop seed", "head", true);
