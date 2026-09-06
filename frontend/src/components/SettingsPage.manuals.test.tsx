// 設定 › 任務手冊 (M3 SPEC §5). Locked here — the acceptance behaviors:
//   1. The settings landing carries the 任務手冊 entry (與角色誌並列); the
//      list starts HONESTLY EMPTY (出廠不含任何類型).
//   2. 新增類型 (T-fa76): the inline row takes a DISPLAY NAME; the system
//      mints the tm- type_key (never the user's text) and the list row shows
//      the display name — the key stays out of the UI.
//   3. The detail is a HUB: 負責成員 summary card + 任務規劃 entry cards —
//      clicking 任務定義 / 學習經驗 PUSHES its own breadcrumb sub-page (設定 ›
//      任務手冊 › <type> › 任務定義/學習經驗, owner 2026-07-20), where the
//      <type> crumb returns to the hub; never shows a filename.
//   4. 任務定義 editing (owner 2026-07-31, proposal P1 — 三塊各自編輯): every
//      block is READ-ONLY by default and carries its OWN 編輯 in its own
//      heading; 完成編輯 persists THAT BLOCK ALONE ({purpose} / {fields} /
//      {sopMd}); 取消 discards its draft alone. ALL THREE may be open at once,
//      each holding its own draft (owner 2026-07-31, superseding the
//      one-block-at-a-time rule). The 版本紀錄 entry belongs to block ③'s edit
//      row only (only the SOP is versioned).
//      (No 重置 — manuals have no seed.)
//   5. 學習經驗 is editable (agent write-back surface, owner-editable too).
//   6. 負責成員 card: member pick or 外包 (model + effort + copies ×N).
//   7. Delete: confirm modal; a type with OPEN tasks survives its 409 with the
//      honest 先讓它們結束 message; a closed-task type deletes.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { SettingsPage } from "./SettingsPage";
import {
  __resetMock,
  __injectMockTask,
  __injectMockTaskManual,
} from "../api/mock";
import { mockApiError } from "../api/errorCodes";
import { api } from "../api";
import type { TaskManualView, TaskView } from "../api/adapter";

let seq = 0;

function mkTask(over: Partial<TaskView>): TaskView {
  seq += 1;
  return {
    id: `task-${seq}`,
    taskNo: `T-${1000 + seq}`,
    title: `任務 ${seq}`,
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "member",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: Date.now() / 1000 - 3600,
    updatedTs: Date.now() / 1000 - 60,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    ...over,
  };
}

function mkManual(over: Partial<TaskManualView>): TaskManualView {
  return {
    typeKey: "review-pr",
    displayName: "",
    purpose: "",
    fields: [],
    sopMd: "",
    learnings: "",
    assignee: null,
    updatedTs: 0,
    ...over,
  };
}

async function renderManualsList() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(await utils.findByTestId("settings-manuals-entry"));
  return utils;
}

/** Every PATCH 任務定義 sent, in order — the only place the per-block scoping
 * is visible: a payload carrying a key the owner never opened looks identical
 * on screen to one that does not. */
