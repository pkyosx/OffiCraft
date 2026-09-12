// 設定 › 任務手冊 — the 「已用 / 上限」 readout on the SOP (T-100).
//
// The defect: the document has been capped all along and the page showed the
// number NOWHERE, so the owner discovered the limit by being refused after he
// had written the thing. His words: 「這個進去以後有一萬多字的限制的 context
// 卻不像是 global context 有顯示 xxxx/oooo 的數字，我看不出來多滿了。」
//
// What is locked here, and why each one is a separate assertion:
//   1. The document draws its readout at all.
//   2. IT FOLLOWS THE DRAFT. A readout showing the SAVED size moves only after
//      the write the owner was trying to decide about — the role journal's
//      cards were exactly that, and they are the precedent this ticket did NOT
//      follow. This is the acceptance criterion, not a refinement of it.
//   3. IT READS ITS OWN PAIR. The size and the cap are measured PER MANUAL, so
//      the fixture uses numbers that cannot be confused with each other.
//   4. A cap of 0 draws NOTHING. The fields are optional on the wire and the
//      mapper floors them at 0, so 「1234 / 0」 would tell the owner he is
//      already over on a document that is fine.
//
// The last test goes through the REAL page and the mock adapter, because the
// tests above hand the sub-page a manual directly and so never exercise the
// wiring that fetches one.
//
// ⚠️ WHAT THAT LAST TEST DOES NOT COVER, stated because an earlier draft of
// this header claimed the opposite and a mutant proved it wrong: the MOCK
// adapter builds its views itself and never calls `toTaskManualSummary`, so
// blanking the fields in that mapper leaves every test in THIS FILE green.
// The mapper — the only path the real HTTP seam takes — is covered by
// `api/mappers.task-manual-usage.test.ts`. Neither file substitutes for the
// other.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskManualDefinitionPage } from "./TaskManualsPage";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, __injectMockTaskManual } from "../api/mock";
import type { TaskManualView } from "../api/adapter";

const SOP_TEXT = "SOP 內容";        // 6 code points

function mkManual(over: Partial<TaskManualView> = {}): TaskManualView {
  return {
    typeKey: "tm-usage",
    displayName: "審查 PR",
    purpose: "",
    fields: [],
    sopMd: SOP_TEXT,
    assignee: null,
    updatedTs: 0,
    // The size is the true rune count of the text above, the way the server
    // measures it; the cap is deliberately unlike it.
    sopMdChars: [...SOP_TEXT].length,
    sopMdCapChars: 18000,
    ...over,
  };
}

function renderDefinition(manual: TaskManualView | null) {
  return render(
    <I18nProvider>
      <TaskManualDefinitionPage
        manual={manual}
        crumbs={[{ label: "設定" }]}
        onSave={async () => undefined}
      />
    </I18nProvider>
  );
}

describe("任務手冊 — 任務定義 (SOP) 的已用/上限", () => {
  it("shows the STORED size against the SOP's OWN cap before editing", () => {
    const { getByTestId } = renderDefinition(mkManual());
    expect(getByTestId("manual-sop-usage").textContent).toBe("6 / 18000");
  });

  it("follows the draft as it is typed, not the saved document", () => {
    const { getByTestId } = renderDefinition(mkManual());
    fireEvent.click(getByTestId("manual-def-edit-3"));
    fireEvent.change(getByTestId("manual-sop-input"), {
      target: { value: "一二三四五六七八九十" },
    });
    // 10 code points typed ⇒ 10, NOT the 6 that is still on the server. This is
    // the assertion the whole ticket is about; a readout reading `sopMdChars`
    // would say 6 here and would be wrong in the only moment it is consulted.
    expect(getByTestId("manual-sop-usage").textContent).toBe("10 / 18000");
  });

  it("counts code points, not UTF-16 units — an emoji is ONE character", () => {
    // The server measures with utf8.RuneCountInString. `String.length` would
    // say 2 for a single astral character and would shrink a CJK owner's
    // visible budget against a cap he was never judged by.
    const { getByTestId } = renderDefinition(mkManual());
    fireEvent.click(getByTestId("manual-def-edit-3"));
    fireEvent.change(getByTestId("manual-sop-input"), {
      target: { value: "🙂🙂🙂" },
    });
    expect(getByTestId("manual-sop-usage").textContent).toBe("3 / 18000");
  });

  it("draws nothing when the cap is not known (0), rather than 「n / 0」", () => {
    const { queryByTestId } = renderDefinition(
      mkManual({ sopMdCapChars: 0 })
    );
    expect(queryByTestId("manual-sop-usage")).toBeNull();
  });
});

describe("任務手冊 — the numbers survive the real adapter", () => {
  beforeEach(() => {
    __resetMock();
    window.location.hash = "";
  });

  it("reaches the page through the mock adapter, measured on the stored text", async () => {
    // 🔴 This covers the page-to-adapter wiring: every test above hands a
    // manual straight to a sub-page. The mock adapter builds its view itself,
    // so this file stays green if `toTaskManualSummary` drops the wire
    // fields. The mapper path is covered separately by
    // `api/mappers.task-manual-usage.test.ts`.
    __injectMockTaskManual({
      typeKey: "tm-real",
      displayName: "審查 PR",
      purpose: "",
      fields: [],
      sopMd: "一二三四五六七",
      assignee: null,
      updatedTs: 0,
    });
    const { findByTestId, getByTestId, getByText } = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(await findByTestId("settings-manuals-entry"));
    fireEvent.click(await findByTestId("manual-open-tm-real"));
    fireEvent.click(await findByTestId("manual-entry-definition"));

    await waitFor(() => expect(getByTestId("manual-sop-usage")).toBeTruthy());
    // 7 = the rune count the mock derives from the STORED text, the way the
    // server derives it — not a number carried alongside it that an edit could
    // leave behind. The cap is whatever the mock's settings say, so it is read
    // rather than restated here: pinning a literal would make this test a copy
    // of DOC_CAP_CHARS_DEFAULTS instead of a check that the value travelled.
    const [shown, cap] = getByTestId("manual-sop-usage")
      .textContent!.split("/")
      .map((s) => Number(s.trim()));
    expect(shown).toBe(7);
    expect(cap).toBeGreaterThan(0);
    expect(getByText("審查 PR")).toBeTruthy();
  });
});
