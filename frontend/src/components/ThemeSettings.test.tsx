// ThemeSettings (T-16a1 P3b): the 設定/主題 management surface that theme
// management MOVED to from the profile dropdown. Covers import (moved verbatim
// + injection block), friendly grouped colour editing, and the 用詞 (wording)
// overlay editor round-trip.

import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { render, fireEvent, within, act, waitFor } from "@testing-library/react";
import { THEME_COLOR_TOKENS } from "../styles/themeTokens.generated";
import { MESSAGE_KEYS } from "../i18n/messageKeys.generated";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { makeMessages } from "../i18n/compose";
import { ThemeSettings } from "./ThemeSettings";
import { tokenMeta } from "../lib/themeTokenMeta";
import { MAX_AVATAR_BYTES, MAX_CUSTOM_THEMES } from "../lib/themeBundleCore";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { ApiError } from "../api/errors";
import { codeForStatus } from "../api/errorCodes";
import type { ThemeBundle } from "../lib/themeBundle";
import { setToken, clearToken } from "../api/auth";

const SENTINEL = "偽造";

// ── Why this file never runs a *ByLabelText / *ByRole query against the whole
// container once the EDIT view is open (T-e2e9; owner ruling rc-cf2a2982f31d) ──
//
// Opening the edit view mounts the whole 用詞 list — all 866 controlled inputs,
// because that list shares one form with the colour and font fields (the list is
// deliberately NOT virtualised, owner ruling 2026-08-02).
//
// dom-testing-library's label lookup walks every labelable element and reads
// `input.labels`; jsdom answers each of those by re-walking the entire document.
// N inputs therefore cost N document walks PER QUERY — O(N²). Measured on this
// very form, same element, same run:
//
//     document.getElementById("ts-canvas-bg-mode")           0 ms
//     container.querySelectorAll("input")  (all 866, once) 119 ms
//     within(<the row it lives in>).getByLabelText(…)      189 ms
//     utils.getByLabelText(…)   ← whole container       16,813 ms
//
// So the fix is not a bigger ceiling (one was tried: 5s → 20s, and 3 of 5 runs
// still went red on a busy machine). The fix is to SCOPE the query. Every helper
// below narrows to a container holding a handful of elements first.
//
// 🔴 Scoping deliberately KEEPS the label lookup rather than swapping in an id.
// `*ByLabelText` does not only find the element — it proves the input is really
// bound to its label, which is what a screen reader reads out. Reaching for
// `getElementById` would be faster still (0 ms) but would leave a broken or
// missing label GREEN, and two of the three capabilities the owner traded the
// virtualisation back for are accessibility ones. Narrowing the container costs
// the binding nothing: every query below is still a label query, so each one is
// still a witness that its own field is labelled.
//
// 🔴 THIS IS A TEST-ENVIRONMENT COST, NOT A USER COST. Do not quote these numbers
// as evidence that dropping virtualisation made the product slow — a real browser
// has no `input.labels` re-walk, and the CT guards open the same 866-row panel in
// tens of milliseconds.

/** The one row a colour token is edited in — a handful of elements, not 866. */
function colourRow(
  utils: ReturnType<typeof render>,
  label: string
): HTMLElement {
  const row = Array.from(
    utils.container.querySelectorAll(".ts-color-row")
  ).find((r) => r.querySelector(".ts-color-name")?.textContent === label);
  if (!row) throw new Error(`no .ts-color-row labelled ${label}`);
  return row as HTMLElement;
}

/** The image slot group headed by `label` — found by its own heading, NOT by
 * position, for the reason spelled out in canvasBgSlots below. */
function imageSlots(
  utils: ReturnType<typeof render>,
  label: string
): HTMLElement {
  const group = Array.from(
    utils.container.querySelectorAll(".ts-avatar-slots")
  ).find((g) => g.querySelector(".ts-avatar-label")?.textContent === label);
  if (!group) throw new Error(`no .ts-avatar-slots headed ${label}`);
  return group as HTMLElement;
}

/** The 外框背景 slot group — where the lay-down mode <select> lives. */
function canvasBgSlots(utils: ReturnType<typeof render>): HTMLElement {
  // Found by its own heading, NOT by position. "the last .ts-avatar-slots"
  // reads correctly today, but the moment anyone adds an image group after
  // this one the NEGATIVE assertion below ("no lay-down mode until there is
  // an image") starts querying the wrong group and goes silently tautological
  // — it would pass by looking in a place the field was never going to be.
  const group = Array.from(
    utils.container.querySelectorAll(".ts-avatar-slots")
  ).find(
    (g) => g.querySelector(".ts-avatar-label")?.textContent === s.themeCanvasBg
  );
  if (!group)
    throw new Error(`no .ts-avatar-slots headed ${s.themeCanvasBg}`);
  return group as HTMLElement;
}

/**
 * The 用詞 search box. This one is fetched structurally rather than by label,
 * because the only container that holds it IS the 866-row list — there is no
 * smaller scope to narrow to, so a label query here is exactly the O(N²) case.
 * Its accessible name is therefore ASSERTED instead of being used to find it:
 * the binding keeps a witness, at O(1) instead of 16 seconds.
 */
function wordingSearch(utils: ReturnType<typeof render>): HTMLElement {
  const el = utils.container.querySelector(".ts-wording-search");
  if (!el) throw new Error("no .ts-wording-search in the edit view");
  expect(el.getAttribute("aria-label")).toBe(s.themeWordingSearch);
  return el as HTMLElement;
}

/** 儲存 / 取消 — two buttons, so a role query over them is cheap. */
function formActions(utils: ReturnType<typeof render>): HTMLElement {
  const el = utils.container.querySelector(".ts-form-actions");
  if (!el) throw new Error("no .ts-form-actions in the edit view");
  return el as HTMLElement;
}

// ── 🔴 CLICK FIRST, THEN await settle() — NEVER fireEvent INSIDE act ────────
//
// Every theme action is a real request now (T-83ef), and the provider's
// handlers re-read `themeRef.current` AFTER their await — that is the check
// that stops a slow response repainting a theme the owner already left, and
// the same identity check `bundleFor` makes.
//
// Wrapping the click in `await act(async () => { fireEvent.click(x); await
// tick; })` puts React's re-render in act's queue while the mock's promise
// resolves on the very next microtask — so the callback reads a ref from BEFORE
// the click and takes the "you already moved on" branch. The whole action then
// silently does nothing, and a test written that way asserts against the state
// the click was supposed to change.
//
// `fireEvent` on its own is already act-wrapped by RTL and flushes React
// synchronously, so clicking first and settling after reproduces the real
// browser order (render, then response) instead of inverting it.

/** Let every pending request settle and React flush what they produced. */
async function settle() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

/** 儲存 — AWAITED (T-83ef). The editor's save goes through
 * `await saveTheme(bundle)`, i.e. one real PUT per theme, so "the click
 * happened" and "the write landed" are no longer the same instant. */
async function clickSave(utils: ReturnType<typeof render>) {
  fireEvent.click(within(formActions(utils)).getByRole("button", { name: p.save }));
  await settle();
}

const p = zh.profile;
const s = zh.settings;

