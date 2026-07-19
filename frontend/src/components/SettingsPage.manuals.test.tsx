// 設定 › 任務手冊 (M3 SPEC §5). Locked here — the acceptance behaviors:
//   1. The settings landing carries the 任務手冊 entry (與角色誌並列); the
//      list starts HONESTLY EMPTY (出廠不含任何類型).
//   2. 新增類型 (T-fa76): the inline row takes a DISPLAY NAME; the system
//      mints the tm- type_key (never the user's text) and the list row shows
//      the display name — the key stays out of the UI.
//   3. The detail is a HUB (owner 2026-07-14): 負責成員 summary card + 任務
//      規劃 accordion cards — clicking 任務定義 / 學習經驗 expands its editor
//      INLINE on the hub (independent toggles); never shows a filename.
//   4. 任務定義 editing (owner T-8a4a r3 — one explicit edit switch for the
//      whole area): READ-ONLY by default; 編輯 flips Q1 purpose text, Q2 field
//      list (add/remove, 必填 toggle, 識別鍵 marking — composite allowed) and
//      Q3 SOP markdown into their editors at once; 完成編輯 persists every
//      change in one PATCH; 取消 discards. (No 重置 — manuals have no seed.)
//   5. 學習經驗 is editable (agent write-back surface, owner-editable too).
//   6. 負責成員 card: member pick or 外包 (model + effort + copies ×N).
//   7. Delete: confirm modal; a type with OPEN tasks survives its 409 with the
//      honest 先讓它們結束 message; a closed-task type deletes.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { SettingsPage } from "./SettingsPage";
import {
  __resetMock,
  __injectMockTask,
  __injectMockTaskManual,
} from "../api/mock";
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

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

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
      sopMd: "",
      learnings: "",
      assignee: null,
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
  it("hub → 任務定義 entry expands the three-section card, READ-ONLY by default (owner T-8a4a r3: explicit edit mode)", async () => {
    __injectMockTaskManual(
      mkManual({
        typeKey: "review-pr",
        purpose: "Review 進來的 PR。",
        fields: [{ name: "PR 連結", required: true, isKey: true }],
        sopMd: "# steps\n1. review",
      })
    );
    const { findByTestId, getByTestId, queryByTestId } =
      await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));

    // The HUB shows the two 任務規劃 entry cards, both collapsed — no card yet.
    await findByTestId("manual-entry-definition");
    await findByTestId("manual-entry-learnings");
    expect(queryByTestId("manual-definition-card")).toBeNull();

    // Click the 任務定義 entry: the three-section card mounts INLINE on the hub
    // (assignee card + both entries stay); learnings stays collapsed.
    fireEvent.click(getByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");
    expect(
      getByTestId("manual-entry-definition").getAttribute("aria-expanded")
    ).toBe("true");
    expect(queryByTestId("manual-assignee-card")).not.toBeNull();
    expect(queryByTestId("manual-learnings-card")).toBeNull();

    // Default is READ-ONLY: the 編輯 switch is shown, no editors are mounted.
    expect(queryByTestId("manual-def-edit")).not.toBeNull();
    expect(queryByTestId("manual-def-done")).toBeNull();
    expect(queryByTestId("manual-purpose-input")).toBeNull();
    expect(queryByTestId("manual-fields-editor")).toBeNull();
    expect(queryByTestId("manual-sop-input")).toBeNull();

    // …and every section renders the stored content as read-only.
    expect(getByTestId("manual-purpose-view").textContent).toBe(
      "Review 進來的 PR。"
    );
    expect(getByTestId("manual-field-view-0").textContent).toContain(
      "PR 連結"
    );
    expect(getByTestId("manual-section-3").textContent).toContain("review");
  });

  it("編輯 → 完成編輯 persists all three sections in one go (owner T-8a4a r3)", async () => {
    __injectMockTaskManual(
      mkManual({
        typeKey: "review-pr",
        fields: [{ name: "PR 連結", required: true, isKey: true }],
      })
    );
    const { findByTestId, getByTestId } = await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));
    fireEvent.click(await findByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");

    // Enter edit mode: the three editors mount, seeded from the manual.
    fireEvent.click(getByTestId("manual-def-edit"));
    await findByTestId("manual-purpose-input");
    expect((getByTestId("manual-field-name-0") as HTMLInputElement).value).toBe(
      "PR 連結"
    );

    // §1 purpose, §2 a second composite field, §3 SOP — all edited in one pass.
    fireEvent.change(getByTestId("manual-purpose-input"), {
      target: { value: "Review 進來的 Pull Request。" },
    });
    fireEvent.click(getByTestId("manual-field-add"));
    fireEvent.change(getByTestId("manual-field-name-1"), {
      target: { value: "repo" },
    });
    fireEvent.click(getByTestId("manual-field-required-1")); // 選填 → 必填
    fireEvent.click(getByTestId("manual-field-key-1")); // 🔑識別鍵 (複合)
    fireEvent.change(getByTestId("manual-sop-input"), {
      target: { value: "# steps\n1. review" },
    });

    // Nothing saved until 完成編輯.
    expect((await api.getTaskManual("review-pr")).purpose).toBe("");

    fireEvent.click(getByTestId("manual-def-done"));
    await waitFor(async () => {
      const m = await api.getTaskManual("review-pr");
      expect(m.purpose).toBe("Review 進來的 Pull Request。");
      expect(m.fields).toEqual([
        { name: "PR 連結", required: true, isKey: true },
        { name: "repo", required: true, isKey: true },
      ]);
      expect(m.sopMd).toBe("# steps\n1. review");
    });
    // Back to read-only after saving.
    await waitFor(() => expect(getByTestId("manual-def-edit")).toBeTruthy());
  });

  it("編輯 → 取消 discards the drafts and saves nothing (owner T-8a4a r3)", async () => {
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

    fireEvent.click(getByTestId("manual-def-edit"));
    fireEvent.change(await findByTestId("manual-purpose-input"), {
      target: { value: "改壞的草稿" },
    });
    fireEvent.click(getByTestId("manual-field-remove-0")); // delete the field
    fireEvent.click(getByTestId("manual-def-cancel"));

    // Read-only again, showing the ORIGINAL content — nothing persisted.
    expect(queryByTestId("manual-purpose-input")).toBeNull();
    expect(getByTestId("manual-purpose-view").textContent).toBe("原本的用途");
    expect(getByTestId("manual-field-view-0").textContent).toContain(
      "PR 連結"
    );
    const m = await api.getTaskManual("review-pr");
    expect(m.purpose).toBe("原本的用途");
    expect(m.fields).toEqual([{ name: "PR 連結", required: true, isKey: true }]);
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
    // …and there is no per-section SOP 編輯 toggle any more (owner T-8a4a r3):
    // the ONE card-level 編輯 switch owns the SOP editor too.
    expect(queryByTestId("manual-sop-edit")).toBeNull();
    expect(queryByTestId("manual-def-edit")).not.toBeNull();
    fireEvent.click(getByTestId("manual-def-edit"));
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
    fireEvent.click(getByTestId("manual-def-edit"));

    // Mark 識別鍵 on a not-yet-required field → 必填 must auto-turn on, so the
    // committed payload carries required:true (never the isKey && !required
    // combo the server 400s).
    fireEvent.click(await findByTestId("manual-field-key-0"));
    await waitFor(() =>
      expect(
        getByTestId("manual-field-required-0").getAttribute("aria-pressed")
      ).toBe("true")
    );
    fireEvent.click(getByTestId("manual-def-done"));
    await waitFor(async () =>
      expect((await api.getTaskManual("review-pr")).fields).toEqual([
        { name: "PR 連結", required: true, isKey: true },
      ])
    );

    // Clearing 必填 also clears 識別鍵 (a key can't be optional).
    fireEvent.click(await findByTestId("manual-def-edit"));
    fireEvent.click(getByTestId("manual-field-required-0"));
    await waitFor(() =>
      expect(
        getByTestId("manual-field-key-0").getAttribute("aria-pressed")
      ).toBe("false")
    );
    fireEvent.click(getByTestId("manual-def-done"));
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
    fireEvent.click(getByTestId("manual-def-edit"));
    fireEvent.click(await findByTestId("manual-field-remove-1"));
    fireEvent.click(getByTestId("manual-def-done"));
    await waitFor(async () => {
      expect((await api.getTaskManual("review-pr")).fields).toEqual([
        { name: "PR 連結", required: true, isKey: true },
      ]);
    });
  });

  it("學習經驗 entry expands the learnings card inline (owner hand edit); both cards can be open together", async () => {
    __injectMockTaskManual(
      mkManual({ typeKey: "review-pr", learnings: "## 經驗\n- 舊經驗" })
    );
    const { findByTestId, getByTestId, queryByTestId } =
      await renderManualsList();
    fireEvent.click(await findByTestId("manual-open-review-pr"));
    // Open definition first, then learnings — the two accordions are
    // independent, so both editors coexist inline on the hub.
    fireEvent.click(await findByTestId("manual-entry-definition"));
    await findByTestId("manual-definition-card");
    fireEvent.click(await findByTestId("manual-entry-learnings"));

    const card = await findByTestId("manual-learnings-card");
    expect(card.textContent).toContain("舊經驗");
    // Definition stayed open — the toggles are independent.
    expect(queryByTestId("manual-definition-card")).not.toBeNull();

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
    // 投入程度 = 低/中/高 segmented.
    fireEvent.click(getByTestId("manual-assignee-effort-high"));
    // 雇用數量 = −/＋ stepper: 1 → 2.
    fireEvent.click(getByTestId("manual-assignee-copies-inc"));
    expect(getByTestId("manual-assignee-copies").textContent).toBe("2");
    // 機器: pick the seed warden machine explicitly.
    fireEvent.click(
      await findByTestId("manual-assignee-machine-warden-mbp5")
    );
    fireEvent.click(getByTestId("manual-assignee-done"));

    await waitFor(async () => {
      expect((await api.getTaskManual("review-pr")).assignee).toEqual({
        kind: "outsource",
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
    const mira = members.find((m) => m.kind === "assistant")!;
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
    const mira = members.find((m) => m.kind === "assistant")!;
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
          machine: "auto",
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
