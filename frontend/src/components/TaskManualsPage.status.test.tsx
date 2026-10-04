import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskManualDefinitionPage } from "./TaskManualsPage";
import type { TaskManualView } from "../api/adapter";

function mkManual(isSeed: boolean, isDefault: boolean): TaskManualView {
  return {
    typeKey: "tm-status-test",
    displayName: "狀態測試手冊",
    purpose: "",
    fields: [],
    sopMd: "",
    assignee: null,
    updatedTs: 0,
    isSeed,
    isDefault,
    sopMdChars: 0,
    sopMdCapChars: 1000,
  };
}

function renderManual(manual: TaskManualView) {
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

describe("任務手冊預設內容狀態", () => {
  it("marks a built-in manual with no overlay as synced", () => {
    const { getByTestId } = renderManual(mkManual(true, true));
    expect(getByTestId("manual-document-status").textContent).toBe(
      "與預設內容同步"
    );
  });

  it("marks an edited built-in manual as modified", () => {
    const { getByTestId } = renderManual(mkManual(true, false));
    expect(getByTestId("manual-document-status").textContent).toBe("已修改");
  });

  it("does not label a custom manual that has no factory version", () => {
    const { queryByTestId } = renderManual(mkManual(false, false));
    expect(queryByTestId("manual-document-status")).toBeNull();
  });
});