// ── reading the server back (T-83ef) ────────────────────────────────────────
// Themes LEFT settings.custom_themes and became their own resource, so the
// question "did the theme really land on the server?" is asked of /api/themes
// now. The two doors are deliberately separate here as well: the LIST answers
// id + name only (never colours — a helper that returned bundles from the list
// would let a test read a field the real list row does not carry), and the
// per-theme read is the only thing that carries a bundle.

/** The ids the server actually holds, in its own order. */
async function savedIds(): Promise<string[]> {
  return (await api.listThemes()).map((x) => x.id);
}

/** ONE saved bundle IN FULL — colours, wording, images. */
function savedTheme(id: string): Promise<ThemeBundle> {
  return api.getTheme(id);
}

/** Open a listed theme's editor. ASYNC on purpose: 編輯 no longer has the
 * bundle in hand — it fetches it (or takes the active one) before the edit view
 * can exist at all, so the click and the form are two different ticks. */
async function openEditor(
  utils: ReturnType<typeof render>,
  name: string
): Promise<void> {
  fireEvent.click(await utils.findByLabelText(`${p.themeEdit} ${name}`));
  await settle();
}

// Let the provider's mount reconcile (getServerSettings) settle BEFORE we touch
// the custom-theme set — otherwise its late-resolving GET overwrites an import
// with the (still-empty) server value.
async function renderManage() {
  let utils!: ReturnType<typeof render>;
  await act(async () => {
    utils = render(
      <I18nProvider>
        <ThemeSettings crumbs={[{ label: zh.settings.title }]} />
      </I18nProvider>
    );
    await new Promise((r) => setTimeout(r, 0));
  });
  return utils;
}

beforeEach(() => {
  __resetMock();
  clearToken();
  // Several tests below stand a seam up on purpose (a rejecting putTheme, a
  // getTheme that never resolves). A spy that outlived its test would make the
  // NEXT one green or red for a reason that is not in it.
  vi.restoreAllMocks();
  localStorage.removeItem("oc.theme");
  document.documentElement.removeAttribute("style");
  delete document.documentElement.dataset.theme;
});

async function importBundle(
  utils: Awaited<ReturnType<typeof renderManage>>,
  bundle: unknown
) {
  fireEvent.click(utils.getByText(p.themeImport));
  fireEvent.change(utils.getByLabelText(p.themeImportTitle), {
    target: { value: JSON.stringify(bundle) },
  });
  // Awaited: the import is a real per-theme PUT now, so the list it lands on
  // is a tick away from the click.
  fireEvent.click(utils.getByText(p.themeConfirmImport));
  await settle();
}

describe("ThemeSettings · import", () => {
  it("imports a pasted bundle, lists it, and lands it on the server", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    expect(await utils.findByText("午夜藍")).toBeTruthy();
    expect(await savedIds()).toContain("midnight");

    // WHICH KIND a theme is comes from the group it sits in — the rows
    // themselves carry no 內建/自訂 chip (the heading already says it).
    expect(utils.container.querySelectorAll(".ts-tag").length).toBe(0);
    const rows = Array.from(utils.container.querySelectorAll(".ts-row"));
    const rowOf = (name: string) =>
      rows.find((r) => r.textContent?.includes(name));
    const headOf = (name: string) =>
      rowOf(name)?.closest(".ts-list")?.querySelector(".ts-group-head")
        ?.textContent;
    expect(headOf(zh.themeIdentity.office)).toBe(zh.themeMarkers.builtinGroup);
    expect(headOf("午夜藍")).toBe(zh.themeMarkers.customGroup);
  });

  it("puts the built-in and the custom rows in separate labelled groups", async () => {
    // Round 4 (BLOCKER-A): the row CHIP alone was forgeable — a pack controlled
    // its text (settings.themeBuiltinTag was overridable), its colour
    // (--color-seg-fill / --color-icon-violet-bg) and the row's own name, so two
    // identical 「辦公室 [內建]」 rows could be produced. The grouping is what a
    // theme cannot reach: it is STRUCTURE. Round 8 handed the heading text back
    // to packs (themeMarkers.* is overridable wording again) and round 7 removed
    // the quick picker's mirroring <optgroup>, so what this pins is the render —
    // which group a row lands in — and nothing else.
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    await utils.findByText("午夜藍");

    const groupOf = (name: string) => {
      const row = Array.from(utils.container.querySelectorAll(".ts-row")).find(
        (r) => r.textContent?.includes(name)
      );
      const list = row?.closest(".ts-list");
      const head = list?.querySelector(".ts-group-head");
      return { head: head?.textContent, labelled: list?.getAttribute("aria-labelledby"), id: head?.id };
    };

    const builtin = groupOf(zh.themeIdentity.office);
    expect(builtin.head).toBe(zh.themeMarkers.builtinGroup);
    expect(builtin.labelled).toBe(builtin.id);

    const custom = groupOf("午夜藍");
    expect(custom.head).toBe(zh.themeMarkers.customGroup);
    expect(custom.labelled).toBe(custom.id);

    // Two DIFFERENT groups — a custom row never lands under the built-in
    // heading, whatever it is called.
    expect(custom.id).not.toBe(builtin.id);
  });

  it("keeps the built-in row's own name when a pack forges everything else", async () => {
    // The round-3/4 recipe, re-pointed at what round 8 left standing. A pack may
    // now re-word the 內建 / 自訂 labels, re-value every colour the headings read,
    // and call ITSELF 辦公室 — the owner does not want any of that policed
    // (「這是大家自己用的,自己要怎麼搞我們不用特別管」). The ONE thing it must not
    // reach is the BUILT-IN theme's own name: that was the original bug — a
    // 「精靈村」 pack renamed the shipped theme too and there was no way back to it.
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "forge",
      name: zh.themeIdentity.office,
      colors: { "--color-text-muted": "#8b7ae8" },
      // EVERY overridable message code re-valued to a sentinel, plus a direct
      // shot at the theme-identity subtree. If the built-in row reads any key a
      // `wording` overlay can reach, the sentinel shows up on screen.
      wording: {
        zh: {
          ...Object.fromEntries(MESSAGE_KEYS.map((k) => [k, SENTINEL])),
          "themeIdentity.office": SENTINEL,
        },
      },
    });
    const rowsNamedOffice = () =>
      Array.from(utils.container.querySelectorAll(".ts-row")).filter((r) =>
        r.textContent?.includes(zh.themeIdentity.office)
      );
    // Two rows called 辦公室 is now a legal state, and the import went through.
    expect(rowsNamedOffice().length).toBe(2);

    // Make the forging pack the ACTIVE theme, so its wording overlay is live.
    fireEvent.click(rowsNamedOffice()[1].querySelector("button.ts-pick")!);
    await settle();
    // The overlay really IS live — without this the assertion below would pass
    // on a page where no wording was applied at all.
    expect(utils.getByTestId("ts-group-builtin").textContent).toBe(SENTINEL);

    // …and yet the built-in row STILL says 辦公室 — the overlay never reached
    // themeIdentity, so the shipped theme is still findable and still
    // selectable, which is the whole of the guarantee that remains.
    const builtinList = utils.getByTestId("ts-group-builtin").closest(".ts-list");
    const builtinRows = Array.from(builtinList?.querySelectorAll(".ts-row") ?? []);
    expect(builtinRows.length).toBe(1);
    expect(builtinRows[0].textContent).toContain(zh.themeIdentity.office);
    expect(builtinRows[0].textContent).not.toContain(SENTINEL);
  });

  it("paints the group headings with a pack-settable colour token", async () => {
    // Round 4 pointed .ts-group-head at a reserved --color-marker-* slot so a
    // pack could not hide the headings. The slot was valued for the built-in
    // DARK theme, so under a light pack the headings measured 1.98:1 and were
    // near-invisible for everyone — the guard cost more than it bought. Round 8
    // sends the colour back through the ordinary theme slot the rest of the
    // muted text uses, and drops the reserved family entirely.
    const css = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), "theme-settings.css"),
      "utf8"
    );
    const at = css.indexOf(".ts-group-head {");
    expect(at).toBeGreaterThan(-1);
    const block = css.slice(at, css.indexOf("}", at) + 1);
    expect(block, block).toContain("var(--color-text-muted)");
    // Every var() it reads is a token a bundle may re-value…
    const read = (block.match(/var\(\s*(--[\w-]+)/g) ?? []).map((v) =>
      v.replace(/var\(\s*/, "")
    );
    expect(read.length).toBeGreaterThan(0);
    for (const token of read) {
      expect(THEME_COLOR_TOKENS, token).toContain(token);
    }
    // …and the reserved family is gone from the stylesheet and the whitelist
    // alike, so nothing is left half-removed.
    expect(css).not.toContain("--color-marker-");
    for (const token of THEME_COLOR_TOKENS) {
      expect(token.startsWith("--color-marker-")).toBe(false);
    }
  });


  it("imports a pack with unrecognised wording codes and warns which were skipped", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "elfvillage",
      name: "精靈村",
      colors: { "--color-accent": "#0b1020" },
      wording: {
        zh: { "nav.tasks": "任務榜", "profile.themeOffice": "精靈村", "typo.not.a.key": "x" },
      },
    });
    // The import SUCCEEDED — the pack is listed and landed on the server.
    expect(await utils.findByText("精靈村")).toBeTruthy();
    expect(await savedIds()).toContain("elfvillage");
    // …and the recognised override survived while the unknown ones did not.
    expect((await savedTheme("elfvillage")).wording).toEqual({
      zh: { "nav.tasks": "任務榜" },
    });
    // …and the drop is named on screen instead of being silent.
    expect(utils.getByTestId("theme-import-skipped").textContent).toBe(
      makeMessages(zh, "zh").themeImportSkipped(2, [
        "profile.themeOffice",
        "typo.not.a.key",
      ])
    );
  });

  it("names only the first few skipped codes and lets the count carry the rest", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    const junk: Record<string, string> = {};
    for (let i = 1; i <= 30; i++) junk[`junk.key.${i}`] = "x";
    await importBundle(utils, {
      id: "noisy",
      name: "吵雜",
      colors: { "--color-accent": "#0b1020" },
      wording: { zh: junk },
    });
    expect(await utils.findByText("吵雜")).toBeTruthy();
    expect(utils.getByTestId("theme-import-skipped").textContent).toBe(
      makeMessages(zh, "zh").themeImportSkipped(30, [
        "junk.key.1",
        "junk.key.2",
        "junk.key.3",
      ])
    );
  });

  it("blocks an injection-shaped bundle inline and never reaches the server", async () => {
    const utils = await renderManage();
    await importBundle(utils, {
      id: "evil",
      name: "Evil",
      colors: { "--color-bg": "red; } body { background: url(x)" },
    });
    expect(utils.getByLabelText(p.themeImportTitle)).toBeTruthy();
    expect(utils.container.querySelector(".set-error")).toBeTruthy();
    expect(await savedIds()).toEqual([]);
  });
});