let updateManualPatches: Record<string, unknown>[] = [];

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  updateManualPatches = [];
  vi.spyOn(api, "updateTaskManual").mockImplementation(async (key, patch) => {
    updateManualPatches.push({ ...patch });
    return realUpdateTaskManual(key, patch);
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

const realUpdateTaskManual = api.updateTaskManual.bind(api);

describe("設定 › 任務手冊 — list", () => {
  it("starts honestly empty (出廠不含任何類型)", async () => {
    const { findByTestId } = await renderManualsList();
    const empty = await findByTestId("manuals-empty");
    expect(empty.textContent).toContain("還沒有任務類型");
  });

  it("creates a blank manual from a display name — the system mints the tm- key", async () => {
    const { findByTestId, getByTestId, getByText, queryByTestId } =
      await renderManualsList();
    fireEvent.click(await findByTestId("manual-add-entry"));
    fireEvent.change(getByTestId("manual-create-key"), {
      target: { value: "審查 PR" },
    });
    fireEvent.click(getByTestId("manual-create-submit"));

    // The row shows the DISPLAY NAME the user typed…
    await waitFor(() => expect(getByText("審查 PR")).toBeTruthy());
    expect(queryByTestId("manuals-empty")).toBeNull();
    // …while the store carries a BLANK manual under a SYSTEM-minted tm- key
    // (never the user's text), which stays out of the row's UI.
    const manuals = await api.listTaskManuals();
    expect(manuals).toHaveLength(1);
    const manual = manuals[0];
    expect(manual.typeKey).toMatch(/^tm-[0-9a-f]{12}$/);
    expect(manual).toMatchObject({
      displayName: "審查 PR",
      purpose: "",
      fields: [],
      assignee: null,
    });
    // T-1170: the LIST answer does not carry either long document — so the
    // blankness of a fresh manual is asserted where it is actually readable,
    // on the manual's own read.
    expect(manual).not.toHaveProperty("sopMd");
    expect(manual).not.toHaveProperty("learnings");
    expect(await api.getTaskManual(manual.typeKey)).toMatchObject({
      sopMd: "",
      learnings: "",
    });
    const row = getByTestId(`manual-open-${manual.typeKey}`);
    expect(
      row.querySelector(".set-entry__name")?.getAttribute("title")
    ).toBeNull();
  });

  it("a blank display name never reaches the wire — inline error instead", async () => {
    const { findByTestId, getByTestId, queryByTestId } =
      await renderManualsList();
    fireEvent.click(await findByTestId("manual-add-entry"));
    fireEvent.change(getByTestId("manual-create-key"), {
      target: { value: "   " },
    });
    fireEvent.click(getByTestId("manual-create-submit"));
    const err = await findByTestId("manual-create-error");
    expect(err.textContent).toContain("建立失敗");
    expect(queryByTestId("manuals-empty")).not.toBeNull();
  });

  it("delete: open tasks of the type → 409, human message; free type deletes", async () => {
    __injectMockTaskManual(
      mkManual({ typeKey: "review-pr", displayName: "審查 PR" })
    );
    __injectMockTask(mkTask({ typeKey: "review-pr", status: "in_progress" }));

    const { findByTestId, getByTestId, queryByTestId } =
      await renderManualsList();
    fireEvent.click(await findByTestId("manual-delete-review-pr"));
    // The confirm modal names the type by its DISPLAY face, not the key
    // (T-fa76: the key is the system's, never the human copy).
    expect(getByTestId("manual-delete-confirm").textContent).toContain(
      "審查 PR"
    );
    expect(getByTestId("manual-delete-confirm").textContent).not.toContain(
      "review-pr"
    );
    fireEvent.click(getByTestId("manual-delete-confirm-btn"));
    await waitFor(() =>
      expect(getByTestId("manual-delete-confirm").textContent).toContain(
        "這個類型還有未結束的任務，先讓它們結束才能刪除"
      )
    );
    // Still listed — nothing was deleted.
    expect(queryByTestId("manual-open-review-pr")).not.toBeNull();

    // Close the blocking task → the delete goes through.
    const open = (await api.listTasks())[0];
    await api.terminateTask(open.id);
    fireEvent.click(getByTestId("manual-delete-confirm-btn"));
    await waitFor(() =>
      expect(queryByTestId("manual-open-review-pr")).toBeNull()
    );
    expect(await api.listTaskManuals()).toEqual([]);
  });
});

describe("設定 › 任務手冊 — detail", () => {
  it("hub → 任務定義 entry pushes the definition sub-page (not inline), READ-ONLY by default; <type> crumb returns to the hub (owner 2026-07-20)", async () => {
    __injectMockTaskManual(
      mkManual({
        typeKey: "review-pr",
        displayName: "審查 PR",
        purpose: "Review 進來的 PR。",
        fields: [{ name: "PR 連結", required: true, isKey: true }],
        sopMd: "# steps\n1. review",
      })
    );
    const { findByTestId, getByTestId, queryByTestId, getByText, container } =
      await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));

    // The HUB shows the two 任務規劃 entry cards — no sub-page card inline yet.
    await findByTestId("manual-entry-definition");
    await findByTestId("manual-entry-learnings");
    expect(queryByTestId("manual-definition-card")).toBeNull();

    // Click 任務定義: it PUSHES a sub-page. The hub-only 負責成員 card and the
    // entry buttons are gone (a real navigation, not an inline expand)…
    fireEvent.click(getByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");
    expect(queryByTestId("manual-assignee-card")).toBeNull();
    expect(queryByTestId("manual-entry-learnings")).toBeNull();

    // …the breadcrumb reads 設定 › 任務手冊 › 審查 PR › 任務定義.
    const crumbText = container.querySelector(".crumbs")!.textContent!;
    expect(crumbText).toContain("設定");
    expect(crumbText).toContain("任務手冊");
    expect(crumbText).toContain("審查 PR");
    expect(crumbText).toContain("任務定義");

    // Default is READ-ONLY on the sub-page: each block carries its OWN 編輯
    // switch, no editors, and every section renders the stored content
    // read-only.
    expect(queryByTestId("manual-def-edit-1")).not.toBeNull();
    expect(queryByTestId("manual-def-edit-2")).not.toBeNull();
    expect(queryByTestId("manual-def-edit-3")).not.toBeNull();
    expect(queryByTestId("manual-purpose-input")).toBeNull();
    expect(getByTestId("manual-purpose-view").textContent).toBe(
      "Review 進來的 PR。"
    );
    expect(getByTestId("manual-field-view-0").textContent).toContain(
      "PR 連結"
    );
    expect(getByTestId("manual-section-3").textContent).toContain("review");

    // The 審查 PR crumb navigates back to the hub (assignee card + entries).
    fireEvent.click(getByText("審查 PR"));
    await findByTestId("manual-assignee-card");
    expect(queryByTestId("manual-definition-card")).toBeNull();
    expect(queryByTestId("manual-entry-definition")).not.toBeNull();
  });

  it("每一塊各自 編輯 → 完成編輯, and the PATCH carries that block's key alone (owner 2026-07-31 P1)", async () => {
    __injectMockTaskManual(
      mkManual({
        typeKey: "review-pr",
        fields: [{ name: "PR 連結", required: true, isKey: true }],
      })
    );
    const { findByTestId, getByTestId, queryByTestId } =
      await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));
    fireEvent.click(await findByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");

    // ① 這是什麼任務? — opening block 1 mounts ITS editor and no other.
    fireEvent.click(getByTestId("manual-def-edit-1"));
    await findByTestId("manual-purpose-input");
    expect(queryByTestId("manual-fields-editor")).toBeNull();
    expect(queryByTestId("manual-sop-input")).toBeNull();
    fireEvent.change(getByTestId("manual-purpose-input"), {
      target: { value: "Review 進來的 Pull Request。" },
    });
    // Nothing saved until 完成編輯.
    expect((await api.getTaskManual("review-pr")).purpose).toBe("");
    fireEvent.click(getByTestId("manual-def-done-1"));
    await waitFor(async () =>
      expect((await api.getTaskManual("review-pr")).purpose).toBe(
        "Review 進來的 Pull Request。"
      )
    );
    // Back to read-only after saving.
    await waitFor(() => expect(getByTestId("manual-def-edit-1")).toBeTruthy());

    // ② 需要哪些資訊? — a second composite field, saved on its own.
    fireEvent.click(getByTestId("manual-def-edit-2"));
    await findByTestId("manual-fields-editor");
    expect((getByTestId("manual-field-name-0") as HTMLInputElement).value).toBe(
      "PR 連結"
    );
    fireEvent.click(getByTestId("manual-field-add"));
    fireEvent.change(getByTestId("manual-field-name-1"), {
      target: { value: "repo" },
    });
    fireEvent.click(getByTestId("manual-field-required-1")); // 選填 → 必填
    fireEvent.click(getByTestId("manual-field-key-1")); // 🔑識別鍵 (複合)
    fireEvent.click(getByTestId("manual-def-done-2"));
    await waitFor(async () =>
      expect((await api.getTaskManual("review-pr")).fields).toEqual([
        { name: "PR 連結", required: true, isKey: true },
        { name: "repo", required: true, isKey: true },
      ])
    );

    // ③ 該怎麼做? — the SOP, saved on its own.
    fireEvent.click(await findByTestId("manual-def-edit-3"));
    fireEvent.change(await findByTestId("manual-sop-input"), {
      target: { value: "# steps\n1. review" },
    });
    fireEvent.click(getByTestId("manual-def-done-3"));
    await waitFor(async () =>
      expect((await api.getTaskManual("review-pr")).sopMd).toBe(
        "# steps\n1. review"
      )
    );

    // The three writes were three PATCHes, each naming ONE key: a block's
    // 完成編輯 must never carry a draft the owner did not open.
    expect(updateManualPatches).toEqual([
      { purpose: "Review 進來的 Pull Request。" },
      {
        fields: [
          { name: "PR 連結", required: true, isKey: true },
          { name: "repo", required: true, isKey: true },
        ],
      },
      { sopMd: "# steps\n1. review" },
    ]);
  });

  it("編輯 → 取消 discards that block's draft and saves nothing (owner 2026-07-31 P1)", async () => {
    __injectMockTaskManual(
      mkManual({
        typeKey: "review-pr",
        purpose: "原本的用途",
        fields: [{ name: "PR 連結", required: true, isKey: true }],
      })
    );
    const { findByTestId, getByTestId, queryByTestId } =
      await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));
    fireEvent.click(await findByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");

    fireEvent.click(getByTestId("manual-def-edit-1"));
    fireEvent.change(await findByTestId("manual-purpose-input"), {
      target: { value: "改壞的草稿" },
    });
    fireEvent.click(getByTestId("manual-def-cancel-1"));
    expect(queryByTestId("manual-purpose-input")).toBeNull();
    expect(getByTestId("manual-purpose-view").textContent).toBe("原本的用途");

    fireEvent.click(getByTestId("manual-def-edit-2"));
    fireEvent.click(await findByTestId("manual-field-remove-0"));
    fireEvent.click(getByTestId("manual-def-cancel-2"));

    // Read-only again, showing the ORIGINAL content — nothing persisted.
    expect(getByTestId("manual-field-view-0").textContent).toContain(
      "PR 連結"
    );
    const m = await api.getTaskManual("review-pr");
    expect(m.purpose).toBe("原本的用途");
    expect(m.fields).toEqual([{ name: "PR 連結", required: true, isKey: true }]);
    expect(updateManualPatches).toEqual([]);
  });

  it("saves ONE block while the other two are open and dirty — the PATCH carries that block's key alone (owner 2026-07-31)", async () => {
    // The case that can actually clobber. With every block open and holding
    // unfinished text, a card-wide payload would push the two the owner did not
    // press 完成編輯 on — and the screen looks identical either way.
    __injectMockTaskManual(
      mkManual({
        typeKey: "review-pr",
        purpose: "原本的用途",
        fields: [{ name: "PR 連結", required: true, isKey: true }],
        sopMd: "# 舊 SOP",
      })
    );
    const { findByTestId, getByTestId } = await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));
    fireEvent.click(await findByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");

    // Open all three and type in each.
    fireEvent.click(getByTestId("manual-def-edit-1"));
    fireEvent.click(getByTestId("manual-def-edit-2"));
    fireEvent.click(getByTestId("manual-def-edit-3"));
    fireEvent.change(await findByTestId("manual-purpose-input"), {
      target: { value: "還在寫的用途" },
    });
    fireEvent.change(getByTestId("manual-field-name-0"), {
      target: { value: "還在寫的欄位" },
    });
    fireEvent.change(getByTestId("manual-sop-input"), {
      target: { value: "# 還在寫的 SOP" },
    });

    // 完成編輯 on ② only.
    fireEvent.click(getByTestId("manual-def-done-2"));
    await waitFor(async () =>
      expect((await api.getTaskManual("review-pr")).fields).toEqual([
        { name: "還在寫的欄位", required: true, isKey: true },
      ])
    );

    // The server saw ②'s key and nothing else…
    expect(updateManualPatches).toEqual([
      { fields: [{ name: "還在寫的欄位", required: true, isKey: true }] },
    ]);
    const m = await api.getTaskManual("review-pr");
    expect(m.purpose).toBe("原本的用途");
    expect(m.sopMd).toBe("# 舊 SOP");

    // …and ① and ③ are still open, still showing their unsaved text.
    expect(
      (getByTestId("manual-purpose-input") as HTMLTextAreaElement).value
    ).toBe("還在寫的用途");
    expect((getByTestId("manual-sop-input") as HTMLTextAreaElement).value).toBe(
      "# 還在寫的 SOP"
    );
  });

  it("keeps all three blocks open at once, each holding its own draft (owner 2026-07-31)", async () => {
    // Supersedes the one-block-at-a-time rule: nothing is disabled, because
    // per-block drafts leave no way for a block change to lose typing.
    __injectMockTaskManual(
      mkManual({
        typeKey: "review-pr",
        purpose: "原本的用途",
        sopMd: "# 舊 SOP",
      })
    );
    const { findByTestId, getByTestId } = await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));
    fireEvent.click(await findByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");

    fireEvent.click(getByTestId("manual-def-edit-1"));
    fireEvent.change(await findByTestId("manual-purpose-input"), {
      target: { value: "①的草稿" },
    });

    // Opening another block is a live affordance, never a dead one…
    const third = getByTestId("manual-def-edit-3") as HTMLButtonElement;
    expect(third.disabled).toBe(false);
    expect(third.getAttribute("title")).toBeNull();
    fireEvent.click(third);
    fireEvent.change(await findByTestId("manual-sop-input"), {
      target: { value: "# ③的草稿" },
    });
    fireEvent.click(getByTestId("manual-def-edit-2"));
    await findByTestId("manual-field-add");

    // …and all three editors hold their own text at the same time.
    expect(
      (getByTestId("manual-purpose-input") as HTMLTextAreaElement).value
    ).toBe("①的草稿");
    expect((getByTestId("manual-sop-input") as HTMLTextAreaElement).value).toBe(
      "# ③的草稿"
    );
    expect(getByTestId("manual-fields-editor")).toBeTruthy();

    // 取消 on ① discards ①'s draft and closes ① — and touches nothing else.
    fireEvent.click(getByTestId("manual-def-cancel-1"));
    expect(getByTestId("manual-purpose-view").textContent).toBe("原本的用途");
    expect((getByTestId("manual-sop-input") as HTMLTextAreaElement).value).toBe(
      "# ③的草稿"
    );
    expect(getByTestId("manual-fields-editor")).toBeTruthy();
    // Re-opening ① shows the STORED content: the cancelled draft is gone.
    fireEvent.click(getByTestId("manual-def-edit-1"));
    expect(
      (getByTestId("manual-purpose-input") as HTMLTextAreaElement).value
    ).toBe("原本的用途");
  });

  it("keeps block ③'s open 版本紀錄 list up while the SOP draft is typed into", async () => {
    // The list is the block switch's CHILD, so it survives only while the
    // switch keeps its component identity across renders — and a render happens
    // on every keystroke in the SOP. Declared inside DefinitionCard the switch
    // would be a NEW component type each time, remounting the entry and closing
    // the list from under the reader. Nothing else in the suite would notice.
    __injectMockTaskManual(mkManual({ typeKey: "review-pr", sopMd: "# 舊 SOP" }));
    const { findByTestId, getByTestId } = await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));
    fireEvent.click(await findByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");
    fireEvent.click(getByTestId("manual-def-edit-3"));
    fireEvent.click(await findByTestId("doc-history-entry-task_manual_sop"));
    await findByTestId("doc-history-list");

    fireEvent.change(getByTestId("manual-sop-input"), {
      target: { value: "# 舊 SOP\n改一個字" },
    });
    expect(getByTestId("doc-history-list")).toBeTruthy();
  });

  it("names the three otherwise identical 編輯 buttons by their block", async () => {
    __injectMockTaskManual(mkManual({ typeKey: "review-pr" }));
    const { findByTestId, getByTestId } = await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));
    fireEvent.click(await findByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");

    expect(getByTestId("manual-def-edit-1").getAttribute("aria-label")).toBe(
      "編輯「這是什麼任務？」"
    );
    expect(getByTestId("manual-def-edit-2").getAttribute("aria-label")).toBe(
      "編輯「需要哪些資訊？」"
    );
    expect(getByTestId("manual-def-edit-3").getAttribute("aria-label")).toBe(
      "編輯「該怎麼做？」"
    );
  });

  it("puts the 版本紀錄 entry in block ③'s edit row and nowhere else (owner 2026-07-31 P1)", async () => {
    __injectMockTaskManual(mkManual({ typeKey: "review-pr", sopMd: "# sop" }));
    const { findByTestId, getByTestId, queryByTestId } =
      await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));
    fireEvent.click(await findByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");

    // Read-only: no version entry anywhere on the page.
    expect(queryByTestId("doc-history-entry-task_manual_sop")).toBeNull();

    // Editing ① or ② — the blocks that are NOT versioned — must not offer one.
    fireEvent.click(getByTestId("manual-def-edit-1"));
    fireEvent.click(getByTestId("manual-def-edit-2"));
    expect(queryByTestId("doc-history-entry-task_manual_sop")).toBeNull();

    // Only ③ 該怎麼做? — the SOP is the one versioned document on this page —
    // and it sits inside that block's own edit row.
    fireEvent.click(getByTestId("manual-def-edit-3"));
    const entry = await findByTestId("doc-history-entry-task_manual_sop");
    expect(getByTestId("manual-section-3").contains(entry)).toBe(true);
  });

  it("顯示名稱 renames via the hub-title inline-edit pencil (owner T-8a4a: moved out of 任務定義, same affordance as the 角色設定 title)", async () => {
    __injectMockTaskManual(mkManual({ typeKey: "review-pr", displayName: "" }));
    const { findByTestId, findByLabelText, container } =
      await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));

    // The title carries the same pencil inline-edit as the role title; the
    // display-name field is GONE from 任務定義.
    const pencil = await findByLabelText("顯示名稱");
    fireEvent.click(pencil);
    const input = container.querySelector(
      "input.inline-edit__input"
    ) as HTMLInputElement;
    expect(input).not.toBeNull();
    fireEvent.change(input, { target: { value: "審查 PR" } });
    fireEvent.keyDown(input, { key: "Enter" }); // ✓ apply

    await waitFor(async () =>
      expect((await api.getTaskManual("review-pr")).displayName).toBe("審查 PR")
    );
  });

  it("§3 SOP card shows NO filename chip and 任務定義 has no display-name field (owner T-8a4a)", async () => {
    __injectMockTaskManual(mkManual({ typeKey: "review-pr" }));
    const { findByTestId, getByTestId, queryByText, queryByTestId } =
      await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));
    fireEvent.click(await findByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");

    // The synthesized "<type>.md" filename is gone from the SOP card head…
    expect(queryByText("review-pr.md")).toBeNull();
    // …and the SOP's editor opens from block ③'s own switch (owner 2026-07-31
    // P1), not from a card-level one and not from an ex-per-section toggle.
    expect(queryByTestId("manual-sop-edit")).toBeNull();
    expect(queryByTestId("manual-def-edit-3")).not.toBeNull();
    fireEvent.click(getByTestId("manual-def-edit-3"));
    expect(await findByTestId("manual-sop-input")).toBeTruthy();
    // 顯示名稱 is no longer an inline field inside 任務定義 (moved to the title).
    expect(queryByTestId("manual-display-name-input")).toBeNull();
  });

  it("marking 🔑識別鍵 forces 必填 on, and clearing 必填 clears 識別鍵 (server gate 00010 parity)", async () => {
    __injectMockTaskManual(
      mkManual({
        typeKey: "review-pr",
        fields: [{ name: "PR 連結", required: false, isKey: false }],
      })
    );
    const { findByTestId, getByTestId } = await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));
    fireEvent.click(await findByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");
    fireEvent.click(getByTestId("manual-def-edit-2"));

    // Mark 識別鍵 on a not-yet-required field → 必填 must auto-turn on, so the
    // committed payload carries required:true (never the isKey && !required
    // combo the server 400s).
    fireEvent.click(await findByTestId("manual-field-key-0"));
    await waitFor(() =>
      expect(
        getByTestId("manual-field-required-0").getAttribute("aria-pressed")
      ).toBe("true")
    );
    fireEvent.click(getByTestId("manual-def-done-2"));
    await waitFor(async () =>
      expect((await api.getTaskManual("review-pr")).fields).toEqual([
        { name: "PR 連結", required: true, isKey: true },
      ])
    );

    // Clearing 必填 also clears 識別鍵 (a key can't be optional).
    fireEvent.click(await findByTestId("manual-def-edit-2"));
    fireEvent.click(getByTestId("manual-field-required-0"));
    await waitFor(() =>
      expect(
        getByTestId("manual-field-key-0").getAttribute("aria-pressed")
      ).toBe("false")
    );
    fireEvent.click(getByTestId("manual-def-done-2"));
    await waitFor(async () =>
      expect((await api.getTaskManual("review-pr")).fields).toEqual([
        { name: "PR 連結", required: false, isKey: false },
      ])
    );
  });

  it("removes a field via its row delete in edit mode (persists on 完成編輯)", async () => {
    __injectMockTaskManual(
      mkManual({
        typeKey: "review-pr",
        fields: [
          { name: "PR 連結", required: true, isKey: true },
          { name: "備註", required: false, isKey: false },
        ],
      })
    );
    const { findByTestId, getByTestId } = await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));
    fireEvent.click(await findByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");
    fireEvent.click(getByTestId("manual-def-edit-2"));
    fireEvent.click(await findByTestId("manual-field-remove-1"));
    fireEvent.click(getByTestId("manual-def-done-2"));
    await waitFor(async () => {
      expect((await api.getTaskManual("review-pr")).fields).toEqual([
        { name: "PR 連結", required: true, isKey: true },
      ]);
    });
  });

  it("學習經驗 entry pushes the learnings sub-page; content carries over and hand edit still persists (owner 2026-07-20)", async () => {
    __injectMockTaskManual(
      mkManual({
        typeKey: "review-pr",
        displayName: "審查 PR",
        learnings: "## 經驗\n- 舊經驗",
      })
    );
    const { findByTestId, getByTestId, queryByTestId, container, getByText } =
      await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));

    // Clicking 學習經驗 navigates to its own sub-page (hub entries gone).
    fireEvent.click(await findByTestId("manual-entry-learnings"));
    const card = await findByTestId("manual-learnings-card");
    expect(card.textContent).toContain("舊經驗");
    expect(queryByTestId("manual-entry-definition")).toBeNull();
    expect(queryByTestId("manual-assignee-card")).toBeNull();

    const crumbText = container.querySelector(".crumbs")!.textContent!;
    expect(crumbText).toContain("審查 PR");
    expect(crumbText).toContain("學習經驗");

    // The edit affordance carried over — the hand edit still persists.
    fireEvent.click(getByTestId("manual-learnings-edit"));
    fireEvent.change(getByTestId("manual-learnings-input"), {
      target: { value: "## 經驗\n- 新經驗" },
    });
    fireEvent.click(getByTestId("manual-learnings-done"));

    await waitFor(async () => {
      expect((await api.getTaskManual("review-pr")).learnings).toBe(
        "## 經驗\n- 新經驗"
      );
    });

    // The 審查 PR crumb navigates back to the hub.
    fireEvent.click(getByText("審查 PR"));
    await findByTestId("manual-assignee-card");
    expect(queryByTestId("manual-learnings-card")).toBeNull();
    expect(queryByTestId("manual-entry-learnings")).not.toBeNull();
  });

  it("負責成員 editor sets an outsource assignee (chips + segmented + stepper + machine)", async () => {
    __injectMockTaskManual(mkManual({ typeKey: "review-pr" }));
    const { findByTestId, getByTestId } = await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));

    // Honest 未設定 first (the hub summary card).
    expect((await findByTestId("manual-assignee")).textContent).toBe("未設定");

    // 編輯 → the member-panel-style editor expands in place.
    fireEvent.click(getByTestId("manual-assignee-edit"));
    fireEvent.click(getByTestId("manual-assignee-kind-outsource"));
    // Model = the member panel's quick-pick chips (opus is one of them).
    fireEvent.click(getByTestId("manual-assignee-model-opus"));
    // 投入程度 = 低/中/高/最高 segmented.
    fireEvent.click(getByTestId("manual-assignee-effort-high"));
    // 雇用數量 = −/＋ stepper: 1 → 2.
    fireEvent.click(getByTestId("manual-assignee-copies-inc"));
    expect(getByTestId("manual-assignee-copies").textContent).toBe("2");
    // 機器: pick the seed warden machine explicitly.
    fireEvent.click(
      await findByTestId("manual-assignee-machine-warden-mbp5")
    );
    // owner 2026-07-31 (rc-b7d1c642f2d2): ONE verb. This note described a
    // worker being brought online as 啟動, twice — the same act the buttons
    // call 喚醒. Both halves are asserted: changing only one leaves the
    // sentence self-inconsistent and nothing would go red.
    const machineNote =
      document.querySelector(".manual-assignee-editor__note")?.textContent ?? "";
    expect(machineNote).toContain("機器上喚醒");
    expect(machineNote).toContain("一律不喚醒");
    fireEvent.click(getByTestId("manual-assignee-done"));

    await waitFor(async () => {
      expect((await api.getTaskManual("review-pr")).assignee).toEqual({
        kind: "outsource",
        runtime: "claude",
        model: "opus",
        effort: "high",
        copies: 2,
        machine: "warden-mbp5",
      });
    });
    expect((await findByTestId("manual-assignee")).textContent).toContain(
      "外包 · opus · 高"
    );
    expect((await findByTestId("manual-assignee")).textContent).toContain(
      "×2"
    );
  });

  it("無限 copies saves 0 on the wire; the summary shows 無限", async () => {
    __injectMockTaskManual(mkManual({ typeKey: "review-pr" }));
    const { findByTestId, getByTestId } = await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));

    fireEvent.click(await findByTestId("manual-assignee-edit"));
    fireEvent.click(getByTestId("manual-assignee-kind-outsource"));
    fireEvent.click(getByTestId("manual-assignee-copies-unlimited"));
    // The stepper reads ∞ while unlimited is armed.
    expect(getByTestId("manual-assignee-copies").textContent).toBe("∞");
    fireEvent.click(getByTestId("manual-assignee-done"));

    await waitFor(async () => {
      const a = await api.getTaskManual("review-pr");
      expect(a.assignee).toMatchObject({ kind: "outsource", copies: 0 });
    });
    expect((await findByTestId("manual-assignee")).textContent).toContain(
      "無限"
    );
  });

  it("負責成員 editor sets a member assignee (roster pick rows)", async () => {
    __injectMockTaskManual(mkManual({ typeKey: "review-pr" }));
    const { findByTestId, getByTestId } = await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));

    fireEvent.click(await findByTestId("manual-assignee-edit"));
    fireEvent.click(getByTestId("manual-assignee-kind-member"));
    // The roster pick rows list the real assistants (mock Mira) — pick her.
    const members = await api.listMembers();
    const mira = members.find((m) => m.kind === "staff")!;
    fireEvent.click(await findByTestId(`manual-assignee-member-${mira.id}`));
    fireEvent.click(getByTestId("manual-assignee-done"));

    await waitFor(async () => {
      expect((await api.getTaskManual("review-pr")).assignee).toEqual({
        kind: "member",
        memberId: mira.id,
      });
    });
  });

  it("成員 pick row shows the member's role label (i18n-resolved)", async () => {
    __injectMockTaskManual(mkManual({ typeKey: "review-pr" }));
    const { findByTestId, getByTestId } = await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));

    fireEvent.click(await findByTestId("manual-assignee-edit"));
    fireEvent.click(getByTestId("manual-assignee-kind-member"));
    // Mock Mira carries role_key "assistant" → the row's role label resolves
    // through the shared order (i18n seed label first) to 特助.
    const members = await api.listMembers();
    const mira = members.find((m) => m.kind === "staff")!;
    const row = await findByTestId(`manual-assignee-member-${mira.id}`);
    expect(row.textContent).toContain(mira.name);
    expect(
      getByTestId(`manual-assignee-member-role-${mira.id}`).textContent
    ).toBe("特助");
  });

  it("解除設定 unsets the assignee (wire {})", async () => {
    __injectMockTaskManual(
      mkManual({
        typeKey: "review-pr",
        assignee: {
          kind: "outsource",
          model: "opus",
          effort: "high",
          copies: 1,
          machine: "mach-a",
        },
      })
    );
    const { findByTestId, getByTestId } = await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));

    fireEvent.click(await findByTestId("manual-assignee-edit"));
    fireEvent.click(getByTestId("manual-assignee-clear"));

    await waitFor(async () => {
      expect((await api.getTaskManual("review-pr")).assignee).toBeNull();
    });
    expect((await findByTestId("manual-assignee")).textContent).toBe("未設定");
  });
});

