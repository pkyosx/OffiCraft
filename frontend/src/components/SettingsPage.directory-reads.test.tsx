// T-1170 — 設定 under the directory wire, from the screen's side.
//
// The sibling `api/mock.directory-reads.test.ts` pins the ADAPTER (what each
// list answers and where the text lives). This file pins the two things a user
// can actually observe, and they are deliberately separate assertions:
//
//   ① THE LISTS STILL RENDER. Every list here now draws itself from rows that
//      carry no document. "It compiles" says nothing about that — a page can
//      typecheck perfectly and render an empty roster.
//   ② OPENING A DOCUMENT REALLY FETCHES IT. Counted as CALLS on the per-item
//      read, not merely as text on screen: a page that had quietly kept a
//      stale copy (or that was handed one by a too-generous fake) would satisfy
//      "the text is visible" and fail this.
//
// Both run against the REAL mock adapter, which is the half of T-1170 that
// serves directory rows — see that file's header for why that matters.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import {
  mockApi,
  __resetMock,
  __injectMockTaskManual,
} from "../api/mock";

const s = zh.settings;

const SOP_MD = "先讀 diff，再看測試。";
const LEARNINGS_MD = "小心 flaky 測試。";
const DUTY_MD = "你是助理，負責替 owner 收斂雜事。";

