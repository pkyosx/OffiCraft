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
 * Deliberately keyed on `.doc-md h1` (the markdown's own heading), not on
 * getByRole("heading"): the page ALSO prints the doc title as its page header,
 * so a role query matches two nodes and cannot tell "the body switched" from
 * "the header switched". */
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
    // The trail no longer starts at 設定 — the guide IS a root now.
    expectHeader(utils, [g.title]);
    expect(utils.getByRole("heading", { name: g.title })).toBeTruthy();

    // Open the doc — its markdown renders and its title trails the crumb.
    const entry = entries[0];
    const docTitle = entry.textContent ?? "";
    fireEvent.click(entry);
    await waitForDocBody(utils, docTitle);
    expectHeader(utils, [g.title, docTitle]);

    // The 使用說明 crumb jumps back to the list.
    fireEvent.click(utils.getByRole("button", { name: g.title }));
    await utils.findAllByTestId("guide-doc-entry");
    expectHeader(utils, [g.title]);
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
