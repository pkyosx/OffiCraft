import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { SharedDocDetail } from "./SharedDocDetail";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { formatAbsolute } from "../lib/dateFormat";

function renderDetail(id: string, onBack = () => {}) {
  return render(
    <I18nProvider>
      <SharedDocDetail id={id} actorName={(a) => (a === "mira" ? "Mira" : a)} onBack={onBack} />
    </I18nProvider>,
  );
}

async function title(getByTestId: (id: string) => HTMLElement) {
  return waitFor(() => getByTestId("shared-doc-title"));
}

beforeEach(() => {
  __resetMock();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("SharedDocDetail", () => {
  it("a Markdown doc shows its name, description, last modified, the three buttons and the rendered content", async () => {
    const { getByTestId } = renderDetail("sd-3f9a2c71b0e4");
    expect((await title(getByTestId)).textContent).toBe("Review Rule");
    const now = Math.floor(Date.now() / 1000);
    expect(getByTestId("shared-doc-description").textContent).toBe(
      "PR 審查時逐項核對的規則與退件標準",
    );
    expect(getByTestId("shared-doc-updated").textContent).toBe(
      `最後修改：Mira · ${formatAbsolute(1763366700, now)}`,
    );
    expect(getByTestId("shared-doc-edit").textContent).toBe("編輯");
    expect(getByTestId("shared-doc-upload-version").textContent).toBe("上傳新版本");
    expect(getByTestId("shared-doc-versions").textContent).toBe("版本紀錄");
    expect(getByTestId("shared-doc-markdown").querySelector("h1")?.textContent).toBe("Review Rule");
  });

  it("a link doc shows its URL and offers no 編輯", async () => {
    const { getByTestId, queryByTestId } = renderDetail("sd-0c41e97a2b58");
    await title(getByTestId);
    const link = getByTestId("shared-doc-link") as HTMLAnchorElement;
    expect(link.textContent).toBe("https://github.com/example/officraft/blob/main/spec/openapi.json");
    expect(link.getAttribute("href")).toBe(link.textContent);
    expect(queryByTestId("shared-doc-edit")).toBeNull();
  });

  it("編輯 saves the text as a new version under the same id", async () => {
    const { getByTestId } = renderDetail("sd-3f9a2c71b0e4");
    await title(getByTestId);
    fireEvent.click(getByTestId("shared-doc-edit"));
    fireEvent.change(getByTestId("shared-doc-editor"), { target: { value: "# Review Rule v3\n" } });
    fireEvent.click(getByTestId("shared-doc-editor-save"));
    await waitFor(() =>
      expect(getByTestId("shared-doc-markdown").querySelector("h1")?.textContent).toBe(
        "Review Rule v3",
      ),
    );
    const doc = await api.getSharedDoc("sd-3f9a2c71b0e4");
    expect(doc).toMatchObject({ text: "# Review Rule v3\n", filename: "review-rule.md", versionCount: 3 });
  });

  it("the pencil renames the doc as a new version that keeps the content", async () => {
    const { getByTestId, getByLabelText } = renderDetail("sd-3f9a2c71b0e4");
    await title(getByTestId);
    fireEvent.click(getByLabelText("改名"));
    fireEvent.change(getByLabelText("改名"), { target: { value: "Review Rule v2" } });
    fireEvent.keyDown(getByLabelText("改名"), { key: "Enter" });
    await waitFor(() => expect(getByTestId("shared-doc-title").textContent).toBe("Review Rule v2"));
    expect(await api.getSharedDoc("sd-3f9a2c71b0e4")).toMatchObject({
      name: "Review Rule v2",
      attachmentId: "att-5d0c11a2b3c4",
      versionCount: 3,
    });
  });

  it("clicking the description edits it in a two-line box; ✓ saves, ✗ leaves it as it was", async () => {
    const { getByTestId, queryByTestId } = renderDetail("sd-3f9a2c71b0e4");
    await title(getByTestId);
    fireEvent.click(getByTestId("shared-doc-description"));
    const box = getByTestId("shared-doc-description-input") as HTMLTextAreaElement;
    expect([box.rows, box.value]).toEqual([2, "PR 審查時逐項核對的規則與退件標準"]);
    fireEvent.change(box, { target: { value: "scratch" } });
    fireEvent.click(getByTestId("shared-doc-description-cancel"));
    expect(getByTestId("shared-doc-description").textContent).toBe("PR 審查時逐項核對的規則與退件標準");

    fireEvent.click(getByTestId("shared-doc-description"));
    fireEvent.change(getByTestId("shared-doc-description-input"), {
      target: { value: "新增規則前先在 #review 頻道討論" },
    });
    fireEvent.click(getByTestId("shared-doc-description-save"));
    await waitFor(() => expect(queryByTestId("shared-doc-description-input")).toBeNull());
    expect(getByTestId("shared-doc-description").textContent).toBe("新增規則前先在 #review 頻道討論");
    expect(await api.getSharedDoc("sd-3f9a2c71b0e4")).toMatchObject({
      description: "新增規則前先在 #review 頻道討論",
      attachmentId: "att-5d0c11a2b3c4",
      versionCount: 3,
    });
  });

  it("上傳新版本 on a link doc replaces its URL", async () => {
    const { getByTestId } = renderDetail("sd-0c41e97a2b58");
    await title(getByTestId);
    fireEvent.click(getByTestId("shared-doc-upload-version"));
    fireEvent.change(getByTestId("shared-doc-url-input"), {
      target: { value: "https://example.com/v2" },
    });
    fireEvent.click(getByTestId("shared-doc-form-submit"));
    await waitFor(() => expect(getByTestId("shared-doc-link").textContent).toBe("https://example.com/v2"));
    expect((await api.getSharedDoc("sd-0c41e97a2b58")).versionCount).toBe(2);
  });

  it("an unknown id says the doc was not found", async () => {
    const onBack = vi.fn();
    const { getByTestId } = renderDetail("sd-000000000000", onBack);
    expect((await waitFor(() => getByTestId("shared-doc-not-found"))).textContent).toBe(
      "找不到這個檔案",
    );
    fireEvent.click(getByTestId("shared-doc-back"));
    expect(onBack).toHaveBeenCalledTimes(1);
  });
});
