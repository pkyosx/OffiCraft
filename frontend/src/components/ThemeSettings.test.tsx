// ThemeSettings (T-16a1 P3b): the 設定/主題 management surface that theme
// management MOVED to from the profile dropdown. Covers import (moved verbatim
// + injection block), friendly grouped colour editing, and the 用詞 (wording)
// overlay editor round-trip.

import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { render, fireEvent, within, act } from "@testing-library/react";
import { THEME_COLOR_TOKENS } from "../styles/themeTokens.generated";
import { MESSAGE_KEYS } from "../i18n/messageKeys.generated";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { makeMessages } from "../i18n/compose";
import { ThemeSettings } from "./ThemeSettings";
import { tokenMeta } from "../lib/themeTokenMeta";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { setToken, clearToken } from "../api/auth";

const SENTINEL = "偽造";

const p = zh.profile;
const s = zh.settings;

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
  fireEvent.click(utils.getByText(p.themeConfirmImport));
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
    const srv = await api.getServerSettings();
    expect(srv.customThemes.map((b) => b.id)).toContain("midnight");

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
    await act(async () => {
      fireEvent.click(rowsNamedOffice()[1].querySelector("button.ts-pick")!);
      await new Promise((r) => setTimeout(r, 0));
    });
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
    const srv = await api.getServerSettings();
    expect(srv.customThemes.map((b) => b.id)).toContain("elfvillage");
    // …and the recognised override survived while the unknown ones did not.
    expect(srv.customThemes[0].wording).toEqual({ zh: { "nav.tasks": "任務榜" } });
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
    const srv = await api.getServerSettings();
    expect(srv.customThemes).toHaveLength(0);
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
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));

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
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));
    // The value text field carries the friendly label as its accessible name.
    fireEvent.change(utils.getByLabelText("主色"), {
      target: { value: "#ffffff" },
    });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    const srv = await api.getServerSettings();
    const b = srv.customThemes.find((x) => x.id === "midnight");
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
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));

    // Narrow the (large) wording list to exactly one code by searching the code.
    fireEvent.change(utils.getByLabelText(s.themeWordingSearch), {
      target: { value: "common.apply" },
    });
    const list = utils.container.querySelector(
      ".ts-wording-list"
    ) as HTMLElement;
    const input = within(list).getByRole("textbox");
    fireEvent.change(input, { target: { value: "套用替代" } });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    const srv = await api.getServerSettings();
    const b = srv.customThemes.find((x) => x.id === "midnight");
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
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));

    fireEvent.change(utils.getByLabelText(s.themeWordingSearch), {
      target: { value: "uninstallWarnBody2" },
    });
    const list = utils.container.querySelector(
      ".ts-wording-list"
    ) as HTMLElement;
    fireEvent.change(within(list).getByRole("textbox"), {
      target: { value: "」上頭還有 " },
    });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    const srv = await api.getServerSettings();
    const b = srv.customThemes.find((x) => x.id === "midnight");
    const stored = b?.wording?.zh?.["monitor.machine.uninstallWarnBody2"];
    expect(stored).toBe("」上頭還有 ");

    // …and the fragment composes into a sentence that still has its spaces.
    const themed = { ...zh, monitor: { ...zh.monitor, machine: { ...zh.monitor.machine, uninstallWarnBody2: stored! } } };
    expect(makeMessages(themed, "zh").machineUninstallWarnBody("Alpha", 3)).toBe(
      "「Alpha」上頭還有 3 位成員在線上。現在解除安裝會在成員仍在這台機器上時把 warden 拆除 —— 建議先將相關成員下線。仍要繼續嗎?"
    );
  });
});

