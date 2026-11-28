import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { SharedDocsPage } from "./SharedDocsPage";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { ApiError } from "../api/errors";
import { codeForStatus } from "../api/errorCodes";

const copyTextMock = vi.hoisted(() => vi.fn(async (_text: string) => true));
vi.mock("../lib/clipboard", () => ({ copyText: copyTextMock }));

const WIRE_DOC = {
  id: "sd-7d03c9e4f1a5",
  kind: "file",
  attachment_id: "att-000000000007",
  name: "舊版發版流程",
  description: "",
  filename: "release.md",
  mime: "text/markdown",
  url: "/api/chat/attachment/att-000000000007",
  created_ts: 100,
  created_by: "mira",
  updated_ts: 100,
  updated_by: "mira",
  version_count: 1,
};

function fakeJwt(claims: Record<string, unknown>): string {
  const b64 = btoa(JSON.stringify(claims)).replace(/=+$/, "");
  return `e30.${b64}.sig`;
}

/** The page in REAL backend mode, so the delete button is decided by the real
 * viewerIsOwner reading the stored token — mock mode always answers owner. */
async function renderRealModeRow(token: string): Promise<HTMLElement> {
  vi.stubEnv("VITE_USE_MOCK", "false");
  vi.stubEnv("VITE_OC_TOKEN", "");
  localStorage.setItem("oc_token", token);
  vi.stubGlobal(
    "EventSource",
    class {
      readyState = 0;
      onopen = null;
      onerror = null;
      onmessage = null;
      addEventListener() {}
      removeEventListener() {}
      close() {}
    },
  );
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request | string) => {
      const url = new URL(typeof input === "string" ? input : input.url, window.location.origin);
      const body =
        url.pathname === "/api/shared-docs"
          ? { docs: [WIRE_DOC] }
          : {};
      return new Response(JSON.stringify(body), {
        status: url.pathname === "/api/shared-docs" ? 200 : 404,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
  vi.resetModules();
  const { I18nProvider: RealI18n } = await import("../i18n");
  const { SharedDocsPage: RealPage } = await import("./SharedDocsPage");
  const { USE_MOCK } = await import("../api");
  expect(USE_MOCK).toBe(false);
  const { container } = render(
    <RealI18n>
      <RealPage />
    </RealI18n>,
  );
  const [row] = await waitRows(container, 1);
  return row!;
}

function renderPage() {
  return render(
    <I18nProvider>
      <SharedDocsPage />
    </I18nProvider>,
  );
}

async function createUploaded(kind: "file" | "image", name: string, filename: string, body: string) {
  const attachmentId = await api.uploadAttachment(new Blob([body]), filename);
  return api.createSharedDoc({ kind, name, description: "", attachmentId });
}

function actionStates(row: HTMLElement): string[][] {
  return Array.from(row.querySelectorAll<HTMLButtonElement>(".shared-docs__actions button")).map((b) => [
    b.dataset.testid ?? "",
    b.title,
    b.getAttribute("aria-disabled") ?? "",
  ]);
}

function chooseAdd(item: "upload" | "link") {
  fireEvent.click(within(document.body).getByTestId("shared-docs-add-menu"));
  fireEvent.click(within(document.body).getByTestId(`shared-docs-add-menu-${item}`));
}

function pickFile(container: HTMLElement, file: File) {
  const input = container.querySelector<HTMLInputElement>('[data-testid="shared-docs-file-input"]')!;
  fireEvent.change(input, { target: { files: [file] } });
}

function rows(container: HTMLElement): HTMLElement[] {
  return Array.from(container.querySelectorAll<HTMLElement>('[data-testid="shared-doc-row"]'));
}

async function waitRows(container: HTMLElement, n: number): Promise<HTMLElement[]> {
  return waitFor(() => {
    const got = rows(container);
    if (got.length !== n) throw new Error(`want ${n} rows, have ${got.length}`);
    return got;
  });
}

function openEditFromPreview(row: HTMLElement) {
  fireEvent.click(within(row).getByTestId("shared-doc-open"));
  fireEvent.click(within(document.body).getByRole("button", { name: "編輯" }));
}

function cellTexts(row: HTMLElement): string[] {
  return Array.from(row.children).map((cell) => cell.textContent ?? "");
}

beforeEach(() => {
  __resetMock();
  copyTextMock.mockClear();
  window.location.hash = "#shared-docs";
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  vi.resetModules();
  localStorage.clear();
});

describe("SharedDocsPage", () => {
  it("lists every doc as its own card in the list, no table: the short ID, the kind as an icon named by its tooltip and the actions on the first line, the name with the description under it, then 建立者 and 最後更新 as label/value rows", async () => {
    vi.spyOn(Date, "now").mockReturnValue((1763366700 + 3 * 86400 + 7200) * 1000);
    const boardId = await api.createSharedDoc({
      kind: "link",
      name: "Board",
      description: "",
      url: "https://example.com/board",
    });
    const shotId = await createUploaded("image", "Shot", "shot.png", "png");
    const contractId = await createUploaded("file", "Contract", "contract.pdf", "pdf");
    const notesId = await createUploaded("file", "Notes", "notes.txt", "plain");
    const listMembers = vi.spyOn(api, "listMembers");
    const { container } = renderPage();
    await waitRows(container, 6);
    const row = (id: string) => container.querySelector<HTMLElement>(`[data-doc-id="${id}"]`)!;
    const api規格 = row("sd-0c41e97a2b58");
    const review = row("sd-3f9a2c71b0e4");
    expect(container.querySelector("table")).toBeNull();
    const list = within(container).getByRole("list");
    expect(Array.from(list.children)).toEqual(rows(container));
    for (const card of rows(container)) {
      expect(card.getAttribute("role")).toBe("listitem");
      expect(card.classList.contains("lore-row")).toBe(true);
    }
    expect(cellTexts(api規格)).toEqual([
      "#sd-0c41e9",
      "API 規格GitHub 上的 spec/openapi.json，改 wire 前先讀",
      "建立者Mira最後更新22 天前 · Mira",
    ]);
    const creator = (id: string) => {
      const chip = within(row(id)).getByTestId("shared-doc-creator");
      return [chip.textContent, chip.querySelectorAll(".avatar").length];
    };
    await waitFor(() => expect(creator("sd-3f9a2c71b0e4")).toEqual(["Mira", 1]));
    expect(creator(boardId)).toEqual(["CEO（你）", 1]);
    expect(listMembers).toHaveBeenCalledTimes(1);
    expect(cellTexts(review)).toEqual([
      "#sd-3f9a2c",
      "Review RulePR 審查時逐項核對的規則與退件標準",
      "建立者Mira最後更新3 天前 · Mira",
    ]);
    const head = (id: string) =>
      Array.from(row(id).firstElementChild!.querySelectorAll("[data-testid]")).map((el) => el.getAttribute("data-testid"));
    const actionIds = ["shared-doc-edit-info", "shared-doc-upload", "shared-doc-delete"];
    expect(head("sd-3f9a2c71b0e4")).toEqual(["shared-doc-id", "shared-doc-kind-file", ...actionIds]);
    expect(head("sd-0c41e97a2b58")).toEqual(["shared-doc-id", "shared-doc-kind-link", ...actionIds]);
    expect(head(shotId)).toEqual(["shared-doc-id", "shared-doc-kind-image", ...actionIds]);
    const kind = (id: string, k: string) => {
      const chip = row(id).querySelector<HTMLElement>(`[data-testid="shared-doc-kind-${k}"]`)!;
      return [chip.textContent, chip.getAttribute("role"), chip.getAttribute("aria-label"), chip.title];
    };
    expect(kind(shotId, "image")).toEqual(["", "img", "圖片", "圖片"]);
    expect(kind("sd-0c41e97a2b58", "link")).toEqual(["", "img", "連結", "連結"]);
    expect(kind("sd-3f9a2c71b0e4", "file")).toEqual(["", "img", "檔案", "檔案"]);
    const block = within(review).getByTestId("shared-doc-name");
    expect(block.querySelector('[data-testid^="shared-doc-kind-"]')).toBeNull();
    expect(Array.from(block.querySelectorAll(".shared-docs__file-text > span")).map((s) => s.textContent)).toEqual([
      "Review Rule",
      "PR 審查時逐項核對的規則與退件標準",
    ]);
    expect(
      Array.from(
        within(row(boardId)).getByTestId("shared-doc-name").querySelectorAll(".shared-docs__file-text > span"),
      ).map((s) => s.textContent),
    ).toEqual(["Board"]);
    expect(row(shotId).querySelector("img")).toBeNull();
    const fileActions = [
      ["shared-doc-edit-info", "編輯名稱與說明", ""],
      ["shared-doc-upload", "上傳新版本", ""],
      ["shared-doc-delete", "永久刪除", ""],
    ];
    for (const id of ["sd-3f9a2c71b0e4", notesId, shotId, contractId]) expect(actionStates(row(id))).toEqual(fileActions);
    const linkActions = [
      ["shared-doc-edit-info", "編輯連結", ""],
      ["shared-doc-upload", "連結的網址請用「編輯連結」修改", "true"],
      ["shared-doc-delete", "永久刪除", ""],
    ];
    expect(actionStates(api規格)).toEqual(linkActions);
    expect(actionStates(row(boardId))).toEqual(linkActions);
    const pencil = (id: string) => {
      const b = within(row(id)).getByTestId("shared-doc-edit-info");
      return [b.getAttribute("aria-label"), b.title];
    };
    expect([pencil("sd-3f9a2c71b0e4"), pencil(shotId), pencil("sd-0c41e97a2b58")]).toEqual([
      ["編輯名稱與說明 Review Rule", "編輯名稱與說明"],
      ["編輯名稱與說明 Shot", "編輯名稱與說明"],
      ["編輯連結 API 規格", "編輯連結"],
    ]);
  });

  it("the ID shows six characters after sd- and its one button copies the full id and shows a check", async () => {
    const { container } = renderPage();
    const [, review] = await waitRows(container, 2);
    const badge = within(review!).getByTestId("shared-doc-id");
    const iconPaths = () => Array.from(badge.querySelectorAll("svg path")).map((p) => p.getAttribute("d"));
    expect(iconPaths()).toEqual(["m12 2 9 5-9 5-9-5 9-5Z", "m3 12 9 5 9-5", "m3 17 9 5 9-5"]);
    fireEvent.click(badge);
    await waitFor(() => expect(copyTextMock).toHaveBeenCalledWith("sd-3f9a2c71b0e4"));
    await waitFor(() => expect(iconPaths()).toEqual(["M20 6 9 17l-5-5"]));
    expect([badge.textContent, badge.getAttribute("aria-label")]).toEqual([
      "#sd-3f9a2c",
      "複製檔案 ID sd-3f9a2c71b0e4",
    ]);
    expect(
      Array.from(review!.querySelectorAll<HTMLButtonElement>(".lore-row__badges button")).map((b) => b.dataset.testid),
    ).toEqual(["shared-doc-id"]);
  });

  it("the name opens the content itself, a link in a new tab and a file or image in the preview over the list with the preview's own actions plus 編輯 on any text file, and 最後更新 opens the doc's version history over the list, a restore there showing on the row", async () => {
    vi.spyOn(Date, "now").mockReturnValue((1763366700 + 3 * 86400 + 7200) * 1000);
    const contractId = await createUploaded("file", "Contract", "contract.pdf", "pdf");
    const shotId = await createUploaded("image", "Shot", "shot.png", "png");
    const configId = await createUploaded("file", "Config", "ci.yaml", "# keep\nkey:  v\n");
    const configDoc = await api.getSharedDoc(configId);
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) =>
        new Response(url.startsWith(configDoc.url) ? "# keep\nkey:  v\n" : "", { status: 200 }),
      ),
    );
    const { container } = renderPage();
    await waitRows(container, 5);
    const row = (id: string) => container.querySelector<HTMLElement>(`[data-doc-id="${id}"]`)!;
    const actions = (overlay: HTMLElement) =>
      Array.from(overlay.querySelectorAll(".md-preview__actions > *")).map((el) => el.getAttribute("aria-label"));
    const openPreview = async (id: string) => {
      const name = within(row(id)).getByTestId("shared-doc-open");
      name.focus();
      fireEvent.click(name);
      return waitFor(() => {
        const el = document.body.querySelector<HTMLElement>(".md-preview");
        if (!el) throw new Error("no preview");
        return el;
      });
    };
    const closePreview = async (overlay: HTMLElement) => {
      fireEvent.click(overlay.querySelector(".md-preview__close")!);
      await waitFor(() => expect(document.body.querySelector(".md-preview")).toBeNull());
    };

    const link = within(row("sd-0c41e97a2b58")).getByTestId("shared-doc-open");
    expect([link.tagName, link.getAttribute("href"), link.getAttribute("target"), link.getAttribute("rel")]).toEqual([
      "A",
      "https://github.com/example/officraft/blob/main/spec/openapi.json",
      "_blank",
      "noopener noreferrer",
    ]);

    const review = await openPreview("sd-3f9a2c71b0e4");
    expect(review.querySelector(".md-preview__title")?.textContent).toBe("Review Rule");
    expect(review.querySelector("a.md-preview__download")?.getAttribute("href")).toContain(
      "/api/chat/attachment/att-5d0c11a2b3c4",
    );
    await waitFor(() =>
      expect(actions(review)).toEqual(["編輯", "複製分享連結", "在新頁面顯示", "下載", "關閉預覽"]),
    );
    fireEvent.click(within(review).getByRole("button", { name: "編輯" }));
    expect(document.body.querySelector(".md-preview:not([data-testid])")).toBeNull();
    const editor = (await waitFor(() => within(document.body).getByTestId("shared-doc-editor"))) as HTMLTextAreaElement;
    expect(editor.value).toBe("# Review Rule\n\n1. 範圍：變更檔案逐一說得出必要性。\n2. 測試：新行為有測試。\n");
    fireEvent.click(within(document.body).getByTestId("shared-doc-editor-cancel"));
    expect(document.body.querySelector(".md-preview")).toBeNull();
    expect(document.activeElement).toBe(within(row("sd-3f9a2c71b0e4")).getByTestId("shared-doc-open"));

    const config = await openPreview(configId);
    await waitFor(() => expect(config.querySelector("pre.md-preview__text")?.textContent).toBe("# keep\nkey:  v\n"));
    await waitFor(() =>
      expect(actions(config)).toEqual(["編輯", "複製分享連結", "在新頁面顯示", "下載", "關閉預覽"]),
    );
    fireEvent.click(within(config).getByRole("button", { name: "編輯" }));
    expect(
      ((await waitFor(() => within(document.body).getByTestId("shared-doc-editor"))) as HTMLTextAreaElement).value,
    ).toBe("# keep\nkey:  v\n");
    fireEvent.click(within(document.body).getByTestId("shared-doc-editor-cancel"));
    await waitFor(() => expect(document.body.querySelector(".md-preview")).toBeNull());

    const contract = await openPreview(contractId);
    expect(contract.querySelector(".md-preview__title")?.textContent).toBe("Contract");
    await waitFor(() => expect(actions(contract)).toEqual(["複製分享連結", "在新頁面顯示", "下載", "關閉預覽"]));
    const contractDoc = await api.getSharedDoc(contractId);
    expect(contract.querySelector("a.md-preview__download")?.getAttribute("href")).toContain(contractDoc.url);
    await closePreview(contract);

    const shot = await openPreview(shotId);
    expect(shot.querySelector(".md-preview__title")?.textContent).toBe("Shot");
    await waitFor(() => expect(actions(shot)).toEqual(["複製分享連結", "在新頁面顯示", "下載", "關閉預覽"]));
    const shotDoc = await api.getSharedDoc(shotId);
    expect(shot.querySelector("img")?.getAttribute("src")).toContain(shotDoc.url);
    await closePreview(shot);
    expect(window.location.hash).toBe("#shared-docs");

    const updated = within(row("sd-3f9a2c71b0e4")).getByTestId("shared-doc-updated");
    expect([updated.tagName, updated.getAttribute("aria-label"), updated.title]).toEqual([
      "BUTTON",
      "3 天前 · Mira · 查看版本紀錄",
      "查看版本紀錄",
    ]);
    fireEvent.click(updated);
    fireEvent.click(await waitFor(() => within(document.body).getByTestId("sd-versions-open-1")));
    fireEvent.click(await waitFor(() => within(document.body).getByTestId("doc-history-modal-restore")));
    fireEvent.click(within(document.body).getByTestId("doc-history-restore-confirm-btn"));
    await waitFor(() => expect(document.body.querySelector("[data-testid='sd-versions-list']")).toBeNull());
    const restored = await api.getSharedDoc("sd-3f9a2c71b0e4");
    expect(restored.attachmentId).toBe("att-5d0c11a2b3b0");
    await waitFor(() =>
      expect(within(row("sd-3f9a2c71b0e4")).getByTestId("shared-doc-updated").textContent).toBe(
        "1 分鐘前 · CEO（你）",
      ),
    );
    expect(window.location.hash).toBe("#shared-docs");
  });

  it("a member's token gets no 永久刪除", async () => {
    const member = await renderRealModeRow(fakeJwt({ scope: "agent", sub: "kip" }));
    expect(actionStates(member)).toEqual([
      ["shared-doc-edit-info", "編輯名稱與說明", ""],
      ["shared-doc-upload", "上傳新版本", ""],
    ]);
  });

  it("an owner token is offered 永久刪除", async () => {
    const ownerRow = await renderRealModeRow(fakeJwt({ scope: "owner", sub: "owner" }));
    expect(within(ownerRow).getByTestId("shared-doc-delete").getAttribute("title")).toBe("永久刪除");
  });

  it("永久刪除 asks first, then removes the doc for good, the count left of ＋ 新增 following", async () => {
    const { container, getByTestId } = renderPage();
    const [, review] = await waitRows(container, 2);
    expect(getByTestId("shared-docs-count").textContent).toBe("2 份檔案");
    fireEvent.click(within(review!).getByTestId("shared-doc-delete"));
    expect(getByTestId("shared-doc-delete-confirm").textContent).toContain(
      "要永久刪除「Review Rule」嗎？檔案與所有版本都會刪除，無法復原。",
    );
    fireEvent.click(getByTestId("shared-doc-delete-confirm-btn"));
    await waitRows(container, 1);
    await waitFor(() => expect(getByTestId("shared-docs-count").textContent).toBe("1 份檔案"));
    expect((await api.listSharedDocs()).map((d) => d.id)).toEqual(["sd-0c41e97a2b58"]);
  });

  it("＋ 新增 opens a menu of 上傳檔案 and 新增連結; 上傳檔案 opens the file picker, and the picked file gets a dialog for its name, starting as the filename, and description that 儲存 uploads as a new doc with both, a refused save staying open with the server's message", async () => {
    const upload = vi.spyOn(api, "uploadAttachment");
    const create = vi.spyOn(api, "createSharedDoc");
    const { container, getByTestId, queryByTestId, getByRole, getAllByRole, queryByRole } = renderPage();
    await waitRows(container, 2);
    const trigger = getByTestId("shared-docs-add-menu");
    expect([trigger.textContent, trigger.getAttribute("aria-label"), trigger.getAttribute("aria-expanded")]).toEqual([
      "+新增",
      "新增",
      "false",
    ]);
    fireEvent.click(trigger);
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    expect(getByRole("menu", { name: "新增" }).textContent).toBe("上傳檔案新增連結");
    expect(getAllByRole("menuitem").map((item) => item.textContent)).toEqual(["上傳檔案", "新增連結"]);
    const input = container.querySelector<HTMLInputElement>('[data-testid="shared-docs-file-input"]')!;
    const openPicker = vi.spyOn(input, "click");
    fireEvent.click(getByTestId("shared-docs-add-menu-upload"));
    expect(openPicker).toHaveBeenCalledTimes(1);
    expect([queryByRole("menu"), queryByTestId("shared-doc-info-dialog")]).toEqual([null, null]);

    const file = new File(["# Notes"], "release-notes.md", { type: "text/markdown" });
    pickFile(container, file);
    const dialog = await waitFor(() => getByTestId("shared-doc-info-dialog"));
    expect(Array.from(dialog.querySelectorAll(".secrets__field")).map((f) => f.textContent)).toEqual([
      "檔案release-notes.md",
      "名稱",
      "說明",
    ]);
    expect(dialog.querySelector(".secrets__form-title")?.textContent).toBe("上傳檔案");
    expect(upload).not.toHaveBeenCalled();
    const nameInput = getByTestId("shared-doc-info-name") as HTMLInputElement;
    expect([nameInput.value, nameInput.maxLength, document.activeElement === nameInput]).toEqual(["release-notes", 128, true]);
    expect((getByTestId("shared-doc-info-description") as HTMLTextAreaElement).value).toBe("");
    const tooLong = "名".repeat(130);
    fireEvent.change(nameInput, { target: { value: tooLong } });
    fireEvent.change(getByTestId("shared-doc-info-description"), { target: { value: " 每次發版前更新 " } });
    fireEvent.click(getByTestId("shared-doc-info-save"));
    expect((await waitFor(() => getByTestId("shared-doc-info-error"))).textContent).toBe(
      "file name is 130 chars, over the 128-char limit",
    );
    expect((getByTestId("shared-doc-info-name") as HTMLInputElement).value).toBe(tooLong);
    expect(rows(container)).toHaveLength(2);
    fireEvent.change(nameInput, { target: { value: "Release notes" } });
    fireEvent.click(getByTestId("shared-doc-info-save"));
    await waitRows(container, 3);
    const created = container.querySelector<HTMLElement>(`[data-doc-id="${await create.mock.results[1]!.value}"]`);
    expect(queryByTestId("shared-doc-info-dialog")).toBeNull();
    expect(upload.mock.calls).toEqual([[file, "release-notes.md"]]);
    const attachmentId = await upload.mock.results[0]!.value;
    expect(create.mock.calls).toEqual([
      [{ kind: "file", name: tooLong, description: "每次發版前更新", attachmentId }],
      [{ kind: "file", name: "Release notes", description: "每次發版前更新", attachmentId }],
    ]);
    expect(cellTexts(created!)[1]).toBe("Release notes每次發版前更新");
    expect(await api.getSharedDoc(created!.dataset.docId!)).toMatchObject({
      name: "Release notes",
      description: "每次發版前更新",
      versionCount: 1,
      text: "# Notes",
    });
    expect(window.location.hash).toBe("#shared-docs");

    chooseAdd("upload");
    pickFile(container, new File(["png"], "shot.png", { type: "image/png" }));
    expect(((await waitFor(() => getByTestId("shared-doc-info-name"))) as HTMLInputElement).value).toBe("shot");
    fireEvent.click(getByTestId("shared-doc-info-cancel"));
    expect(queryByTestId("shared-doc-info-dialog")).toBeNull();
    expect([upload.mock.calls.length, create.mock.calls.length, rows(container).length]).toEqual([1, 2, 3]);
    chooseAdd("upload");
    pickFile(container, new File(["png"], "shot.png", { type: "image/png" }));
    fireEvent.keyDown(await waitFor(() => getByTestId("shared-doc-info-name")), { key: "Escape" });
    expect(queryByTestId("shared-doc-info-dialog")).toBeNull();
    await new Promise((r) => setTimeout(r, 0));
    expect([upload.mock.calls.length, create.mock.calls.length, rows(container).length]).toEqual([1, 2, 3]);
    chooseAdd("upload");
    pickFile(container, new File(["png"], "shot.png", { type: "image/png" }));
    fireEvent.click(await waitFor(() => getByTestId("shared-doc-info-save")));
    await waitRows(container, 4);
    expect(create.mock.calls[2]).toEqual([
      { kind: "image", name: "shot", description: "", attachmentId: await upload.mock.results[1]!.value },
    ]);
  });

  it("新增連結 opens a dialog for the URL, name and description that 儲存 adds as a link, a blank name taking the URL's host and last segment, and 取消 adds nothing", async () => {
    const create = vi.spyOn(api, "createSharedDoc");
    const { container, getByTestId, queryByTestId } = renderPage();
    await waitRows(container, 2);
    const addLink = (url: string, description = "") => {
      chooseAdd("link");
      fireEvent.change(getByTestId("shared-doc-info-url"), { target: { value: url } });
      if (description) fireEvent.change(getByTestId("shared-doc-info-description"), { target: { value: description } });
      fireEvent.click(getByTestId("shared-doc-info-save"));
    };
    chooseAdd("link");
    const dialog = getByTestId("shared-doc-info-dialog");
    expect(dialog.querySelector(".secrets__form-title")?.textContent).toBe("新增連結");
    expect(
      Array.from(dialog.querySelectorAll(".secrets__field")).map((f) => [
        f.querySelector(".secrets__label")?.textContent,
        f.querySelector<HTMLInputElement>("input, textarea")?.placeholder,
      ]),
    ).toEqual([
      ["網址", "https://"],
      ["名稱", "名稱（留空就從網址取）"],
      ["說明", ""],
    ]);
    expect(Array.from(dialog.querySelectorAll("button")).map((b) => b.textContent)).toEqual(["取消", "儲存"]);
    expect(document.activeElement).toBe(getByTestId("shared-doc-info-url"));
    const save = getByTestId("shared-doc-info-save") as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    fireEvent.change(getByTestId("shared-doc-info-url"), { target: { value: "https://example.com/draft" } });
    fireEvent.change(getByTestId("shared-doc-info-description"), { target: { value: "草稿" } });
    fireEvent.click(getByTestId("shared-doc-info-cancel"));
    expect(queryByTestId("shared-doc-info-dialog")).toBeNull();
    expect([create.mock.calls.length, rows(container).length]).toEqual([0, 2]);
    chooseAdd("link");
    fireEvent.change(getByTestId("shared-doc-info-url"), { target: { value: "https://example.com/draft" } });
    fireEvent.keyDown(getByTestId("shared-doc-info-url"), { key: "Escape" });
    expect(queryByTestId("shared-doc-info-dialog")).toBeNull();
    await new Promise((r) => setTimeout(r, 0));
    expect([create.mock.calls.length, rows(container).length]).toEqual([0, 2]);

    chooseAdd("link");
    expect((getByTestId("shared-doc-info-url") as HTMLInputElement).value).toBe("");
    fireEvent.change(getByTestId("shared-doc-info-url"), { target: { value: "https://example.com/board" } });
    fireEvent.change(getByTestId("shared-doc-info-name"), { target: { value: "Board" } });
    fireEvent.change(getByTestId("shared-doc-info-description"), { target: { value: " 每週排程看板 " } });
    fireEvent.click(getByTestId("shared-doc-info-save"));
    await waitRows(container, 3);
    const board = container.querySelector<HTMLElement>(`[data-doc-id="${await create.mock.results[0]!.value}"]`);
    expect(queryByTestId("shared-doc-info-dialog")).toBeNull();
    expect(create.mock.calls[0]).toEqual([
      { kind: "link", name: "Board", description: "每週排程看板", url: "https://example.com/board" },
    ]);
    expect(cellTexts(board!)[1]).toBe("Board每週排程看板");

    addLink("https://example.com/wiki", "團隊 wiki 首頁");
    await waitRows(container, 4);
    expect(create.mock.calls[1]).toEqual([
      { kind: "link", name: "example.com/wiki", description: "團隊 wiki 首頁", url: "https://example.com/wiki" },
    ]);

    const docsUrl = `https://docs.google.com/document/d/${"1aB2cD3eF4gH5iJ6kL7mN8oP9qR0sT".repeat(4)}/edit?usp=sharing&ouid=${"9".repeat(21)}`;
    expect(docsUrl.length).toBeGreaterThan(128);
    addLink(docsUrl);
    await waitRows(container, 5);
    expect((await api.listSharedDocs()).find((d) => d.url === docsUrl)?.name).toBe("docs.google.com/edit");

    const longSegment = "季度規劃".repeat(40);
    const encodedUrl = `https://drive.example.com/files/${encodeURIComponent(longSegment)}`;
    addLink(encodedUrl);
    await waitRows(container, 6);
    expect((await api.listSharedDocs()).find((d) => d.url === encodedUrl)?.name).toBe(
      `drive.example.com/${longSegment}`.slice(0, 128),
    );
    expect(window.location.hash).toBe("#shared-docs");
    expect(create.mock.calls.map(([input]) => input.description)).toEqual(["每週排程看板", "團隊 wiki 首頁", "", ""]);
  });

  it("a 新增連結 the server refuses keeps the dialog open with the server's message and what was typed", async () => {
    vi.spyOn(api, "createSharedDoc").mockRejectedValueOnce(
      new ApiError("http 400", 400, "validation_error", "url must start with https:// or http://"),
    );
    const { container, getByTestId } = renderPage();
    await waitRows(container, 2);
    chooseAdd("link");
    fireEvent.change(getByTestId("shared-doc-info-url"), { target: { value: "not a url" } });
    fireEvent.click(getByTestId("shared-doc-info-save"));
    expect((await waitFor(() => getByTestId("shared-doc-info-error"))).textContent).toBe(
      "url must start with https:// or http://",
    );
    expect((getByTestId("shared-doc-info-url") as HTMLInputElement).value).toBe("not a url");
    expect(rows(container)).toHaveLength(2);
  });

  it("上傳新版本 on a row puts the picked file in as that doc's new version, the row busy until its own upload ends", async () => {
    const otherId = await createUploaded("file", "Other", "other.md", "x");
    const realUpload = api.uploadAttachment.bind(api);
    const gates: Array<() => void> = [];
    vi.spyOn(api, "uploadAttachment").mockImplementation(async (file, filename) => {
      await new Promise<void>((open) => gates.push(open));
      return realUpload(file, filename);
    });
    const { container } = renderPage();
    await waitRows(container, 3);
    const row = (id: string) => container.querySelector<HTMLElement>(`[data-doc-id="${id}"]`)!;
    const rowUpload = within(row("sd-3f9a2c71b0e4")).getByTestId("shared-doc-upload");
    const otherUpload = within(row(otherId)).getByTestId("shared-doc-upload");
    fireEvent.click(rowUpload);
    pickFile(container, new File(["# Review Rule v3"], "review-rule.md", { type: "text/markdown" }));
    fireEvent.click(otherUpload);
    pickFile(container, new File(["y"], "other.md", { type: "text/markdown" }));
    await waitFor(() => expect(gates).toHaveLength(2));
    expect([rowUpload.getAttribute("aria-disabled"), otherUpload.getAttribute("aria-disabled")]).toEqual(["true", "true"]);
    gates[1]!();
    await waitFor(() => expect(otherUpload.getAttribute("aria-disabled")).toBeNull());
    expect(rowUpload.getAttribute("aria-disabled")).toBe("true");
    gates[0]!();
    await waitFor(async () =>
      expect(await api.getSharedDoc("sd-3f9a2c71b0e4")).toMatchObject({ text: "# Review Rule v3", versionCount: 3 }),
    );
    await waitFor(() => expect(rowUpload.getAttribute("aria-disabled")).toBeNull());
    expect(window.location.hash).toBe("#shared-docs");
    expect(rows(container)).toHaveLength(3);
  });

  it("上傳新版本 refuses a file that would change the doc between image and not, and uploads nothing", async () => {
    const shotId = await createUploaded("image", "Shot", "shot.png", "png");
    const upload = vi.spyOn(api, "uploadAttachment");
    const { container, getByTestId, queryByTestId } = renderPage();
    await waitRows(container, 3);
    const row = (id: string) => container.querySelector<HTMLElement>(`[data-doc-id="${id}"]`)!;
    fireEvent.click(within(row(shotId)).getByTestId("shared-doc-upload"));
    expect(container.querySelector<HTMLInputElement>('[data-testid="shared-docs-file-input"]')!.accept).toBe(
      "image/*",
    );
    pickFile(container, new File(["# x"], "notes.md", { type: "text/markdown" }));
    expect((await waitFor(() => getByTestId("shared-docs-action-error"))).textContent).toBe(
      "這份是圖片，新版本也要是圖片",
    );
    fireEvent.click(within(row("sd-3f9a2c71b0e4")).getByTestId("shared-doc-upload"));
    expect(queryByTestId("shared-docs-action-error")).toBeNull();
    pickFile(container, new File(["png"], "photo.png", { type: "image/png" }));
    expect((await waitFor(() => getByTestId("shared-docs-action-error"))).textContent).toBe(
      "這份不是圖片，不能換成圖片；圖片請另外上傳成一份新檔案",
    );
    expect(upload).not.toHaveBeenCalled();
    expect((await api.getSharedDoc(shotId)).versionCount).toBe(1);
    expect((await api.getSharedDoc("sd-3f9a2c71b0e4")).versionCount).toBe(2);
  });

  it("an upload that fails changes nothing and says why: a new version's on the list, a new file's in its dialog, which stays open", async () => {
    vi.spyOn(api, "uploadAttachment").mockRejectedValue(
      new ApiError("http 400", 400, "validation_error", "attachment exceeds the 100 MB size limit"),
    );
    const { container, getByTestId, queryByTestId } = renderPage();
    const [, review] = await waitRows(container, 2);
    fireEvent.click(within(review!).getByTestId("shared-doc-upload"));
    pickFile(container, new File(["x"], "review-rule.md", { type: "text/markdown" }));
    expect((await waitFor(() => getByTestId("shared-docs-action-error"))).textContent).toBe(
      "attachment exceeds the 100 MB size limit",
    );
    chooseAdd("upload");
    pickFile(container, new File(["x"], "new.md", { type: "text/markdown" }));
    fireEvent.click(await waitFor(() => getByTestId("shared-doc-info-save")));
    expect((await waitFor(() => getByTestId("shared-doc-info-error"))).textContent).toBe(
      "attachment exceeds the 100 MB size limit",
    );
    expect([(getByTestId("shared-doc-info-name") as HTMLInputElement).value, queryByTestId("shared-docs-action-error")]).toEqual([
      "new",
      null,
    ]);
    expect(rows(container)).toHaveLength(2);
    expect((await api.getSharedDoc("sd-3f9a2c71b0e4")).versionCount).toBe(2);
  });

  it("a link's dimmed 上傳新版本 does nothing, by click or by Enter/Space", async () => {
    const { container, queryByTestId } = renderPage();
    await waitRows(container, 2);
    const input = container.querySelector<HTMLInputElement>('[data-testid="shared-docs-file-input"]')!;
    const openPicker = vi.spyOn(input, "click");
    const button = within(container.querySelector<HTMLElement>('[data-doc-id="sd-0c41e97a2b58"]')!).getByTestId(
      "shared-doc-upload",
    );
    expect(button.getAttribute("aria-disabled")).toBe("true");
    button.focus();
    expect(document.activeElement).toBe(button);
    for (const key of ["Enter", " "]) {
      fireEvent.keyDown(button, { key });
      fireEvent.keyUp(button, { key });
    }
    fireEvent.click(button);
    expect(openPicker).not.toHaveBeenCalled();
    expect(window.location.hash).toBe("#shared-docs");
    expect(queryByTestId("shared-docs-action-error")).toBeNull();
  });

  it("the pencil on the first line opens a dialog for its name and description, plus the URL on a link, without opening the file: 儲存 is off for a blank name and sends only what this dialog changed, a refused save stays open with the server's message, 取消 and Esc leave it, focus goes back to the pencil", async () => {
    const replace = vi.spyOn(api, "replaceSharedDoc");
    const { container, getByTestId, queryByTestId } = renderPage();
    const [apiSpec, review] = await waitRows(container, 2);
    const open = (row: HTMLElement) => {
      const pencil = within(row).getByTestId("shared-doc-edit-info");
      pencil.focus();
      fireEvent.click(pencil);
      return pencil;
    };
    const fields = () =>
      Array.from(getByTestId("shared-doc-info-dialog").querySelectorAll<HTMLInputElement | HTMLTextAreaElement>("input, textarea")).map(
        (el) => [el.closest("label")?.querySelector(".secrets__label")?.textContent, el.value],
      );

    let pencil = open(review!);
    expect(document.body.querySelector(".md-preview")).toBeNull();
    expect(getByTestId("shared-doc-info-dialog").querySelector(".secrets__form-title")?.textContent).toBe("編輯名稱與說明");
    expect(fields()).toEqual([
      ["名稱", "Review Rule"],
      ["說明", "PR 審查時逐項核對的規則與退件標準"],
    ]);
    fireEvent.change(getByTestId("shared-doc-info-name"), { target: { value: "dropped" } });
    fireEvent.click(getByTestId("shared-doc-info-cancel"));
    expect(queryByTestId("shared-doc-info-dialog")).toBeNull();
    expect(document.activeElement).toBe(pencil);

    pencil = open(review!);
    expect(fields()[0]).toEqual(["名稱", "Review Rule"]);
    fireEvent.keyDown(getByTestId("shared-doc-info-description"), { key: "Escape" });
    expect(queryByTestId("shared-doc-info-dialog")).toBeNull();
    expect(document.activeElement).toBe(pencil);

    pencil = open(review!);
    fireEvent.click(getByTestId("shared-doc-info-save"));
    expect(queryByTestId("shared-doc-info-dialog")).toBeNull();
    expect(replace).not.toHaveBeenCalled();

    pencil = open(review!);
    fireEvent.change(getByTestId("shared-doc-info-name"), { target: { value: "  " } });
    expect((getByTestId("shared-doc-info-save") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.submit(getByTestId("shared-doc-info-dialog"));
    expect(replace).not.toHaveBeenCalled();
    fireEvent.change(getByTestId("shared-doc-info-name"), { target: { value: "Review Rule v2" } });
    fireEvent.change(getByTestId("shared-doc-info-description"), { target: { value: "  新的說明 " } });
    fireEvent.click(getByTestId("shared-doc-info-save"));
    await waitFor(() => expect(cellTexts(review!)[1]).toBe("Review Rule v2新的說明"));
    expect(queryByTestId("shared-doc-info-dialog")).toBeNull();
    expect(document.activeElement).toBe(pencil);
    expect(replace).toHaveBeenLastCalledWith("sd-3f9a2c71b0e4", { name: "Review Rule v2", description: "新的說明" });
    expect(await api.getSharedDoc("sd-3f9a2c71b0e4")).toMatchObject({ name: "Review Rule v2", versionCount: 2 });

    open(review!);
    await api.replaceSharedDoc("sd-3f9a2c71b0e4", { name: "Renamed by member" });
    await waitFor(() => expect(cellTexts(review!)[1]).toBe("Renamed by member新的說明"));
    fireEvent.change(getByTestId("shared-doc-info-description"), { target: { value: "我只改說明" } });
    fireEvent.click(getByTestId("shared-doc-info-save"));
    await waitFor(() => expect(queryByTestId("shared-doc-info-dialog")).toBeNull());
    expect(replace).toHaveBeenLastCalledWith("sd-3f9a2c71b0e4", { description: "我只改說明" });
    expect(await api.getSharedDoc("sd-3f9a2c71b0e4")).toMatchObject({ name: "Renamed by member", description: "我只改說明" });

    const gates: Array<() => void> = [];
    replace.mockImplementationOnce(async () => {
      await new Promise<void>((release) => gates.push(release));
      throw new ApiError("http 400", 400, codeForStatus(400), "url must be http or https");
    });
    open(apiSpec!);
    expect(getByTestId("shared-doc-info-dialog").querySelector(".secrets__form-title")?.textContent).toBe("編輯連結");
    expect(fields()).toEqual([
      ["名稱", "API 規格"],
      ["說明", "GitHub 上的 spec/openapi.json，改 wire 前先讀"],
      ["網址", "https://github.com/example/officraft/blob/main/spec/openapi.json"],
    ]);
    fireEvent.change(getByTestId("shared-doc-info-url"), { target: { value: "ftp://example.com/openapi.json" } });
    fireEvent.click(getByTestId("shared-doc-info-save"));
    await waitFor(() => expect(gates).toHaveLength(1));
    fireEvent.keyDown(getByTestId("shared-doc-info-url"), { key: "Escape" });
    fireEvent.submit(getByTestId("shared-doc-info-dialog"));
    expect([replace.mock.calls.length, (getByTestId("shared-doc-info-save") as HTMLButtonElement).disabled]).toEqual([4, true]);
    gates[0]!();
    expect((await waitFor(() => getByTestId("shared-doc-info-error"))).textContent).toBe("url must be http or https");
    expect((getByTestId("shared-doc-info-url") as HTMLInputElement).value).toBe("ftp://example.com/openapi.json");
    fireEvent.change(getByTestId("shared-doc-info-url"), { target: { value: "https://example.com/openapi.json" } });
    fireEvent.click(getByTestId("shared-doc-info-save"));
    await waitFor(() => expect(queryByTestId("shared-doc-info-dialog")).toBeNull());
    expect(replace).toHaveBeenLastCalledWith("sd-0c41e97a2b58", { url: "https://example.com/openapi.json" });
    expect(await api.getSharedDoc("sd-0c41e97a2b58")).toMatchObject({
      name: "API 規格",
      url: "https://example.com/openapi.json",
      versionCount: 2,
    });
    expect(window.location.hash).toBe("#shared-docs");
  });

  it("編輯 in a text file's preview opens its content in an editor over the list that a click beside it leaves open: 取消, × and Esc close it unsaved, 儲存 puts it in as a new version, focus going back to the name each time", async () => {
    const original = "# Review Rule\n\n1. 範圍：變更檔案逐一說得出必要性。\n2. 測試：新行為有測試。\n";
    const { container, getByTestId, queryByTestId } = renderPage();
    const [, review] = await waitRows(container, 2);
    const edit = within(review!).getByTestId("shared-doc-open");
    const openEditor = async () => {
      edit.focus();
      fireEvent.click(edit);
      fireEvent.click(within(document.body).getByRole("button", { name: "編輯" }));
      expect(document.activeElement).toBe(getByTestId("shared-doc-edit-overlay"));
      return (await waitFor(() => getByTestId("shared-doc-editor"))) as HTMLTextAreaElement;
    };

    let editor = await openEditor();
    const overlay = getByTestId("shared-doc-edit-overlay");
    expect([overlay.parentElement, overlay.getAttribute("aria-label"), overlay.querySelector(".md-preview__title")?.textContent]).toEqual([
      document.body,
      "Review Rule",
      "Review Rule",
    ]);
    expect(editor.value).toBe(original);
    expect(document.activeElement).toBe(editor);
    fireEvent.change(editor, { target: { value: "draft one" } });
    fireEvent.click(getByTestId("shared-doc-editor-cancel"));
    expect(queryByTestId("shared-doc-edit-overlay")).toBeNull();
    expect(document.activeElement).toBe(edit);

    editor = await openEditor();
    expect(editor.value).toBe(original);
    fireEvent.change(editor, { target: { value: "draft two" } });
    fireEvent.click(getByTestId("shared-doc-edit-overlay"));
    expect(editor.value).toBe("draft two");
    fireEvent.click(getByTestId("shared-doc-edit-overlay").querySelector(".md-preview__close")!);
    expect(queryByTestId("shared-doc-edit-overlay")).toBeNull();
    expect(document.activeElement).toBe(edit);

    editor = await openEditor();
    fireEvent.change(editor, { target: { value: "draft three" } });
    fireEvent.keyDown(editor, { key: "Escape" });
    expect(queryByTestId("shared-doc-edit-overlay")).toBeNull();
    expect(document.activeElement).toBe(edit);
    expect(await api.getSharedDoc("sd-3f9a2c71b0e4")).toMatchObject({ text: original, versionCount: 2 });

    editor = await openEditor();
    expect(editor.value).toBe(original);
    fireEvent.change(editor, { target: { value: "# Review Rule v3\n" } });
    const reads = vi.spyOn(api, "getSharedDoc");
    await api.replaceSharedDoc("sd-3f9a2c71b0e4", { description: "別人剛改的說明" });
    await waitFor(() => expect(reads).toHaveBeenCalledWith("sd-3f9a2c71b0e4"));
    await waitFor(() => expect(cellTexts(review!)[1]).toBe("Review Rule別人剛改的說明"));
    expect((getByTestId("shared-doc-editor") as HTMLTextAreaElement).value).toBe("# Review Rule v3\n");
    fireEvent.click(getByTestId("shared-doc-editor-save"));
    await waitFor(() => expect(queryByTestId("shared-doc-edit-overlay")).toBeNull());
    expect(document.activeElement).toBe(edit);
    expect(await api.getSharedDoc("sd-3f9a2c71b0e4")).toMatchObject({
      text: "# Review Rule v3\n",
      filename: "review-rule.md",
      versionCount: 3,
    });
    await waitFor(() =>
      expect(within(review!).getByTestId("shared-doc-updated").textContent).toBe(
        "1 分鐘前 · CEO（你）",
      ),
    );
    expect(window.location.hash).toBe("#shared-docs");
  });

  it("a save the server refuses keeps the editor open with the server's message and what was typed, and Esc waits for the save", async () => {
    const gates: Array<() => void> = [];
    const realUpload = api.uploadAttachment.bind(api);
    vi.spyOn(api, "uploadAttachment")
      .mockImplementationOnce(async () => {
        await new Promise<void>((open) => gates.push(open));
        throw new ApiError("http 400", 400, codeForStatus(400), "attachment exceeds the 100 MB size limit");
      })
      .mockImplementation(realUpload);
    const { container, getByTestId } = renderPage();
    const [, review] = await waitRows(container, 2);
    openEditFromPreview(review!);
    const editor = (await waitFor(() => getByTestId("shared-doc-editor"))) as HTMLTextAreaElement;
    fireEvent.change(editor, { target: { value: "# kept\n" } });
    fireEvent.click(getByTestId("shared-doc-editor-save"));
    await waitFor(() => expect(gates).toHaveLength(1));
    fireEvent.keyDown(editor, { key: "Escape" });
    expect((getByTestId("shared-doc-editor-save") as HTMLButtonElement).disabled).toBe(true);
    gates[0]!();
    expect((await waitFor(() => getByTestId("shared-doc-edit-error"))).textContent).toBe(
      "attachment exceeds the 100 MB size limit",
    );
    expect((getByTestId("shared-doc-editor") as HTMLTextAreaElement).value).toBe("# kept\n");
    expect((getByTestId("shared-doc-editor-save") as HTMLButtonElement).disabled).toBe(false);
    expect((await api.getSharedDoc("sd-3f9a2c71b0e4")).versionCount).toBe(2);
    fireEvent.click(getByTestId("shared-doc-editor-save"));
    await waitFor(() => expect(document.body.querySelector('[data-testid="shared-doc-edit-overlay"]')).toBeNull());
    expect(await api.getSharedDoc("sd-3f9a2c71b0e4")).toMatchObject({ text: "# kept\n", versionCount: 3 });
  });

  it("編輯 on a text file that cannot be edited here shows why with the file's name, a 下載 and an 上傳新版本 that replaces it like the row's button, saying too large only when it is", async () => {
    const real = await api.getSharedDoc("sd-3f9a2c71b0e4");
    vi.spyOn(api, "getSharedDoc")
      .mockResolvedValueOnce({ ...real, text: null, textOmitted: "too_large" })
      .mockResolvedValueOnce({ ...real, text: null, textOmitted: "binary" });
    const { container, getByTestId, queryByTestId } = renderPage();
    const [, review] = await waitRows(container, 2);
    const card = async () => {
      openEditFromPreview(review!);
      const el = await waitFor(() => getByTestId("shared-doc-edit-unavailable"));
      const download = getByTestId("shared-doc-download");
      expect([download.textContent, download.getAttribute("download")]).toEqual(["下載", "review-rule.md"]);
      expect(download.getAttribute("href")).toContain("/api/chat/attachment/att-5d0c11a2b3c4");
      expect(queryByTestId("shared-doc-editor")).toBeNull();
      expect(document.activeElement).toBe(getByTestId("shared-doc-edit-overlay"));
      return Array.from(el.children).map((child) => child.textContent);
    };
    expect(await card()).toEqual([
      "",
      "這份檔案太大，無法在網頁上編輯",
      "review-rule.md",
      "下載後在電腦上修改，再用「上傳新版本」換上去。",
      "下載上傳新版本",
    ]);
    fireEvent.click(getByTestId("shared-doc-edit-overlay").querySelector(".md-preview__close")!);
    expect((await card())[1]).toBe("這種檔案不能在網頁上編輯");

    const input = container.querySelector<HTMLInputElement>('[data-testid="shared-docs-file-input"]')!;
    const openPicker = vi.spyOn(input, "click");
    fireEvent.click(getByTestId("shared-doc-edit-upload"));
    expect(queryByTestId("shared-doc-edit-overlay")).toBeNull();
    expect(openPicker).toHaveBeenCalledTimes(1);
    pickFile(container, new File(["# smaller"], "review-rule.md", { type: "text/markdown" }));
    await waitFor(async () =>
      expect(await api.listSharedDocs()).toEqual(
        expect.arrayContaining([expect.objectContaining({ id: "sd-3f9a2c71b0e4", versionCount: 3 })]),
      ),
    );
  });

  it("編輯 on a doc that cannot be read shows why in the overlay: gone says not found, any other failure gives the server's message", async () => {
    vi.spyOn(api, "getSharedDoc")
      .mockRejectedValueOnce(new ApiError("http 404", 404, codeForStatus(404), "shared doc not found"))
      .mockRejectedValueOnce(new ApiError("http 500", 500, codeForStatus(500), "database is locked"));
    const { container, getByTestId } = renderPage();
    const [, review] = await waitRows(container, 2);
    const status = () => getByTestId("shared-doc-edit-overlay").querySelector(".md-preview__status")?.textContent;
    openEditFromPreview(review!);
    await waitFor(() => expect(status()).toBe("找不到這個檔案"));
    fireEvent.click(getByTestId("shared-doc-edit-overlay").querySelector(".md-preview__close")!);
    openEditFromPreview(review!);
    await waitFor(() => expect(status()).toBe("database is locked"));
  });
});
