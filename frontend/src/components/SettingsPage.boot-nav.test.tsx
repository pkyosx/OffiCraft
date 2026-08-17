// 設定 › 角色誌 › 啟動程序 is an INDEX of two documents, one row each, and this
// pins the state half of that: the index carries NO document body, and pressing
// a row lands on that runtime's page ALONE (T-bac4).
//
// WHAT THIS REPLACED. The page used to stack both whole documents (the owner
// met that on a phone: he scrolled 啟動程序（Claude Code）to the end of its card,
// read the card's bottom as the end of the PAGE, and reported 啟動程序（Codex
// CLI）as missing), and then stacked them COLLAPSED (T-6278). This file used to
// pin the collapsed shape. The owner replaced it outright — 「我覺得呈現方式不好,
// 可以改成像任務手冊那樣嗎」 — so the collapse assertions are gone rather than
// left standing over a shape nothing renders. What SURVIVED the shape change,
// and is asserted below, is the claim underneath all three versions:
//
//   BOTH DOCUMENTS ARE ANNOUNCED BY THEIR OWN NAME, AND NEITHER CAN HIDE
//   BEHIND THE OTHER.
//
// jsdom pins the STATE (what renders); whether the two rows actually share one
// phone screen is geometry and is measured in visual-guards/boot-nav.ct.spec.tsx.
// Neither half is the whole claim.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock } from "../api/mock";

const s = zh.settings;

async function openBootIndex() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.roles));
  await utils.findByText(s.systemName);
  fireEvent.click(utils.getByText(s.bootName));
  await utils.findByTestId("boot-entry-claude");
  return utils;
}

beforeEach(() => {
  __resetMock();
});

describe("SettingsPage · 啟動程序 index", () => {
  it("lists both runtimes as rows and shows no document of its own", async () => {
    const { container, getByText, getByTestId } = await openBootIndex();

    // Each row names its OWN runtime. This is the assertion T-6278's review
    // sent that build back on: its collapse toggles carried a shared
    // aria-label, so both buttons announced the same thing and the very defect
    // being fixed (two documents you cannot tell apart) was rebuilt in the
    // accessibility tree.
    expect(getByText(s.bootClaudeName)).toBeTruthy();
    expect(getByText(s.bootCodexName)).toBeTruthy();
    expect(getByTestId("boot-entry-claude")).toBeTruthy();
    expect(getByTestId("boot-entry-codex")).toBeTruthy();

    // The index holds no document at all — not a collapsed one, none. This is
    // strictly stronger than the collapsed page it replaced: there is no state
    // in which the index can render a body.
    expect(container.querySelectorAll(".doc-md").length).toBe(0);
    expect(
      container.querySelectorAll('[data-testid="doc-card-edit"]').length
    ).toBe(0);
  });

  it("opens ONE runtime's page, and the other is not on it", async () => {
    const { container, getByTestId, findByTestId, queryByText, getAllByText } =
      await openBootIndex();

    fireEvent.click(getByTestId("boot-entry-claude"));
    await findByTestId("doc-card-edit");
    expect(container.querySelectorAll(".doc-md").length).toBe(1);
    // Twice on purpose: the terminal breadcrumb and the page title. getByText
    // would throw on the second one, and that throw would read as "the page is
    // wrong" rather than "it names itself in both places".
    expect(getAllByText(s.bootClaudeName).length).toBeGreaterThanOrEqual(1);
    // The other runtime is ABSENT, not merely closed. Their third step means
    // opposite things (claude attaches `ocagent listen` itself; codex must not,
    // the sidecar does), so a page that shows both invites copying one over the
    // other — which would silently stop that runtime's agents coming online.
    expect(queryByText(s.bootCodexName)).toBeNull();

    // NO COLLAPSE CONTROL ON THIS PAGE. T-fc57 recorded a 364px heading jump
    // when the LAST stacked card was collapsed from a scrolled-to-the-bottom
    // page (the page gets shorter than the scroll position, the browser clamps
    // scrollTop, and no scroll correction can undo it — which is why DocCard
    // carries none). One document per page means nothing here can collapse.
    //
    // ⚠️ SAY IT NO STRONGER THAN THIS. An earlier version of this comment said
    // the jump was "unreachable"; the independent review measured that the
    // MECHANISM is still in DocCard, just unused — so what is true is "no page
    // passes `collapsible`, and these three assertions are what keep it that
    // way". Put a collapse control back into a page and T-fc57 comes back with
    // it, unfixed.
    expect(
      container.querySelectorAll('[data-testid="doc-card-collapse"]').length
    ).toBe(0);
  });

  it("goes back to the index from a runtime page via the breadcrumb", async () => {
    const { getByTestId, findByTestId, getAllByText } = await openBootIndex();

    fireEvent.click(getByTestId("boot-entry-codex"));
    await findByTestId("doc-card-edit");

    // The crumb trail is 設定 › 角色誌 › 啟動程序; the last crumb returns to the
    // index. Without it a reader who opened one runtime could only reach the
    // other by going out to 角色誌 and back in.
    const crumbs = getAllByText(s.bootName);
    fireEvent.click(crumbs[0]);
    await findByTestId("boot-entry-claude");
  });

  it("edits and saves the runtime reached THROUGH the new index", async () => {
    // BootDocPage's own suite already covers edit/save/history — but it mounts
    // the card directly. What could break here is the WIRING: an index that
    // opened the wrong runtime's document, or dropped a prop the card needs,
    // would leave that suite green while every save through the real page went
    // to the wrong place. So this walks the route a person walks and checks the
    // text lands on the runtime whose row was pressed.
    const utils = await openBootIndex();
    fireEvent.click(utils.getByTestId("boot-entry-codex"));

    fireEvent.click(await utils.findByTestId("doc-card-edit"));
    fireEvent.change(utils.getByTestId("doc-card-editor"), {
      target: { value: "# 走新導覽列存進來的 codex 內容" },
    });
    fireEvent.click(utils.getByTestId("doc-card-save"));
    fireEvent.click(await utils.findByTestId("doc-card-save-confirm-btn"));
    await utils.findByText("走新導覽列存進來的 codex 內容");

    // …and the OTHER runtime did not receive it. The two documents' third step
    // means opposite things, so a save that crossed over would be silent and
    // would stop one runtime's agents ever coming online.
    fireEvent.click(utils.getAllByText(s.bootName)[0]);
    fireEvent.click(await utils.findByTestId("boot-entry-claude"));
    await utils.findByTestId("doc-card-edit");
    expect(
      utils.queryByText("走新導覽列存進來的 codex 內容")
    ).toBeNull();
  });

  it("keeps 系統互動 — the page carrying ONE document — as it always was", async () => {
    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.roles));
    await utils.findByText(s.systemName);
    fireEvent.click(utils.getByText(s.systemName));

    await utils.findByTestId("doc-card-edit");
    expect(utils.container.querySelectorAll(".doc-md").length).toBe(1);
    expect(
      utils.container.querySelectorAll('[data-testid="doc-card-collapse"]')
        .length
    ).toBe(0);
  });
});