describe("設定 › 任務手冊 — deep link (T-e987 任務類型 label 跳轉)", () => {
  it("initialManualKey opens straight on that manual's hub", async () => {
    __injectMockTaskManual(mkManual({ typeKey: "review-pr" }));
    const { findByTestId } = render(
      <I18nProvider>
        <SettingsPage initialManualKey="review-pr" />
      </I18nProvider>
    );
    // The definition/learnings accordion entries only render on the hub.
    expect(await findByTestId("manual-entry-definition")).toBeTruthy();
  });

  it("a stale/unknown key self-heals to the manuals list", async () => {
    // No manual injected → the {kind:"manual"} render falls back to the list,
    // which is honestly empty.
    const { findByTestId } = render(
      <I18nProvider>
        <SettingsPage initialManualKey="gone" />
      </I18nProvider>
    );
    expect(await findByTestId("manuals-empty")).toBeTruthy();
  });
});
describe("設定 › 任務手冊 — 完成編輯 的兩個 await (T-91)", () => {
  // 8. A saved block that the RE-READ could not confirm still counts as saved.
  //    Since T-91 the PATCH answers a receipt, so this page writes and then
  //    re-reads the manual; those are two promises about two different things
  //    and only the first one is the save. When the re-read blips, the block
  //    used to close on 儲存失敗 over an edit the server already had — and the
  //    owner's natural retry wrote it a second time.
  it("a save that lands with a re-read that fails is NOT reported as 儲存失敗", async () => {
    __injectMockTaskManual(mkManual({ typeKey: "review-pr" }));
    const readManual = api.getTaskManual.bind(api);
    const { findByTestId, getByTestId, queryByText, container } =
      await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));
    fireEvent.click(await findByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");

    // From here on EVERY read fails — the manual's own re-read (SettingsPage's
    // `onSave`) and the directory refetch inside `useTaskManuals.update`.
    // Nothing touches the PATCH, which keeps landing.
    let readsBroken = false;
    vi.spyOn(api, "getTaskManual").mockImplementation(async (key) => {
      if (readsBroken) throw mockApiError("read failed", 503, "");
      return readManual(key);
    });
    vi.spyOn(api, "listTaskManuals").mockImplementation(async () => {
      throw mockApiError("read failed", 503, "");
    });

    fireEvent.click(getByTestId("manual-def-edit-1"));
    await findByTestId("manual-purpose-input");
    fireEvent.change(getByTestId("manual-purpose-input"), {
      target: { value: "Review 進來的 Pull Request。" },
    });
    readsBroken = true;
    fireEvent.click(getByTestId("manual-def-done-1"));

    // The block closes the way a SUCCESSFUL save closes it: back to read-only…
    await waitFor(() => expect(getByTestId("manual-def-edit-1")).toBeTruthy());
    // …and the write really did land (asked through the un-mocked store).
    expect(updateManualPatches).toEqual([
      { purpose: "Review 進來的 Pull Request。" },
    ]);
    // …with no save error anywhere on the page.
    expect(queryByText("儲存失敗，請稍後重試")).toBeNull();
    expect(container.querySelector(".set-error")).toBeNull();
  });
});