describe("ThemeSettings · colour editing", () => {
  it("shows friendly names grouped by purpose — never the raw --color-* token", async () => {
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020", "--color-bg": "#040506" },
    });
    await openEditor(utils, "午夜藍");

    // The friendly group + label are shown; the raw token is not visible text.
    const colorSection = utils.container.querySelector(".ts-color-group__label");
    expect(colorSection?.textContent).toBe("主色"); // brand group heading
    expect(utils.getAllByText("主色").length).toBeGreaterThan(0); // group + accent label
    expect(utils.queryByText("--color-accent")).toBeNull();
  });

  it("round-trips an edited colour value through save", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    await openEditor(utils, "午夜藍");
    // The value text field carries the friendly label as its accessible name.
    fireEvent.change(within(colourRow(utils, "主色")).getByLabelText("主色"), {
      target: { value: "#ffffff" },
    });
    await clickSave(utils);

    const b = await savedTheme("midnight");
    expect(b?.colors["--color-accent"]).toBe("#ffffff");
  });
});

describe("ThemeSettings · wording overlay", () => {
  it("stores a wording override and lands it on the server bundle", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    await openEditor(utils, "午夜藍");

    // Narrow the (large) wording list to exactly one code by searching the code.
    fireEvent.change(wordingSearch(utils), {
      target: { value: "common.apply" },
    });
    const list = utils.container.querySelector(
      ".ts-wording-list"
    ) as HTMLElement;
    const input = within(list).getByRole("textbox");
    fireEvent.change(input, { target: { value: "套用替代" } });
    await clickSave(utils);

    const b = await savedTheme("midnight");
    expect(b?.wording?.zh?.["common.apply"]).toBe("套用替代");
  });

  it("keeps the boundary spaces of a sentence-fragment override", async () => {
    // Several codes T-081b made overridable are sentence FRAGMENTS whose
    // leading/trailing space is load-bearing: uninstallWarnBody2 is 「」上還有 」
    // and Body3 opens with a space, so the composed sentence reads
    // 「Alpha」上還有 3 位成員…. Trimming what the owner typed would make the
    // product's own editor render 「上還有3位成員」 — the editor corrupting the
    // very strings the ticket just opened up.
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    await openEditor(utils, "午夜藍");

    fireEvent.change(wordingSearch(utils), {
      target: { value: "uninstallWarnBody2" },
    });
    const list = utils.container.querySelector(
      ".ts-wording-list"
    ) as HTMLElement;
    fireEvent.change(within(list).getByRole("textbox"), {
      target: { value: "」上頭還有 " },
    });
    await clickSave(utils);

    const b = await savedTheme("midnight");
    const stored = b?.wording?.zh?.["monitor.machine.uninstallWarnBody2"];
    expect(stored).toBe("」上頭還有 ");

    // …and the fragment composes into a sentence that still has its spaces.
    const themed = { ...zh, monitor: { ...zh.monitor, machine: { ...zh.monitor.machine, uninstallWarnBody2: stored! } } };
    expect(makeMessages(themed, "zh").machineUninstallWarnBody("Alpha", 3)).toBe(
      "「Alpha」上頭還有 3 位成員在線上。現在解除安裝會在成員仍在這台機器上時把 warden 拆除 —— 建議先將相關成員下線。仍要繼續嗎?"
    );
  });
});

