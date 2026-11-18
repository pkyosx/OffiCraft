import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { SharedDocDetail } from "./SharedDocDetail";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { formatAbsolute } from "../lib/dateFormat";
import type { SharedDocDetailView } from "../types";

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

  it("編輯 then 取消 leaves the text as it was; 編輯 then 儲存 saves it as a new version under the same id", async () => {
    const { getByTestId, queryByTestId } = renderDetail("sd-3f9a2c71b0e4");
    await title(getByTestId);
    fireEvent.click(getByTestId("shared-doc-edit"));
    fireEvent.change(getByTestId("shared-doc-editor"), { target: { value: "discarded" } });
    fireEvent.click(getByTestId("shared-doc-editor-cancel"));
    expect(queryByTestId("shared-doc-editor")).toBeNull();
    expect(getByTestId("shared-doc-markdown").querySelector("h1")?.textContent).toBe("Review Rule");
    fireEvent.click(getByTestId("shared-doc-edit"));
    expect((getByTestId("shared-doc-editor") as HTMLTextAreaElement).value).toBe(
      "# Review Rule\n\n1. 範圍：變更檔案逐一說得出必要性。\n2. 測試：新行為有測試。\n",
    );
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

  it("the pencil renames the doc in place, keeping its content, its versions and who wrote that content", async () => {
    const { getByTestId, getByLabelText } = renderDetail("sd-3f9a2c71b0e4");
    await title(getByTestId);
    fireEvent.click(getByLabelText("改名"));
    fireEvent.change(getByLabelText("改名"), { target: { value: "Review Rule v2" } });
    fireEvent.keyDown(getByLabelText("改名"), { key: "Enter" });
    await waitFor(() => expect(getByTestId("shared-doc-title").textContent).toBe("Review Rule v2"));
    expect(await api.getSharedDoc("sd-3f9a2c71b0e4")).toMatchObject({
      name: "Review Rule v2",
      attachmentId: "att-5d0c11a2b3c4",
      versionCount: 2,
    });
    await expect(api.replaceSharedDoc("sd-3f9a2c71b0e4", { name: " " })).rejects.toMatchObject({
      status: 400,
      serverMessage: "name cannot be blank: omit it to keep the current name",
    });
    await api.replaceSharedDoc("sd-3f9a2c71b0e4", { attachmentId: "att-5d0c11a2b3b0" });
    const [kept] = await api.listSharedDocVersions("sd-3f9a2c71b0e4");
    expect([kept!.attachmentId, kept!.name, kept!.createdBy, kept!.createdTs]).toEqual([
      "att-5d0c11a2b3c4",
      "Review Rule v2",
      "mira",
      1763366700,
    ]);
  });

  it("clicking the description edits it in a two-line box; ✓ saves once however often it is clicked and a blank clears it, ✗ leaves it as it was", async () => {
    const replace = vi.spyOn(api, "replaceSharedDoc");
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
    fireEvent.click(getByTestId("shared-doc-description-save"));
    await waitFor(() => expect(queryByTestId("shared-doc-description-input")).toBeNull());
    expect(getByTestId("shared-doc-description").textContent).toBe("新增規則前先在 #review 頻道討論");
    expect(await api.getSharedDoc("sd-3f9a2c71b0e4")).toMatchObject({
      description: "新增規則前先在 #review 頻道討論",
      attachmentId: "att-5d0c11a2b3c4",
      versionCount: 2,
    });

    fireEvent.click(getByTestId("shared-doc-description"));
    fireEvent.change(getByTestId("shared-doc-description-input"), { target: { value: "  " } });
    fireEvent.click(getByTestId("shared-doc-description-save"));
    await waitFor(() => expect(queryByTestId("shared-doc-description-input")).toBeNull());
    const shown = getByTestId("shared-doc-description");
    expect([shown.textContent, shown.className]).toEqual([
      "加上說明",
      "shared-docs__description shared-docs__muted",
    ]);
    expect((await api.getSharedDoc("sd-3f9a2c71b0e4")).description).toBe("");
    expect(replace.mock.calls).toEqual([
      ["sd-3f9a2c71b0e4", { description: "新增規則前先在 #review 頻道討論" }],
      ["sd-3f9a2c71b0e4", { description: "" }],
    ]);
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

  it("上傳新版本 on a file doc puts the chosen file in, and refuses an image in its place", async () => {
    const upload = vi.spyOn(api, "uploadAttachment");
    const { getByTestId, queryByTestId } = renderDetail("sd-3f9a2c71b0e4");
    await title(getByTestId);
    fireEvent.click(getByTestId("shared-doc-upload-version"));
    fireEvent.change(getByTestId("shared-doc-file-input"), {
      target: { files: [new File(["png"], "photo.png", { type: "image/png" })] },
    });
    fireEvent.click(getByTestId("shared-doc-form-submit"));
    expect(getByTestId("shared-doc-form-error").textContent).toBe(
      "這份不是圖片，不能換成圖片；圖片請另外上傳成一份新檔案",
    );
    expect(upload).not.toHaveBeenCalled();
    fireEvent.change(getByTestId("shared-doc-file-input"), {
      target: { files: [new File(["# v3"], "review-rule.md", { type: "text/markdown" })] },
    });
    expect(queryByTestId("shared-doc-form-error")).toBeNull();
    fireEvent.click(getByTestId("shared-doc-form-submit"));
    await waitFor(() => expect(queryByTestId("shared-doc-replace-form")).toBeNull());
    expect(await api.getSharedDoc("sd-3f9a2c71b0e4")).toMatchObject({ text: "# v3", versionCount: 3 });
  });

  function fileDoc(filename: string, mime: string, text: string | null): SharedDocDetailView {
    return {
      id: "sd-aaaaaaaaaaaa",
      kind: mime.startsWith("image/") ? "image" : "file",
      attachmentId: "att-aaaaaaaaaaaa",
      name: "Fixture",
      description: "",
      filename,
      mime,
      url: "/api/chat/attachment/att-aaaaaaaaaaaa",
      createdTs: 1762900000,
      createdBy: "mira",
      updatedTs: 1762900000,
      updatedBy: "mira",
      versionCount: 1,
      text,
      textOmitted: text === null ? "binary" : null,
    };
  }

  function showDoc(doc: SharedDocDetailView) {
    vi.spyOn(api, "getSharedDoc").mockResolvedValue(doc);
    vi.spyOn(api, "getChatAttachmentShareLink").mockResolvedValue(
      "/api/chat/attachment/att-aaaaaaaaaaaa?sig=s1",
    );
    return renderDetail(doc.id);
  }

  it("a JSON file is indented, a YAML file is shown exactly as stored, a CSV file is a table", async () => {
    const json = showDoc(fileDoc("data.json", "application/json", '{"a":1}'));
    await title(json.getByTestId);
    expect(json.getByTestId("shared-doc-json").textContent).toBe('{\n  "a": 1\n}');
    json.unmount();
    const yaml = showDoc(fileDoc("ci.yaml", "text/plain", "# keep\nkey:  v\n"));
    await title(yaml.getByTestId);
    expect(yaml.getByTestId("shared-doc-yaml").textContent).toBe("# keep\nkey:  v\n");
    yaml.unmount();
    const csv = showDoc(fileDoc("t.csv", "text/csv", "a,b\n1,2\n"));
    await title(csv.getByTestId);
    expect(
      Array.from(csv.getByTestId("shared-doc-csv").querySelectorAll("td")).map((c) => c.textContent),
    ).toEqual(["1", "2"]);
  });

  it("an HTML file is its page in a sandboxed frame at its share link, never at a token address", async () => {
    const { getByTestId } = showDoc(fileDoc("page.html", "text/html", "<p>hi</p>"));
    await title(getByTestId);
    const frame = await waitFor(() => getByTestId("shared-doc-html"));
    expect(frame.tagName).toBe("IFRAME");
    expect(frame.getAttribute("src")).toBe(
      `${window.location.origin}/api/chat/attachment/att-aaaaaaaaaaaa?sig=s1`,
    );
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts allow-popups allow-popups-to-escape-sandbox");
  });

  it("a PDF opens in a new tab at its share link beside 下載, and is not framed", async () => {
    const { getByTestId, container } = showDoc(fileDoc("spec.pdf", "application/pdf", null));
    await title(getByTestId);
    const open = await waitFor(() => getByTestId("shared-doc-pdf").querySelector("a")!);
    expect(open.getAttribute("href")).toBe(
      `${window.location.origin}/api/chat/attachment/att-aaaaaaaaaaaa?sig=s1`,
    );
    expect(open.getAttribute("target")).toBe("_blank");
    expect(container.querySelector("iframe")).toBeNull();
    expect(getByTestId("shared-doc-download").textContent).toBe("下載");
  });

  it("every text file, HTML included, offers 編輯 as plain text; an image or a PDF does not", async () => {
    for (const doc of [
      fileDoc("ci.yaml", "text/plain", "k: v\n"),
      fileDoc("data.json", "application/json", '{"a":1}'),
      fileDoc("feed.xml", "text/xml", "<a/>"),
      fileDoc("t.csv", "text/csv", "a,b\n"),
      fileDoc("page.html", "text/html", "<p>hi</p>"),
    ]) {
      const r = showDoc(doc);
      await title(r.getByTestId);
      fireEvent.click(r.getByTestId("shared-doc-edit"));
      expect(`${doc.filename}: ${(r.getByTestId("shared-doc-editor") as HTMLTextAreaElement).value}`).toBe(
        `${doc.filename}: ${doc.text}`,
      );
      r.unmount();
      vi.restoreAllMocks();
    }
    for (const doc of [fileDoc("a.png", "image/png", null), fileDoc("spec.pdf", "application/pdf", null)]) {
      const r = showDoc(doc);
      await title(r.getByTestId);
      expect(`${doc.filename}: ${r.queryByTestId("shared-doc-edit")}`).toBe(`${doc.filename}: null`);
      r.unmount();
      vi.restoreAllMocks();
    }
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