// The 用詞 list mounts only the rows its 340px scroll window can show (T-8115 —
// ~870 controlled inputs at once is what made the editor slow to open). These
// tests pin the part that must NOT change: everything is still reachable. They
// deliberately assert reachability rather than "row X is absent", so a future
// implementation that mounts more (or all) of the list still passes.
describe("ThemeSettings · wording list is browsable in full", () => {
  async function openWordingEditor() {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));
    const list = utils.container.querySelector(
      ".ts-wording-list"
    ) as HTMLElement;
    return { utils, list };
  }

  const codesIn = (list: HTMLElement) =>
    Array.from(list.querySelectorAll("[data-wording-code]")).map((r) =>
      r.getAttribute("data-wording-code")
    );

  // Walk the scroll window from the top of the list to the bottom and union
  // everything it ever showed — i.e. what an owner with a mouse wheel can see.
  // 48px is the row pitch the component falls back to where there is no layout
  // (jsdom). The stride is one row short of however many rows the list is
  // currently showing, so it never outruns the window no matter how the window
  // is sized — and it takes as few steps as that allows, because this walk is
  // the most expensive thing in the file.
  const ROW_PITCH_PX = 48;
  function scrollThrough(list: HTMLElement) {
    const total = Number(list.getAttribute("data-wording-total"));
    const seen = new Set<string | null>();
    for (let top = 0; top <= total * ROW_PITCH_PX; ) {
      act(() => {
        list.scrollTop = top;
        fireEvent.scroll(list);
      });
      const shown = codesIn(list);
      for (const c of shown) seen.add(c);
      top += Math.max(1, shown.length - 1) * ROW_PITCH_PX;
    }
    return seen;
  }

  it("reaches every one of the ~870 codes by scrolling, with no search at all", async () => {
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
    // …and scrolling gets to every single one of them.
    const seen = scrollThrough(list);
    const missed = MESSAGE_KEYS.filter((c) => !seen.has(c));
    expect(missed).toEqual([]);

    // The last code is not a read-only tail: it is the same editable input as
    // any other row, and what is typed into it lands on the saved bundle.
    const last = MESSAGE_KEYS[MESSAGE_KEYS.length - 1];
    const row = list.querySelector(
      `[data-wording-code="${last}"]`
    ) as HTMLElement;
    fireEvent.change(within(row).getByRole("textbox"), {
      target: { value: "末列也能改" },
    });
    fireEvent.click(utils.getByRole("button", { name: p.save }));
    const srv = await api.getServerSettings();
    const b = srv.customThemes.find((x) => x.id === "midnight");
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

    // Every match is reachable — including the ones past the first screenful.
    const seen = scrollThrough(list);
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

  it("keeps the caret in the row you are editing when the list scrolls away", async () => {
    // The failure this guards is specific to windowing and did not exist
    // before it: unmounting the row focus lives in makes the browser hand
    // focus back to <body>, so an owner who scrolls down to compare a row
    // against another one and scrolls back finds their caret gone and their
    // keystrokes going nowhere. (The VALUE was never at risk — editWording
    // lives above the window — which is why this needs its own assertion
    // rather than leaning on the "override survives a scroll" one.)
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

    // Scroll far enough that the window cannot possibly still cover row 0.
    act(() => {
      list.scrollTop = 300 * ROW_PITCH_PX;
      fireEvent.scroll(list);
    });
    expect(codesIn(list)).not.toContain(MESSAGE_KEYS[1]);

    // Still the same element, still focused, still holding the edit.
    expect(document.activeElement).toBe(input);
    expect(input.value).toBe("我的未存編輯");
    expect(input.isConnected).toBe(true);

    // …and typing from there still lands on the right code.
    fireEvent.change(input, { target: { value: "我的未存編輯2" } });
    expect(
      (
        within(
          list.querySelector(`[data-wording-code="${code}"]`) as HTMLElement
        ).getByRole("textbox") as HTMLInputElement
      ).value
    ).toBe("我的未存編輯2");

    // Blurring releases the pin — the row must not linger once focus is gone,
    // or the window would slowly accumulate every row ever touched.
    act(() => {
      input.blur();
    });
    expect(codesIn(list)).not.toContain(code);
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
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));

    // The content-area background follows --color-bg, so no bundle ever exports
    // it — yet it must be reachable here, or only a hand-edited JSON can set it.
    const mainBg = utils.getByLabelText(tokenMeta("--color-main-bg", "zh").label);
    expect((mainBg as HTMLInputElement).value).toBe("");
    fireEvent.change(mainBg, { target: { value: "#12345680" } });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    const b = (await api.getServerSettings()).customThemes.find(
      (x) => x.id === "midnight"
    );
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
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));

    const label = tokenMeta("--color-card", "zh").label;
    const slider = utils.getByLabelText(`${label} ${s.themeColorOpacity}`);
    expect((slider as HTMLInputElement).value).toBe("100");
    fireEvent.change(slider, { target: { value: "40" } });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    const b = (await api.getServerSettings()).customThemes.find(
      (x) => x.id === "midnight"
    );
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
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));

    const mode = utils.getByLabelText(s.themeCanvasBgMode);
    fireEvent.change(mode, { target: { value: "sides" } });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    let b = (await api.getServerSettings()).customThemes.find(
      (x) => x.id === "midnight"
    );
    expect(b?.backgroundModes).toEqual({ canvas: "sides" });

    // Back to the default and the field disappears entirely — a tiling theme
    // stays byte-identical to one authored before the field existed.
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));
    fireEvent.change(utils.getByLabelText(s.themeCanvasBgMode), {
      target: { value: "tile" },
    });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    b = (await api.getServerSettings()).customThemes.find(
      (x) => x.id === "midnight"
    );
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
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 純色`));

    expect(utils.queryByLabelText(s.themeCanvasBgMode)).toBeNull();
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

    expect(utils.queryByText("午夜藍")).toBeNull();
    const srv = await api.getServerSettings();
    expect(srv.customThemes).toHaveLength(0);
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