// The 用詞 list renders EVERY overridable code — all 866 of them, all in the
// document at once. It was briefly virtualised (T-8115) and the owner reverted
// that (2026-08-02): the editor is opened rarely and themes usually arrive by
// import, so the measured open cost was judged not worth the three capabilities
// windowing took away (keyboard/AT order, the browser's own find, whole-page
// select-all and print).
//
// These tests assert PRESENCE, not "row X is absent" — the invariant is "not one
// code may be missing". The one that pins keyboard/AT order lives here too, but
// note what each layer can and cannot see: jsdom can read DOM order and what is
// in the document, and it cannot press a real Tab. The browser half is in
// visual-guards/wording-list-full.ct.spec.tsx.
describe("ThemeSettings · wording list is browsable in full", () => {
  async function openWordingEditor() {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    await openEditor(utils, "午夜藍");
    const list = utils.container.querySelector(
      ".ts-wording-list"
    ) as HTMLElement;
    return { utils, list };
  }

  const codesIn = (list: HTMLElement) =>
    Array.from(list.querySelectorAll("[data-wording-code]")).map((r) =>
      r.getAttribute("data-wording-code")
    );

  // The scroll offset the virtualised list used to move its window well past
  // row 0. It is kept only so the "scrolling changes nothing" test scrolls to a
  // place that USED to matter; 48px is the row pitch that implementation
  // measured in Chromium.
  const ROW_PITCH_PX = 48;

  it("has every one of the 866 codes in the document, with no search at all", async () => {
    const { utils, list } = await openWordingEditor();

    // Nothing is filtered out before the owner has typed anything.
    expect(list.getAttribute("data-wording-total")).toBe(
      String(MESSAGE_KEYS.length)
    );
    // The list opens at the top of the code set…
    expect(codesIn(list)).toContain(MESSAGE_KEYS[0]);
    // …and a screen reader is told how long the list really is, not how many
    // rows happen to be mounted right now.
    const firstRow = list.querySelector(
      `[data-wording-code="${MESSAGE_KEYS[0]}"]`
    ) as HTMLElement;
    expect(firstRow.getAttribute("aria-setsize")).toBe(
      String(MESSAGE_KEYS.length)
    );
    expect(firstRow.getAttribute("aria-posinset")).toBe("1");
    // …and every single code is in the document, without scrolling anywhere.
    // No scroll walk: with nothing unmounted there is no window to advance, and
    // a walk would only re-measure the same set N times. The invariant is the
    // same one the walk used to establish — not one code missing — so a cap of
    // any size (v1's slice(0, 30), a 400-row window, anything) still reds it.
    const seen = new Set(codesIn(list));
    const missed = MESSAGE_KEYS.filter((c) => !seen.has(c));
    expect(missed).toEqual([]);
    expect(seen.size).toBe(MESSAGE_KEYS.length);

    // The last code is not a read-only tail: it is the same editable input as
    // any other row, and what is typed into it lands on the saved bundle.
    const last = MESSAGE_KEYS[MESSAGE_KEYS.length - 1];
    const row = list.querySelector(
      `[data-wording-code="${last}"]`
    ) as HTMLElement;
    fireEvent.change(within(row).getByRole("textbox"), {
      target: { value: "末列也能改" },
    });
    await clickSave(utils);
    const b = await savedTheme("midnight");
    expect(b?.wording?.zh?.[last]).toBe("末列也能改");
  });

  it("shows ALL of a search's matches, not a first-N slice of them", async () => {
    const { list } = await openWordingEditor();

    // "task" matches well over a hundred codes — the case where a display cap
    // would quietly swallow the tail and tell the owner to search harder.
    act(() => {
      fireEvent.change(
        (list.parentElement as HTMLElement).querySelector(
          ".ts-wording-search"
        ) as HTMLElement,
        { target: { value: "task" } }
      );
    });
    const matchedByCode = MESSAGE_KEYS.filter((c) =>
      c.toLowerCase().includes("task")
    );
    expect(matchedByCode.length).toBeGreaterThan(100);
    // The panel also matches the English/current text, so its result set is a
    // superset of the codes that merely contain the word.
    const total = Number(list.getAttribute("data-wording-total"));
    expect(total).toBeGreaterThanOrEqual(matchedByCode.length);

    // Every match is in the document — including the ones past the first
    // screenful, which is exactly where a cap hides the tail.
    const seen = new Set(codesIn(list));
    expect(matchedByCode.filter((c) => !seen.has(c))).toEqual([]);
    expect(seen.size).toBe(total);
  });

  it("does not move a row out from under the cursor when you start typing in it", async () => {
    // Regression guard: an earlier attempt at this panel ordered overridden
    // codes first, so the first keystroke in row N teleported that row to the
    // top — away from the caret, and possibly off screen. Row order is the
    // code order, and typing an override is not allowed to disturb it.
    //
    // ⚠️ This one has NO discriminating power over the windowing change: the
    // pre-windowing component listed rows in MESSAGE_KEYS order too, with no
    // reordering logic anywhere, so it was already true before the diff that
    // introduced it. It is a forward guard against re-introducing that v1
    // ordering, plus one live assertion about THIS component
    // (`scrollTop` stays 1200 — red if anyone wires resetWordingScroll into
    // setWordingAt). Do not count it among the tests that hold the windowing
    // itself up; those are the two reachability tests above.
    const { list } = await openWordingEditor();

    act(() => {
      list.scrollTop = 1200;
      fireEvent.scroll(list);
    });
    const before = codesIn(list);
    const target = before[5]!;
    const rowOf = (code: string) =>
      list.querySelector(`[data-wording-code="${code}"]`) as HTMLElement;
    const inputOf = (code: string) =>
      within(rowOf(code)).getByRole("textbox") as HTMLInputElement;

    fireEvent.change(inputOf(target), { target: { value: "甲" } });
    expect(codesIn(list)).toEqual(before);
    expect(inputOf(target).value).toBe("甲");

    // …and it stays put as the override grows, too.
    fireEvent.change(inputOf(target), { target: { value: "甲乙" } });
    expect(codesIn(list)).toEqual(before);
    expect(list.scrollTop).toBe(1200);
    expect(inputOf(target).value).toBe("甲乙");
  });

  it("keeps the whole set — and its reading order — after the list scrolls away", async () => {
    // The regression this replaces a test for: while the list was virtualised it
    // had to keep the FOCUSED row mounted (unmounting the element focus lives in
    // hands focus to <body> and the caret is gone), and that pinned row was
    // rendered after the window — so sequential DOM order, which both Tab and a
    // screen reader's virtual cursor read, ran …865, 866, 1 and Tab out of the
    // row left the list entirely.
    //
    // With nothing unmounted there is no pin and no reordering. This asserts the
    // two halves jsdom can actually see: the set does not shrink when the list
    // is scrolled, and DOM order is monotonic. Pressing a real Tab is the
    // browser half — visual-guards/wording-list-full.ct.spec.tsx.
    const { list } = await openWordingEditor();

    const code = codesIn(list)[0]!;
    const input = within(
      list.querySelector(`[data-wording-code="${code}"]`) as HTMLElement
    ).getByRole("textbox") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "我的未存編輯" } });
    act(() => {
      input.focus();
    });
    expect(document.activeElement).toBe(input);

    // Scroll far past it — the offset that used to move the window off row 0.
    act(() => {
      list.scrollTop = 300 * ROW_PITCH_PX;
      fireEvent.scroll(list);
    });

    // Not one code left the document, and the focused row is the same element
    // (so the caret cannot have been handed back to <body>).
    expect(codesIn(list).length).toBe(MESSAGE_KEYS.length);
    expect(codesIn(list)).toContain(MESSAGE_KEYS[1]);
    expect(document.activeElement).toBe(input);
    expect(input.isConnected).toBe(true);
    expect(input.value).toBe("我的未存編輯");

    // Reading order is the code order, start to end, with no step backwards.
    const positions = Array.from(
      list.querySelectorAll("[data-wording-code]")
    ).map((r) => Number(r.getAttribute("aria-posinset")));
    expect(positions[0]).toBe(1);
    expect(positions[positions.length - 1]).toBe(MESSAGE_KEYS.length);
    expect(
      positions.filter((pos, i) => i > 0 && pos < positions[i - 1]),
      "reading order must not step backwards"
    ).toEqual([]);
    // …and no row is taken out of flow to achieve any of it.
    expect(list.querySelectorAll(".ts-wording-row--pinned").length).toBe(0);
    expect(list.querySelectorAll(".ts-wording-pad").length).toBe(0);
  });
});

