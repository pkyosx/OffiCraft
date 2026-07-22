// 使用說明 (product guide) — the TOP-LEVEL tab (owner 2026-07-22:「user guide
// 改放在 tab 中,監控的右邊,不要放在 settings 裡」).
//
// This suite is the former "使用說明" half of SettingsPage.breadcrumbs.test.tsx,
// moved here VERBATIM in its assertions and re-rooted on the new page: every
// crumb trail simply loses its leading 設定 segment, because 設定 is no longer
// the parent. Nothing was relaxed in the move — the doc-link contract
// (T-68f1) below is assertion-for-assertion the same, and the two things the
// tab gained (a hash route, and no settings parent) are pinned as ADDITIONAL
// tests rather than as softened old ones.
//
// Runs against the REAL mock adapter, like the sibling SettingsPage tests.
//
// 🔴 KNOWN COVERAGE GAP — no test anywhere renders the REAL docs/guide/*.md
// bytes through the product's own Markdown wiring. Every suite here, and the
// mock adapter's fixtures, render a FIXTURE instead of what actually ships.
//
// Why that is a real hole and not a tidiness note: this renderer is a SUBSET of
// GitHub markdown (single-`*` emphasis, for one, is not supported). So a doc
// can look perfect on GitHub, read fine in review, and still render literal
// syntax to the user. It has already happened once — a screenshot caption
// written with single-`*` emphasis shipped and was caught by hand, because
// nothing in CI could see it. The next one will not announce itself either.
//
// What the next person has to do: read each docs/guide/*.md, run it through the
// real <Markdown> with the real resolveImageSrc/resolveDocLink wiring, strip
// <code>/<pre> (literal markers there are intentional), and fail on leftover
// GitHub-only syntax in the remaining prose. It needs no browser and no server
// — the docs are on disk and the renderer is a plain component. Pair it with a
// positive control (inject a single-`*` caption and watch it go red) so a
// silently broken probe cannot read as "0 findings".
//
// No ticket is tracking this; this comment is the only record.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { GuidePage } from "./UserGuidePage";
import { __resetMock } from "../api/mock";

const g = zh.guide;

function renderGuide() {
  return render(
    <I18nProvider>
      <GuidePage />
    </I18nProvider>
  );
}

type Utils = ReturnType<typeof renderGuide>;

/** The breadcrumb's segment labels, in order (separators stripped). */
function crumbSegs(utils: Utils): string[] {
  return Array.from(
    utils.container.querySelectorAll("nav.crumbs .crumbs__seg")
  ).map((el) => (el.textContent ?? "").replace(/^›/, ""));
}

/** Wait until the RENDERED DOC BODY is the doc titled `title`.
 * Deliberately keyed on `.doc-md h1` — the markdown's OWN heading, i.e. bytes
 * that came from the server — rather than on getByRole("heading"). A role query
 * would also match whatever chrome the page happens to render around the body,
 * so it could not tell "the body switched" from "the header switched"; keying
 * on the rendered document proves the new doc's CONTENT arrived. */
async function waitForDocBody(utils: Utils, title: string) {
  await waitFor(() => {
    expect(utils.container.querySelector(".doc-md h1")?.textContent).toBe(title);
  });
}

/** The unified header contract: breadcrumb segments + NO back button. */
function expectHeader(utils: Utils, segs: string[]) {
  expect(crumbSegs(utils)).toEqual(segs);
  // 返回鍵移除 — neither the old .set-back row nor any 返回-labelled button.
  expect(utils.container.querySelector(".set-back")).toBeNull();
  expect(utils.queryByRole("button", { name: "返回" })).toBeNull();
}

beforeEach(() => {
  __resetMock();
  // The page reads its view off the hash — start every test on the list.
  history.replaceState(null, "", window.location.pathname + "#guide");
});