function openSettings() {
  return render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  vi.restoreAllMocks();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("設定 › 角色誌 · the roster is a directory (T-1170)", () => {
  it("lists the roles without asking for any persona body, and fetches ONE when it is opened", async () => {
    await mockApi.saveRole("assistant", { definitionMd: DUTY_MD });
    const getRole = vi.spyOn(mockApi, "getRole");

    const utils = openSettings();
    fireEvent.click(utils.getByText(s.roles));

    // ① the role row is on screen…
    const roleLabel = zh.office.role.assistant;
    await utils.findByText(roleLabel);
    // …and landing on the LIST has read no document at all. Before T-1170 the
    // roster answer WAS the documents, so this is the saving being asserted.
    expect(getRole).not.toHaveBeenCalled();

    // ② opening the role fetches that role's document, and renders it.
    fireEvent.click(utils.getByText(roleLabel));
    await waitFor(() => expect(getRole).toHaveBeenCalledWith("assistant"));
    await utils.findByText(DUTY_MD);
  });

  it("shows the owner's save without waiting for a refetch or an SSE frame", async () => {
    const utils = openSettings();
    fireEvent.click(utils.getByText(s.roles));
    const roleLabel = zh.office.role.assistant;
    fireEvent.click(await utils.findByText(roleLabel));
    await utils.findByTestId("doc-card-edit");

    fireEvent.click(utils.getByTestId("doc-card-edit"));
    fireEvent.change(utils.getByTestId("doc-card-editor"), {
      target: { value: "改寫過的職責" },
    });
    // The page is no longer a slice of the roster array, so nothing else would
    // put the saved text back on screen: it adopts the write echo.
    fireEvent.click(utils.getByTestId("doc-card-save"));
    await utils.findByText("改寫過的職責");
  });

  it("states an honest failure when the roster answered but the document did not", async () => {
    // A state that could not exist before: two requests, one of which failed.
    // An empty card under a real role's title would read as "this role has no
    // definition", which is a different and false claim.
    //
    // The role is EDITED first, on purpose. That is what makes the badge
    // assertion below discriminating: the roster row now says 預設 is false,
    // so a badge on this page can only have come from the (absent) document.
    await mockApi.saveRole("assistant", { definitionMd: DUTY_MD });
    vi.spyOn(mockApi, "getRole").mockRejectedValue(new Error("boom"));

    const utils = openSettings();
    fireEvent.click(utils.getByText(s.roles));
    const roleLabel = zh.office.role.assistant;
    fireEvent.click(await utils.findByText(roleLabel));

    await utils.findByTestId("role-doc-load-error");
    // …and the editor stays shut rather than offering to overwrite a document
    // nobody managed to read.
    expect(utils.getByTestId("doc-card-edit")).toHaveProperty("disabled", true);

    // 🔴 THE FAILURE PAGE MUST NOT ALSO MAKE CLAIMS IT CANNOT SUPPORT.
    // The role above was EDITED by its owner, so 預設 is false — and the badge
    // used to be derived from the document, which on this page is null. That
    // turned "I could not read it" into the positive claim 預設, on the one
    // screen where the reader has nothing else to go on.
    expect(utils.queryByTestId("doc-card-default-badge")).toBeNull();
    // Nor may it print a size budget over a body it failed to load: the roster
    // row carries both numbers and answered fine, so 「N / cap」 would render
    // above a blank document and the two would contradict each other.
    expect(utils.queryByTestId("doc-card-usage")).toBeNull();
  });

  it("says it is LOADING rather than drawing an empty document", async () => {
    // Loading and "this role has no definition" are different screens, and an
    // empty <Markdown> is indistinguishable from the second — which is the
    // shape the manuals page avoids by drawing no card until its body lands.
    let release: (v: unknown) => void = () => {};
    const held = new Promise((r) => {
      release = r;
    });
    const real = mockApi.getRole.bind(mockApi);
    vi.spyOn(mockApi, "getRole").mockImplementation(async (key: string) => {
      await held;
      return real(key);
    });

    const utils = openSettings();
    fireEvent.click(utils.getByText(s.roles));
    fireEvent.click(await utils.findByText(zh.office.role.assistant));

    await utils.findByTestId("role-doc-loading");
    // No fabricated budget while the document is still in flight, for the same
    // reason as the failure case above.
    expect(utils.queryByTestId("doc-card-usage")).toBeNull();

    release(undefined);
    await waitFor(() =>
      expect(utils.queryByTestId("role-doc-loading")).toBeNull()
    );
    // …and once it lands the budget is back, so the assertion above is about
    // the loading window and not about the number having been removed.
    await utils.findByTestId("doc-card-usage");
  });

  it("keeps the 預設 badge honest off the ROSTER row while the document is still in flight", async () => {
    // The seeded role IS shipped-default and its roster row says so, so the
    // badge is readable before the body arrives. This is the other direction of
    // the same rule: the page must not go silent about a fact it holds.
    let release: (v: unknown) => void = () => {};
    const held = new Promise((r) => {
      release = r;
    });
    const real = mockApi.getRole.bind(mockApi);
    vi.spyOn(mockApi, "getRole").mockImplementation(async (key: string) => {
      await held;
      return real(key);
    });

    const utils = openSettings();
    fireEvent.click(utils.getByText(s.roles));
    fireEvent.click(await utils.findByText(zh.office.role.assistant));

    await utils.findByTestId("role-doc-loading");
    await utils.findByTestId("doc-card-default-badge");
    release(undefined);
  });
});

describe("設定 › 任務手冊 · the list is a directory (T-1170)", () => {
  beforeEach(() => {
    __injectMockTaskManual({
      typeKey: "tm-000000000001",
      displayName: "審查 PR",
      purpose: "審一份 PR",
      fields: [],
      sopMd: SOP_MD,
      learnings: LEARNINGS_MD,
      assignee: null,
      updatedTs: 1,
    });
  });

  it("lists the manuals and their hub without reading either document, then fetches on the sub-page", async () => {
    const getManual = vi.spyOn(mockApi, "getTaskManual");

    const utils = openSettings();
    fireEvent.click(utils.getByText(s.manuals));

    // ① the manual row renders from the directory…
    await utils.findByTestId("manual-open-tm-000000000001");
    fireEvent.click(utils.getByTestId("manual-open-tm-000000000001"));
    // …and so does the hub: 顯示名稱 / 負責成員 / the two entry cards all come
    // off the row, so neither long document has been read yet.
    await utils.findByTestId("manual-entry-definition");
    expect(getManual).not.toHaveBeenCalled();

    // ② the 任務定義 sub-page is what renders the SOP, so it reads the manual.
    fireEvent.click(utils.getByTestId("manual-entry-definition"));
    await waitFor(() =>
      expect(getManual).toHaveBeenCalledWith("tm-000000000001")
    );
    await utils.findByText(SOP_MD);
    // The OTHER document is not on this page — the read is per manual, but the
    // page still shows only its own half.
    expect(utils.queryByText(LEARNINGS_MD)).toBeNull();
  });

  it("renders 學習經驗 from its own read, not from anything the list carried", async () => {
    const utils = openSettings();
    fireEvent.click(utils.getByText(s.manuals));
    fireEvent.click(await utils.findByTestId("manual-open-tm-000000000001"));
    fireEvent.click(await utils.findByTestId("manual-entry-learnings"));
    await utils.findByText(LEARNINGS_MD);
  });

  it("says so when the manual's own read fails, instead of drawing a blank document", async () => {
    vi.spyOn(mockApi, "getTaskManual").mockRejectedValue(new Error("boom"));

    const utils = openSettings();
    fireEvent.click(utils.getByText(s.manuals));
    fireEvent.click(await utils.findByTestId("manual-open-tm-000000000001"));
    fireEvent.click(await utils.findByTestId("manual-entry-definition"));

    await utils.findByTestId("manual-doc-load-error");
    // The page keeps its title — the owner is not dropped somewhere
    // unlabelled because one request failed. (The same words also sit in the
    // breadcrumb, which is the other half of that statement.)
    expect(
      utils.container.querySelector(".settings__title--doc")?.textContent
    ).toBe(s.manualTabDefinition);
  });
});

describe("版本紀錄 · the list is a picker, the reader fetches (T-1170)", () => {
  it("draws every retained revision from the directory and reads only the one that is opened", async () => {
    await mockApi.saveGlobalContext("第一版");
    await mockApi.saveGlobalContext("第二版");
    await mockApi.saveGlobalContext("第三版");
    const getRevision = vi.spyOn(mockApi, "getDocumentRevision");

    const utils = openSettings();
    fireEvent.click(utils.getByText(s.roles));
    await utils.findByText(s.systemName);
    fireEvent.click(utils.getByText(s.customName));
    fireEvent.click(await utils.findByTestId("doc-card-edit"));
    fireEvent.click(utils.getByTestId("doc-history-entry-global_context"));
    await utils.findByTestId("doc-history-list");

    // ① two retained revisions are listed (the first write replaced an empty
    // document and retains nothing) — the list renders off the directory.
    const listed = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    expect(listed).toHaveLength(2);
    await utils.findByTestId(`doc-history-open-${listed[0].id}`);
    await utils.findByTestId(`doc-history-open-${listed[1].id}`);
    expect(
      utils.container.querySelectorAll(
        ".doc-hist__item:not(.doc-hist__item--seed)"
      )
    ).toHaveLength(2);
    // Opening the LIST costs nothing per revision — that is the whole point of
    // the split: three documents are no longer downloaded to show a picker.
    expect(getRevision).not.toHaveBeenCalled();

    // ② opening ONE row fetches exactly that revision.
    fireEvent.click(utils.getByTestId(`doc-history-open-${listed[0].id}`));
    await waitFor(() =>
      expect(
        within(utils.getByTestId("doc-history-modal")).getByText("第二版")
      ).toBeTruthy()
    );
    expect(getRevision).toHaveBeenCalledTimes(1);

    // …and stepping back and into the OTHER row reads that one, rather than
    // showing the first one's text under the second one's heading.
    fireEvent.click(utils.getByTestId("doc-history-modal-back"));
    fireEvent.click(utils.getByTestId(`doc-history-open-${listed[1].id}`));
    await waitFor(() =>
      expect(
        within(utils.getByTestId("doc-history-modal")).getByText("第一版")
      ).toBeTruthy()
    );
    expect(getRevision).toHaveBeenCalledTimes(2);
  });

  it("says the revision could not be read rather than drawing it empty", async () => {
    await mockApi.saveGlobalContext("第一版");
    await mockApi.saveGlobalContext("第二版");
    vi.spyOn(mockApi, "getDocumentRevision").mockRejectedValue(
      new Error("boom")
    );

    const utils = openSettings();
    fireEvent.click(utils.getByText(s.roles));
    await utils.findByText(s.systemName);
    fireEvent.click(utils.getByText(s.customName));
    fireEvent.click(await utils.findByTestId("doc-card-edit"));
    fireEvent.click(utils.getByTestId("doc-history-entry-global_context"));
    await utils.findByTestId("doc-history-list");
    const row = await waitFor(() => {
      const found = utils.container.querySelectorAll(
        ".doc-hist__item:not(.doc-hist__item--seed)"
      );
      expect(found.length).toBeGreaterThan(0);
      return found[0] as HTMLElement;
    });
    fireEvent.click(row.querySelector(".doc-hist__row") as HTMLElement);

    // 「這個版本沒有內容」 would be a different, and false, statement — and it
    // would be made next to a button that overwrites the live document.
    const modal = await utils.findByTestId("doc-history-modal");
    await utils.findByTestId("doc-history-default-unreadable");
    expect(modal.textContent).not.toContain(s.historyModalEmpty);
    // The restore itself stays live: putting the document back on that
    // revision needs nothing from this client.
    expect(
      utils.getByTestId("doc-history-modal-restore")
    ).toHaveProperty("disabled", false);
  });
});