describe("ThemeSettings · alias-default colours", () => {
  it("offers the zone/split tokens a bundle never carries, and saves only the touched one", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    await openEditor(utils, "午夜藍");

    // The content-area background follows --color-bg, so no bundle ever exports
    // it — yet it must be reachable here, or only a hand-edited JSON can set it.
    const mainBgLabel = tokenMeta("--color-main-bg", "zh").label;
    const mainBg = within(colourRow(utils, mainBgLabel)).getByLabelText(
      mainBgLabel
    );
    expect((mainBg as HTMLInputElement).value).toBe("");
    fireEvent.change(mainBg, { target: { value: "#12345680" } });
    await clickSave(utils);

    const b = await savedTheme("midnight");
    expect(b?.colors["--color-main-bg"]).toBe("#12345680");
    // …and the ones left alone stay ABSENT rather than baked to a literal —
    // that is what keeps them following their parent.
    expect("--color-nav-bg" in (b?.colors ?? {})).toBe(false);
    expect("--color-knob" in (b?.colors ?? {})).toBe(false);
  });

  it("edits opacity through a slider, not only through hand-typed #RRGGBBAA", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-card": "#242832" },
    });
    await openEditor(utils, "午夜藍");

    const label = tokenMeta("--color-card", "zh").label;
    const slider = within(colourRow(utils, label)).getByLabelText(
      `${label} ${s.themeColorOpacity}`
    );
    expect((slider as HTMLInputElement).value).toBe("100");
    fireEvent.change(slider, { target: { value: "40" } });
    await clickSave(utils);

    const b = await savedTheme("midnight");
    expect(b?.colors["--color-card"]).toBe("#24283266");
  });
});

describe("ThemeSettings · outer-canvas background", () => {
  const png =
    "data:image/png;base64," +
    btoa(
      String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01)
    );

  it("stores a non-default lay-down mode and drops it again on tile", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
      backgrounds: { canvas: png },
    });
    await openEditor(utils, "午夜藍");

    const mode = within(canvasBgSlots(utils)).getByLabelText(s.themeCanvasBgMode);
    fireEvent.change(mode, { target: { value: "sides" } });
    await clickSave(utils);

    let b = await savedTheme("midnight");
    expect(b?.backgroundModes).toEqual({ canvas: "sides" });

    // Back to the default and the field disappears entirely — a tiling theme
    // stays byte-identical to one authored before the field existed.
    await openEditor(utils, "午夜藍");
    fireEvent.change(within(canvasBgSlots(utils)).getByLabelText(s.themeCanvasBgMode), {
      target: { value: "tile" },
    });
    await clickSave(utils);

    b = await savedTheme("midnight");
    expect(b?.backgroundModes).toBeUndefined();
    expect(b?.backgrounds).toEqual({ canvas: png });
  });

  it("offers no lay-down mode until there is an image to lay down", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "plain",
      name: "純色",
      colors: { "--color-accent": "#0b1020" },
    });
    await openEditor(utils, "純色");

    expect(within(canvasBgSlots(utils)).queryByLabelText(s.themeCanvasBgMode)).toBeNull();
  });

  // T-72da. readValidatedImage is shared by FOUR file pickers, and before this
  // it hard-coded the avatar gate. Relaxing only the server would have produced
  // the worst possible shape: the server accepts the owner's background, and the
  // cockpit still refuses it at the picker with "invalid image" — so the owner
  // never gets far enough to find out the server changed its mind.
  //
  // The two assertions are ONE test on purpose: "the background is accepted"
  // alone would also pass if the gate had been removed altogether, and "the
  // avatar is refused" alone was already true before T-72da. Only the PAIR shows
  // the split.
  it("takes a background the avatar gate would refuse — and still refuses it as an avatar", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    await openEditor(utils, "午夜藍");

    // A real PNG signature + padding: past the 64 KiB avatar cap, inside the
    // 512 KiB background cap. Only the SIZE distinguishes these two outcomes —
    // the mime and magic bytes are valid either way, so neither assertion can
    // pass for some unrelated reason.
    const bytes = new Uint8Array(MAX_AVATAR_BYTES + 1);
    bytes.set([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
    const bigPng = new File([bytes], "wallpaper.png", { type: "image/png" });

    const bgInput = canvasBgSlots(utils).querySelector(".ts-file")!;
    fireEvent.change(bgInput, { target: { files: [bigPng] } });
    // FileReader.onload is a MACROtask in jsdom, so awaiting microtasks (a bare
    // act) is not enough — wait for the preview the accepted image produces.
    await waitFor(() =>
      expect(canvasBgSlots(utils).querySelector("img.avatar__img")).not.toBeNull()
    );
    // Accepted: no error, and the picked image is now the canvas background.
    expect(
      canvasBgSlots(utils).parentElement?.querySelector(".set-error")
    ).toBeNull();
    await clickSave(utils);
    const saved = await savedTheme("midnight");
    expect(saved?.backgrounds?.canvas?.startsWith("data:image/png;base64,")).toBe(
      true
    );

    // The SAME file as an avatar is still refused — the relaxation did not leak
    // across, which is the whole point of splitting the caps.
    await openEditor(utils, "午夜藍");
    const avatarInput = imageSlots(utils, s.themeAvatarMember).querySelector(
      ".ts-file"
    )!;
    fireEvent.change(avatarInput, { target: { files: [bigPng] } });
    await waitFor(() =>
      expect(
        imageSlots(utils, s.themeAvatarMember).parentElement?.querySelector(
          ".set-error"
        )?.textContent
      ).toBe(s.themeAvatarInvalid)
    );
  });
});