describe("使用說明 · page header + navigation", () => {
  it("列表: 使用說明 + title; doc: 使用說明 › <title>", async () => {
    const utils = renderGuide();
    const entries = await utils.findAllByTestId("guide-doc-entry");
    // The LIST carries no trail at all. This assertion used to read
    // `expectHeader(utils, [g.title])` — a one-segment trail — and what it was
    // defending was "the guide is no longer under 設定". An EMPTY trail defends
    // that strictly harder: with no segments there is no parent to be wrong
    // about. So the target moved, the guarantee did not weaken. (The trail was
    // dropped because a single terminal segment is plain text with nothing to
    // click, and it made 使用說明 appear three times above the fold — tab,
    // crumb, h1.) The 設定 negative is asserted explicitly rather than implied:
    expectHeader(utils, []);
    expect(utils.container.querySelector("nav.crumbs")).toBeNull();
    expect(utils.queryByText("設定")).toBeNull();
    // The page is still HEADED by 使用說明, and now exactly once.
    expect(utils.getByRole("heading", { name: g.title })).toBeTruthy();
    expect(utils.getAllByRole("heading", { name: g.title })).toHaveLength(1);

    // Open the doc — its markdown renders and its title trails the crumb.
    const entry = entries[0];
    const docTitle = entry.textContent ?? "";
    fireEvent.click(entry);
    await waitForDocBody(utils, docTitle);
    expectHeader(utils, [g.title, docTitle]);

    // The 使用說明 crumb jumps back to the list.
    fireEvent.click(utils.getByRole("button", { name: g.title }));
    await utils.findAllByTestId("guide-doc-entry");
    expectHeader(utils, []);
  });

  // The doc title used to be printed THREE times before any prose — breadcrumb
  // tail, page <h1>, and the markdown's own <h1> — which cost half a desktop
  // screen and two thirds of a 390px one. The page <h1> was the removable copy:
  // the server derives a doc's title FROM its first `# ` heading (api_docs.go
  // docTitle), so it was a guaranteed duplicate of the heading rendered right
  // below it. This pins the count, not the layout: the trail still names the
  // doc (navigation), the body still opens with its own heading (content).
  it("a doc names itself ONCE outside the trail, not three times", async () => {
    const utils = renderGuide();
    const entries = await utils.findAllByTestId("guide-doc-entry");
    const docTitle = entries[0].textContent ?? "";
    fireEvent.click(entries[0]);
    await waitForDocBody(utils, docTitle);

    // Exactly one heading carries the title, and it is the DOC BODY's own.
    const headings = utils.getAllByRole("heading", { name: docTitle });
    expect(headings).toHaveLength(1);
    expect(headings[0].closest(".doc-md")).not.toBeNull();
    // The page-level header node is gone entirely (it was the only other
    // .settings__title--doc on this view).
    expect(
      utils.container.querySelectorAll(".settings__title--doc"),
    ).toHaveLength(0);
    // Navigation is untouched: the trail still ends on this doc and still
    // offers 使用說明 as a real button back to the list.
    expectHeader(utils, [g.title, docTitle]);
    expect(utils.getByRole("button", { name: g.title })).toBeTruthy();

    // ...and the landmark a screen reader announces is this page's region, not
    // 設定. Breadcrumbs.test.tsx pins the component's rule; this pins the real
    // page's WIRING, which is what was actually wrong: promoting the guide out
    // of Settings left the trail announcing 設定 here. Queried by role+name
    // (an aria-label is not a text node, so the `queryByText("設定")` check
    // above cannot see it — that is exactly how this survived).
    expect(
      utils.getByRole("navigation", { name: g.title }),
      "the guide's breadcrumb landmark must be named after the guide",
    ).toBeTruthy();
    expect(
      utils.queryByRole("navigation", { name: zh.settings.title }),
      "no landmark on the guide tab may announce itself as 設定",
    ).toBeNull();
  });

  // NEW with the promotion to a tab. As a settings sub-page the open doc lived
  // in a local useState with no route, so a refresh (or the top-bar reload
  // button, which is a full reload) silently dropped the reader back to the
  // list and no doc could be linked to.
  it("the open doc is in the URL (#guide/<slug>), so it survives a reload", async () => {
    const utils = renderGuide();
    const entries = await utils.findAllByTestId("guide-doc-entry");
    fireEvent.click(entries[0]);
    const docTitle = entries[0].textContent ?? "";
    await waitForDocBody(utils, docTitle);
    await waitFor(() => {
      expect(window.location.hash).toMatch(/^#guide\/.+/);
    });
    const deepLink = window.location.hash;

    // Re-mount from that URL alone (what a reload does): same doc.
    utils.unmount();
    history.replaceState(null, "", window.location.pathname + deepLink);
    const reloaded = renderGuide();
    await waitForDocBody(reloaded, docTitle);
    expectHeader(reloaded, [g.title, docTitle]);
  });

  // A slug that is not embedded must not fabricate a doc; the page reports the
  // honest load error and the crumb still walks back to the list.
  it("an unknown slug in the hash self-heals to an error, not a fake doc", async () => {
    history.replaceState(null, "", window.location.pathname + "#guide/nope");
    const utils = renderGuide();
    await utils.findByText(g.loadError);
    fireEvent.click(utils.getByRole("button", { name: g.title }));
    await utils.findAllByTestId("guide-doc-entry");
  });
});

// ── T-68f1 · in-app doc links inside 使用說明 ────────────────────────────────
// The defect owner saw: a doc's `[介面說明](interface.md)` printed as literal
// `[…](…)` source text and could not be clicked. The fix is opt-in per surface,
// so this suite pins BOTH halves — the link navigates HERE, and the shapes that
// must never become clickable still are not, ON THIS PAGE (the unit tests pin
// the renderer; this pins the wiring, which is where a resolver can be
// forgotten or handed the wrong slug rule).
describe("使用說明 · in-app doc links (T-68f1)", () => {
  /** Open the doc whose list entry title matches. */
  async function openDoc(utils: Utils, title: string) {
    const entries = await utils.findAllByTestId("guide-doc-entry");
    const entry = entries.find((e) => (e.textContent ?? "").includes(title));
    if (!entry) throw new Error(`no guide entry titled ${title}`);
    fireEvent.click(entry);
    await waitForDocBody(utils, title);
  }

  it("a cross-doc link is a real control and switches to THAT doc", async () => {
    const utils = renderGuide();
    await openDoc(utils, "介面說明");

    // The literal source text is GONE — that alone was the visible symptom.
    expect(utils.container.textContent).not.toContain("](why.md)");

    const link = utils.getByRole("button", { name: "為什麼是 OffiCraft" });
    expect(link.className).toContain("md-doclink");
    fireEvent.click(link);

    // The destination doc is showing, and the crumb trail followed it.
    await waitForDocBody(utils, "為什麼是 OffiCraft");
    expectHeader(utils, [g.title, "為什麼是 OffiCraft"]);
  });

  it("chains: doc → doc → doc, each hop landing on the linked doc", async () => {
    const utils = renderGuide();
    await openDoc(utils, "為什麼是 OffiCraft");
    fireEvent.click(utils.getByRole("button", { name: "介面說明" }));
    await waitForDocBody(utils, "介面說明");
    fireEvent.click(utils.getByRole("button", { name: "為什麼是 OffiCraft" }));
    await waitForDocBody(utils, "為什麼是 OffiCraft");
    expectHeader(utils, [g.title, "為什麼是 OffiCraft"]);
  });

  // The same doc is reachable by two different written forms, because the
  // repo's own docs are written for TWO base directories: README-style
  // `docs/guide/x.md` (read from the repo root) and guide-internal `x.md`.
  // build-docsdist flattens both onto one slug — a mapping that kept the
  // directory prefix would silently drop every repo-root-relative link.
  it("a repo-root-relative form of the same target lands on the same doc", async () => {
    const utils = renderGuide();
    await openDoc(utils, "為什麼是 OffiCraft");
    fireEvent.click(utils.getByRole("button", { name: "介面說明(長路徑)" }));
    await waitForDocBody(utils, "介面說明");
    expectHeader(utils, [g.title, "介面說明"]);
  });

  // settings.css's `.md-doclink` rationale states WHERE the post-click URL
  // comes from: the target's BASENAME, and only after the existence check —
  // never the target verbatim, never as a path. That clause had no guard until
  // now (review3-recheck3 RC3-2 disproved the previous wording by measuring the
  // hash by hand). Both written forms of the SAME doc — the guide-internal
  // `interface.md` and the repo-root-relative `docs/guide/interface.md` — must
  // land on the identical hash, which is exactly what "only the basename can
  // carry information" means; and an UNSHIPPED target must move the URL not at
  // all, which is the `docs.some(...)` half of the same sentence.
  it("the post-click hash is the target's BASENAME, and only for a shipped doc", async () => {
    const utils = renderGuide();
    await openDoc(utils, "為什麼是 OffiCraft");
    expect(window.location.hash).toBe("#guide/why");

    fireEvent.click(utils.getByRole("button", { name: "介面說明" }));
    await waitForDocBody(utils, "介面說明");
    expect(window.location.hash).toBe("#guide/interface");

    // The long-path form of the same target: same basename → same URL. If the
    // directory prefix reached the hash, this would read #guide/docs/guide/….
    fireEvent.click(utils.getByRole("button", { name: "為什麼是 OffiCraft" }));
    await waitForDocBody(utils, "為什麼是 OffiCraft");
    fireEvent.click(utils.getByRole("button", { name: "介面說明(長路徑)" }));
    await waitForDocBody(utils, "介面說明");
    expect(window.location.hash).toBe("#guide/interface");

  });

  // Every in-app link lives in the BODY of a doc, so the reader is always far
  // down the page when they click one. The scrolling box (`.settings`) is the
  // SAME DOM node across doc→doc — GuidePage renders the same component type,
  // so React reconciles rather than remounting — and the old offset therefore
  // survived the switch: the reader landed mid-way into the new document, with
  // no title and no breadcrumb on screen to say anything had changed.
  //
  // jsdom has no layout, so this pins the RESET, not real scrolling; the
  // companion CT guard (visual-guards/guide-doc-scroll.ct.spec.tsx) checks the
  // same thing in a real browser with real overflow, where "is .settings even
  // the element that scrolls" is decidable at all.
  it("switching docs returns the reader to the TOP of the new one", async () => {
    const utils = renderGuide();
    await openDoc(utils, "為什麼是 OffiCraft");

    const box = utils.container.querySelector(".settings") as HTMLElement;
    expect(box, "the doc view must have a .settings scroll box").toBeTruthy();

    // Put the reader deep into the current doc, where the links actually are.
    box.scrollTop = 1759;
    expect(box.scrollTop).toBe(1759);

    fireEvent.click(utils.getByRole("button", { name: "介面說明" }));
    await waitForDocBody(utils, "介面說明");

    // Same node (proving this is the reconcile case, not a remount that would
    // have reset the offset for free) and back at the top.
    expect(utils.container.querySelector(".settings")).toBe(box);
    expect(
      box.scrollTop,
      "a new doc must start at its own beginning, not mid-page",
    ).toBe(0);
  });

  // Browser BACK gets the same rule, and that is a deliberate choice rather
  // than an accident of the implementation: Back changes the hash, which
  // changes `slug`, which fires the same reset. Restoring the offset the reader
  // left behind was the alternative and was rejected — the doc body arrives
  // asynchronously, so a restore would have to wait for the new content to
  // paint and would visibly jump whenever it guessed wrong. One rule in every
  // direction beats a rule that is right half the time. Pinned so that a future
  // "let's restore scroll on Back" change is a deliberate decision, not a
  // silent divergence between forward and backward navigation.
  it("browser Back lands at the top too, not at the old offset", async () => {
    const utils = renderGuide();
    await openDoc(utils, "為什麼是 OffiCraft");
    expect(window.location.hash).toBe("#guide/why");

    fireEvent.click(utils.getByRole("button", { name: "介面說明" }));
    await waitForDocBody(utils, "介面說明");
    expect(window.location.hash).toBe("#guide/interface");

    // The reader scrolls down inside the doc they navigated TO...
    const box = utils.container.querySelector(".settings") as HTMLElement;
    box.scrollTop = 1200;
    expect(box.scrollTop).toBe(1200);

    // ...then presses Back. This is a real history navigation: navigateHash
    // assigns window.location.hash, which pushes an entry and fires hashchange.
    history.back();
    await waitFor(() => {
      expect(window.location.hash).toBe("#guide/why");
    });
    await waitForDocBody(utils, "為什麼是 OffiCraft");

    expect(
      box.scrollTop,
      "Back must land at the top of the doc returned to, like every other switch",
    ).toBe(0);
  });

  it("an unshipped target never reaches the URL at all", async () => {
    const utils = renderGuide();
    await openDoc(utils, "安裝、升級與移除");
    // ../dev/agent-env.md derives the slug "agent-env", which the server never
    // served — `docs.some(...)` declines it, so it is not even a control and
    // the URL has no way to acquire it.
    const parked = window.location.hash;
    expect(parked).toBe("#guide/install");
    expect(
      utils.queryByRole("button", { name: "../dev/agent-env.md" }),
    ).toBeNull();
    expect(window.location.hash).toBe(parked);
    expect(window.location.hash).not.toContain("agent-env");
  });

  // A doc that links to ITSELF (interface.md, read from interface) must not
  // become a button: clicking it would re-enter the same slug, re-fetch, and
  // push a crumb that goes nowhere — a control whose only effect is a flicker.
  // The `next === slug` guard in UserGuideDoc owns this, and until this fixture
  // existed no doc in mockDocs linked to itself, so deleting that guard left
  // the whole suite green (review3 §2.5).
  it("a link to the CURRENT doc stays literal text, not a self-button", async () => {
    const utils = renderGuide();
    await openDoc(utils, "介面說明");
    expect(utils.container.textContent).toContain(
      "[介面說明(本頁)](interface.md)",
    );
    expect(utils.queryByRole("button", { name: "介面說明(本頁)" })).toBeNull();
    // The cross-doc link in the SAME doc is still a real control — this test
    // fails for the right reason, not because links stopped working here.
    expect(
      utils.queryByRole("button", { name: "為什麼是 OffiCraft" }),
    ).not.toBeNull();
  });

  it("an UNSHIPPED target (../dev/agent-env.md) stays literal, not a dead button", async () => {
    const utils = renderGuide();
    await openDoc(utils, "安裝、升級與移除");
    expect(utils.container.textContent).toContain(
      "[../dev/agent-env.md](../dev/agent-env.md)",
    );
    expect(
      utils.queryByRole("button", { name: "../dev/agent-env.md" }),
    ).toBeNull();
  });

  it("an external link stays an external anchor (new tab, noopener)", async () => {
    const utils = renderGuide();
    await openDoc(utils, "安裝、升級與移除");
    const a = utils.container.querySelector(".doc-md a") as HTMLAnchorElement;
    expect(a.getAttribute("href")).toBe(
      "https://github.com/pkyosx/OffiCraft/releases",
    );
    expect(a.getAttribute("rel")).toBe("noopener noreferrer");
    expect(a.getAttribute("target")).toBe("_blank");
  });

  it("a javascript: target stays inert literal text on this page too", async () => {
    const utils = renderGuide();
    await openDoc(utils, "為什麼是 OffiCraft");
    // No anchor and no in-app button carries the javascript: payload — it is
    // still the literal source text the renderer falls back to.
    expect(utils.container.textContent).toContain(
      "[別點我](javascript:alert(1))",
    );
    expect(utils.queryByRole("button", { name: "別點我" })).toBeNull();
    expect(
      Array.from(utils.container.querySelectorAll(".doc-md a")).map((el) =>
        el.getAttribute("href"),
      ),
    ).not.toContain("javascript:alert(1)");
  });

  it("strips the [!NOTE] marker instead of printing it as text", async () => {
    const utils = renderGuide();
    await openDoc(utils, "安裝、升級與移除");
    expect(utils.container.textContent).not.toContain("[!NOTE]");
    const q = utils.container.querySelector(".doc-md blockquote");
    expect(q?.className).toContain("md-alert--note");
    expect(q?.textContent).toContain("loopback");
  });
});