describe("ThemeSettings · delete", () => {
  it("deletes a custom theme via the confirm modal", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeDelete} 午夜藍`));
    fireEvent.click(utils.getByTestId("theme-delete-confirm-btn"));
    await settle();

    expect(utils.queryByText("午夜藍")).toBeNull();
    expect(await savedIds()).toEqual([]);
  });

  it("switches the cockpit back to the built-in when the ACTIVE theme is deleted", async () => {
    // The claim is unchanged — you must never be left looking at a theme that
    // no longer exists — but the DOOR moved: this screen used to compute the
    // fallback itself (`theme === id ? "office" : undefined`). It does not any
    // more; `removeTheme` is told by the server that it reset display_theme and
    // the provider switches. So the assertion is about the RESULT (the built-in
    // row is the selected one again), which is true of either implementation
    // and stays true of the one we actually ship.
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    await utils.findByText("午夜藍");

    // Make it the ACTIVE theme — that is the case with something to get wrong.
    fireEvent.click(utils.getByText("午夜藍"));
    await settle();
    const activePicks = () =>
      Array.from(utils.container.querySelectorAll(".ts-pick--active")).map(
        (b) => b.textContent
      );
    expect(activePicks()).toEqual(["午夜藍"]);

    fireEvent.click(await utils.findByLabelText(`${p.themeDelete} 午夜藍`));
    fireEvent.click(utils.getByTestId("theme-delete-confirm-btn"));
    await settle();

    // Gone from the list AND from the server…
    expect(utils.queryByText("午夜藍")).toBeNull();
    expect(await savedIds()).toEqual([]);
    // …and the built-in is what the cockpit is showing, not a dangling id.
    expect(activePicks()).toEqual([zh.themeIdentity.office]);
    expect(document.documentElement.dataset.theme).toBe("office");
  });
});

describe("ThemeSettings · export", () => {
  // jsdom's URL has no object-URL helpers; downloadBundle needs them, so provide
  // stubs and clean them up.
  afterEach(() => {
    vi.restoreAllMocks();
    delete (URL as { createObjectURL?: unknown }).createObjectURL;
    delete (URL as { revokeObjectURL?: unknown }).revokeObjectURL;
  });

  it("has no toolbar 匯出 button — export is per-row download only", async () => {
    const utils = await renderManage();
    // The toolbar keeps 新增 + 匯入; the standalone 匯出 button is gone.
    expect(utils.getByText(p.themeAdd)).toBeTruthy();
    expect(utils.getByText(p.themeImport)).toBeTruthy();
    expect(utils.queryByText(p.themeExport)).toBeNull();
  });

  it("office 列下載鈕可用,下載一個非保留 id 的 office 包(可再匯入)", async () => {
    const utils = await renderManage();
    const btn = utils.getByLabelText(
      `${p.themeExport} ${zh.themeIdentity.office}`
    ) as HTMLButtonElement;
    expect(btn.disabled).toBe(false);

    const createFn = vi.fn().mockReturnValue("blob:office");
    (URL as { createObjectURL: unknown }).createObjectURL = createFn;
    (URL as { revokeObjectURL: unknown }).revokeObjectURL = vi.fn();
    let downloadName = "";
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
      this: HTMLAnchorElement
    ) {
      downloadName = this.download;
    });

    fireEvent.click(btn);

    expect(createFn).toHaveBeenCalledTimes(1);
    // The download uses id "office-base", NOT the reserved built-in "office"
    // (which validateThemeBundle rejects), so the bundle re-imports.
    expect(downloadName).toBe("officraft-theme-office-base.json");
    const text = await new Promise<string>((resolve) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.readAsText(createFn.mock.calls[0][0] as Blob);
    });
    const payload = JSON.parse(text);
    expect(payload.id).toBe("office-base");
    // …under the BUILT-IN's own name, with nothing appended. Round 10 removed
    // the 「(副本)」 tag (owner: 「我覺得檔名不用附註副本」), and with it the only
    // pack-settable string that reached this name: themeMarkers.copyTag was
    // ordinary overridable wording, so a pack could stretch it until this very
    // download produced a file the product refused to import back (review round
    // 9, SHOULD-2). The name now comes wholly from themeIdentity, which the
    // wording whitelist excludes — pinned from the pack's side by
    // 「keeps the built-in row's own name when a pack forges everything else」.
    expect(payload.name).toBe(zh.themeIdentity.office);
    // (that this name actually re-imports is pinned in themeExport.test.ts —
    // jsdom has no stylesheet, so the payload here carries no colours to import)
  });
});

// ── The list is id + name; the bundle is a REQUEST (T-83ef) ─────────────────
//
// 匯出 and 編輯 used to read `colors` straight off the set the provider held.
// The set is one line per theme now, so both actions have to GO AND GET the
// bundle — and that is a new failure surface with three distinct things to get
// wrong: fetching the wrong theme, not fetching at all, and failing silently.
describe("ThemeSettings · fetching a bundle to edit or export", () => {
  /** Seed a theme straight through the server door, the way one that was
   * imported in an earlier session exists at login: no UI, no import view. */
  async function seed(bundle: ThemeBundle) {
    await api.putTheme(bundle);
  }

  const AURORA: ThemeBundle = {
    id: "aurora",
    name: "極光",
    colors: { "--color-accent": "#aa0011" },
  };
  const MIDNIGHT: ThemeBundle = {
    id: "midnight",
    name: "午夜藍",
    colors: { "--color-accent": "#bb0022" },
  };

  const accentField = (utils: ReturnType<typeof render>) =>
    within(colourRow(utils, tokenMeta("--color-accent", "zh").label)).getByLabelText(
      tokenMeta("--color-accent", "zh").label
    ) as HTMLInputElement;

  const editNameField = (utils: ReturnType<typeof render>) =>
    utils.container.querySelector("#ts-edit-name") as HTMLInputElement;

  /** Make `id` the ACTIVE theme through the row's own picker (the seam
   * production uses), and wait for its bundle to arrive. */
  async function activate(utils: ReturnType<typeof render>, name: string) {
    fireEvent.click(await utils.findByText(name));
    await settle();
  }

  it("fetches the bundle of a theme that is NOT the active one before editing it", async () => {
    setToken("owner-token");
    await seed(AURORA);
    await seed(MIDNIGHT);
    const utils = await renderManage();
    await utils.findByText("極光");
    await activate(utils, "極光");

    const spy = vi.spyOn(api, "getTheme");
    await openEditor(utils, "午夜藍");

    // It really went and got THAT theme — not the one already in hand.
    expect(spy.mock.calls.map((c) => c[0])).toEqual(["midnight"]);
    expect(editNameField(utils).value).toBe("午夜藍");
    expect(accentField(utils).value).toBe("#bb0022");
    spy.mockRestore();
  });

  it("🔴 never edits one theme's colours under another theme's name", async () => {
    // THE DANGEROUS FAILURE of the whole design. `bundleFor` short-circuits to
    // `activeThemeBundle` — a whole request saved on the row most likely to be
    // edited — and that is sound ONLY under `activeThemeBundle.id === id`. Drop
    // the id check and every screen still looks right: the editor opens, the
    // form fills, the save succeeds. What lands on the server is the ACTIVE
    // theme's colours written under the OTHER theme's name, i.e. silent data
    // loss on a theme the owner never even opened.
    //
    // So: 極光 is active (#aa0011), 午夜藍 is the one being edited (#bb0022).
    // The two are distinguishable in the form AND on the server, and the test
    // reads both — the form alone would still pass if the save wrote the wrong
    // bundle, and the server alone would still pass if the form showed the
    // wrong colours and the owner overwrote them by hand.
    setToken("owner-token");
    await seed(AURORA);
    await seed(MIDNIGHT);
    const utils = await renderManage();
    await utils.findByText("極光");
    await activate(utils, "極光");

    await openEditor(utils, "午夜藍");
    expect(editNameField(utils).value).toBe("午夜藍");
    expect(accentField(utils).value).toBe("#bb0022");

    await clickSave(utils);

    // Neither theme took the other's colour.
    expect((await savedTheme("midnight")).colors["--color-accent"]).toBe("#bb0022");
    expect((await savedTheme("aurora")).colors["--color-accent"]).toBe("#aa0011");
  });

  it("serves the ACTIVE theme from the bundle already in hand, with no second request", async () => {
    // The other half of the same rule: the short-circuit must actually fire for
    // the id it IS sound for, or the test above would also pass on a component
    // that had simply deleted the optimisation.
    setToken("owner-token");
    await seed(AURORA);
    const utils = await renderManage();
    await utils.findByText("極光");
    await activate(utils, "極光");

    const spy = vi.spyOn(api, "getTheme");
    await openEditor(utils, "極光");
    expect(spy).not.toHaveBeenCalled();
    // …and it is the real thing, not an empty shell standing in for it.
    expect(editNameField(utils).value).toBe("極光");
    expect(accentField(utils).value).toBe("#aa0011");
    spy.mockRestore();
  });

  it("exports the bundle it fetched — for the row that was clicked", async () => {
    setToken("owner-token");
    await seed(AURORA);
    await seed(MIDNIGHT);
    const utils = await renderManage();
    await utils.findByText("午夜藍");
    await activate(utils, "極光");

    const createFn = vi.fn().mockReturnValue("blob:x");
    (URL as { createObjectURL: unknown }).createObjectURL = createFn;
    (URL as { revokeObjectURL: unknown }).revokeObjectURL = vi.fn();
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});

    fireEvent.click(await utils.findByLabelText(`${p.themeExport} 午夜藍`));
    await settle();

    expect(createFn).toHaveBeenCalledTimes(1);
    const text = await new Promise<string>((resolve) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.readAsText(createFn.mock.calls[0][0] as Blob);
    });
    const payload = JSON.parse(text);
    // The file is 午夜藍's, not the active 極光's — the same id check as above,
    // through the other door.
    expect(payload.id).toBe("midnight");
    expect(payload.colors["--color-accent"]).toBe("#bb0022");

    vi.restoreAllMocks();
    delete (URL as { createObjectURL?: unknown }).createObjectURL;
    delete (URL as { revokeObjectURL?: unknown }).revokeObjectURL;
  });

  it("shows the reason on the list when the bundle cannot be fetched", async () => {
    // A failure that only reached console.warn is a failure the owner cannot
    // see: the edit view simply never opens and nothing says why.
    setToken("owner-token");
    await seed(MIDNIGHT);
    const utils = await renderManage();
    await utils.findByText("午夜藍");

    vi.spyOn(api, "getTheme").mockRejectedValue(
      new ApiError("http 404", 404, codeForStatus(404), "theme not found")
    );
    await openEditor(utils, "午夜藍");

    expect(utils.container.querySelector(".set-error")?.textContent).toBe(
      "theme not found"
    );
    // …and we are still on the LIST — no half-open editor over a bundle that
    // never arrived.
    expect(utils.container.querySelector(".ts-form-actions")).toBeNull();
    expect(utils.getByText("午夜藍")).toBeTruthy();
    // 🔴 …and the row is LIVE AGAIN. `busyId` is both the in-flight signal and
    // the one-request-at-a-time lock, so a release that does not happen on the
    // failure path leaves every row's 匯出/編輯 permanently dead — the owner's
    // only way out is a reload. This is the ONLY place that claim can be made:
    // on the success path the list is gone (the editor replaced it) and there
    // is no button left to measure.
    //
    // Dropping `finally { setBusyId(null) }` does redden two OTHER cases —
    // both outer-canvas background ones, which happen to click again after a
    // fetch — but neither is about this, and their failure text points at
    // backgrounds. Being caught in passing by an unrelated case is not the
    // same as being guarded.
    expect(
      (utils.getByLabelText(`${p.themeEdit} 午夜藍`) as HTMLButtonElement)
        .disabled
    ).toBe(false);
    vi.restoreAllMocks();
  });

  it("falls back to the shared 動作失敗 line when the failure names no reason", async () => {
    setToken("owner-token");
    await seed(MIDNIGHT);
    const utils = await renderManage();
    await utils.findByText("午夜藍");

    vi.spyOn(api, "getTheme").mockRejectedValue(new Error("boom"));
    await openEditor(utils, "午夜藍");
    expect(utils.container.querySelector(".set-error")?.textContent).toBe(
      s.docActionFailed
    );
    vi.restoreAllMocks();
  });

  // NOTE the title says only what this case actually measures. Releasing the
  // lock is asserted by the FAILURE case above, because that is the only path
  // that comes back to a list with buttons on it.
  it("disables 匯出/編輯 on every row while a bundle is in flight", async () => {
    // A theme pack is hundreds of KB on the wire. "Nothing has happened yet"
    // has to be VISIBLE — and a second click must not start a second full
    // download whose answer could land out of order.
    setToken("owner-token");
    await seed(AURORA);
    await seed(MIDNIGHT);
    const utils = await renderManage();
    await utils.findByText("午夜藍");

    let release!: (b: ThemeBundle) => void;
    vi.spyOn(api, "getTheme").mockReturnValue(
      new Promise<ThemeBundle>((resolve) => {
        release = resolve;
      })
    );

    const btn = (label: string) =>
      utils.getByLabelText(label) as HTMLButtonElement;
    // Before: every custom row's actions are live.
    expect(btn(`${p.themeEdit} 午夜藍`).disabled).toBe(false);
    expect(btn(`${p.themeExport} 極光`).disabled).toBe(false);

    fireEvent.click(btn(`${p.themeEdit} 午夜藍`));

    // During: the clicked row AND the other rows are inert — the guard is one
    // request at a time, not one per row.
    expect(btn(`${p.themeEdit} 午夜藍`).disabled).toBe(true);
    expect(btn(`${p.themeExport} 午夜藍`).disabled).toBe(true);
    expect(btn(`${p.themeEdit} 極光`).disabled).toBe(true);
    expect(btn(`${p.themeExport} 極光`).disabled).toBe(true);

    release(structuredClone(MIDNIGHT));
    await settle();

    // After: the editor opened on the theme that was asked for.
    expect(editNameField(utils).value).toBe("午夜藍");
    vi.restoreAllMocks();
  });
});

// ── Claims that outlived the rewrite ───────────────────────────────────────
// The cap and the duplicate-id refusal used to be checked against the whole
// bundle set the provider held. The set is one line per theme now, so both are
// checked against `themeList` — the rule did not change, only what it counts.
describe("ThemeSettings · refusals that still stand", () => {
  async function seedMany(n: number) {
    for (let i = 0; i < n; i++) {
      await api.putTheme({
        id: `pack-${i}`,
        name: `包 ${i}`,
        colors: { "--color-accent": "#0b1020" },
      });
    }
  }

  it("refuses 新增 once the saved set is at the cap — and creates nothing", async () => {
    setToken("owner-token");
    await seedMany(MAX_CUSTOM_THEMES);
    const utils = await renderManage();
    await utils.findByText(`包 ${MAX_CUSTOM_THEMES - 1}`);

    fireEvent.click(utils.getByText(p.themeAdd));
    await settle();

    expect(utils.container.querySelector(".set-error")?.textContent).toBe(
      p.themeLimitReached
    );
    // No editor was opened over a theme that does not exist…
    expect(utils.container.querySelector(".ts-form-actions")).toBeNull();
    // …and nothing was written.
    expect((await savedIds()).length).toBe(MAX_CUSTOM_THEMES);
  });

  it("refuses an import whose id is already taken — and keeps the stored one", async () => {
    setToken("owner-token");
    await api.putTheme({
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#aa0011" },
    });
    const utils = await renderManage();
    await utils.findByText("午夜藍");

    await importBundle(utils, {
      id: "midnight",
      name: "冒名的午夜藍",
      colors: { "--color-accent": "#bb0022" },
    });

    expect(utils.container.querySelector(".set-error")?.textContent).toBe(
      p.themeImportDup
    );
    // Still in the import view holding the rejected text (nothing landed).
    expect(utils.getByLabelText(p.themeImportTitle)).toBeTruthy();
    expect(await savedIds()).toEqual(["midnight"]);
    // 🔴 and the STORED theme is untouched — a "duplicate" that silently
    // overwrote would satisfy every assertion above.
    const stored = await savedTheme("midnight");
    expect(stored.name).toBe("午夜藍");
    expect(stored.colors["--color-accent"]).toBe("#aa0011");
  });

  it("refuses an import once the saved set is at the cap", async () => {
    setToken("owner-token");
    await seedMany(MAX_CUSTOM_THEMES);
    const utils = await renderManage();
    await utils.findByText(`包 ${MAX_CUSTOM_THEMES - 1}`);

    await importBundle(utils, {
      id: "one-too-many",
      name: "多出來的",
      colors: { "--color-accent": "#0b1020" },
    });

    expect(utils.container.querySelector(".set-error")?.textContent).toBe(
      p.themeLimitReached
    );
    expect(await savedIds()).not.toContain("one-too-many");
  });
});

// ── Writes fail too, and the owner has to be able to tell ───────────────────
describe("ThemeSettings · a refused write is visible", () => {
  it("keeps the row out of the list when 新增 is refused, and says why", async () => {
    // Jumping into the editor for a theme the server never created would let
    // the owner spend real work on something the next reload does not have.
    setToken("owner-token");
    const utils = await renderManage();
    vi.spyOn(api, "putTheme").mockRejectedValue(
      new ApiError("http 422", 422, codeForStatus(422), "伺服器不收")
    );

    fireEvent.click(utils.getByText(p.themeAdd));
    await settle();

    expect(utils.container.querySelector(".set-error")?.textContent).toBe(
      "伺服器不收"
    );
    expect(utils.container.querySelector(".ts-form-actions")).toBeNull();
    expect(utils.queryByTestId("ts-group-custom")).toBeNull();
    vi.restoreAllMocks();
    expect(await savedIds()).toEqual([]);
  });

  it("STAYS IN THE EDITOR when the save is refused, holding what was typed", async () => {
    // Returning to the list would throw away everything typed while showing a
    // list that still holds the OLD colours — i.e. it would look like the save
    // worked.
    setToken("owner-token");
    await api.putTheme({
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#aa0011" },
    });
    const utils = await renderManage();
    await utils.findByText("午夜藍");
    await openEditor(utils, "午夜藍");

    const label = tokenMeta("--color-accent", "zh").label;
    fireEvent.change(within(colourRow(utils, label)).getByLabelText(label), {
      target: { value: "#ffffff" },
    });
    vi.spyOn(api, "putTheme").mockRejectedValue(
      new ApiError("http 422", 422, codeForStatus(422), "伺服器不收")
    );
    await clickSave(utils);

    // Still in the editor, still holding the edit, with the reason on screen.
    expect(utils.container.querySelector(".ts-form-actions")).not.toBeNull();
    expect(
      (within(colourRow(utils, label)).getByLabelText(label) as HTMLInputElement)
        .value
    ).toBe("#ffffff");
    expect(utils.container.querySelector(".set-error")?.textContent).toBe(
      "伺服器不收"
    );
    vi.restoreAllMocks();
    // …and the stored theme still has its old colour.
    expect((await savedTheme("midnight")).colors["--color-accent"]).toBe("#aa0011");
  });

  it("KEEPS THE ROW when the delete is refused, closes the dialog and says why", async () => {
    // Delete is the third write this screen makes and the only one that can be
    // refused for a reason outside this cockpit: another device deleted the
    // theme first, so the request 404s. It is also the one where a silent
    // failure looks most like success — the owner asked for the row to go, and
    // a screen that removed it optimistically would agree with them until the
    // next reload brought it back.
    setToken("owner-token");
    await api.putTheme({
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#aa0011" },
    });
    const utils = await renderManage();
    await utils.findByText("午夜藍");

    vi.spyOn(api, "deleteTheme").mockRejectedValue(
      new ApiError("http 404", 404, codeForStatus(404), "theme not found")
    );
    fireEvent.click(await utils.findByLabelText(`${p.themeDelete} 午夜藍`));
    fireEvent.click(utils.getByTestId("theme-delete-confirm-btn"));
    await settle();

    // The reason is on screen, and the dialog is gone — it asked a question
    // that has now been answered.
    expect(utils.container.querySelector(".set-error")?.textContent).toBe(
      "theme not found"
    );
    expect(utils.queryByTestId("theme-delete-confirm-btn")).toBeNull();
    // The row is STILL THERE, ready for a second attempt…
    expect(utils.getByText("午夜藍")).toBeTruthy();
    vi.restoreAllMocks();
    // …and so is the theme itself.
    expect(await savedIds()).toEqual(["midnight"]);
  });

  it("releases the lock after a refused delete, so a second attempt is possible", async () => {
    // `busyId` guards delete too, and the confirm dialog stays up until the
    // request settles. A release that only happens on success leaves every row
    // inert after one failed delete — including the 刪除 the owner is about to
    // press again, which is the whole point of keeping the row.
    setToken("owner-token");
    await api.putTheme({
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#aa0011" },
    });
    const utils = await renderManage();
    await utils.findByText("午夜藍");

    const spy = vi
      .spyOn(api, "deleteTheme")
      .mockRejectedValue(new ApiError("http 500", 500, codeForStatus(500), "伺服器爆了"));
    fireEvent.click(await utils.findByLabelText(`${p.themeDelete} 午夜藍`));
    fireEvent.click(utils.getByTestId("theme-delete-confirm-btn"));
    await settle();
    expect(spy).toHaveBeenCalledTimes(1);

    // Second attempt: it must actually reach the server, not be swallowed by a
    // lock that was never released.
    fireEvent.click(await utils.findByLabelText(`${p.themeDelete} 午夜藍`));
    fireEvent.click(utils.getByTestId("theme-delete-confirm-btn"));
    await settle();
    expect(spy).toHaveBeenCalledTimes(2);
    vi.restoreAllMocks();
  });
});
