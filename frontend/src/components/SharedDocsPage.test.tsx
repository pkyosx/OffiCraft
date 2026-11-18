import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { SharedDocsPage, sharedDocLink } from "./SharedDocsPage";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { formatAbsolute } from "../lib/dateFormat";
import { ApiError } from "../api/errors";

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

function cellTexts(row: HTMLElement): string[] {
  return Array.from(row.querySelectorAll("td")).map((td) => td.textContent ?? "");
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
  it("lists every doc under ID, 檔案, 最後修改 and 操作, the file block showing the type, the name and the description under it", async () => {
    const boardId = await api.createSharedDoc({
      kind: "link",
      name: "Board",
      description: "",
      url: "https://example.com/board",
    });
    const shotId = await createUploaded("image", "Shot", "shot.png", "png");
    const contractId = await createUploaded("file", "Contract", "contract.pdf", "pdf");
    const notesId = await createUploaded("file", "Notes", "notes.txt", "plain");
    const { container } = renderPage();
    await waitRows(container, 6);
    const row = (id: string) => container.querySelector<HTMLElement>(`[data-doc-id="${id}"]`)!;
    const api規格 = row("sd-0c41e97a2b58");
    const review = row("sd-3f9a2c71b0e4");
    const now = Math.floor(Date.now() / 1000);
    expect(Array.from(container.querySelectorAll("thead th")).map((th) => th.textContent)).toEqual([
      "ID",
      "檔案",
      "最後修改",
      "操作",
    ]);
    expect(cellTexts(api規格)).toEqual([
      "#sd-0c41e97a2b58",
      "API 規格GitHub 上的 spec/openapi.json，改 wire 前先讀",
      `Mira · ${formatAbsolute(1761648240, now)}`,
      "",
    ]);
    expect(cellTexts(review)).toEqual([
      "#sd-3f9a2c71b0e4",
      "Review RulePR 審查時逐項核對的規則與退件標準",
      `Mira · ${formatAbsolute(1763366700, now)}`,
      "",
    ]);
    const block = within(review).getByTestId("shared-doc-name");
    expect(block.querySelector('[data-testid="shared-doc-kind-file"]')).not.toBeNull();
    expect(Array.from(block.querySelectorAll(".shared-docs__file-text > span")).map((s) => s.textContent)).toEqual([
      "Review Rule",
      "PR 審查時逐項核對的規則與退件標準",
    ]);
    expect(
      within(api規格).getByTestId("shared-doc-name").querySelector('[data-testid="shared-doc-kind-link"]'),
    ).not.toBeNull();
    expect(
      Array.from(
        within(row(boardId)).getByTestId("shared-doc-name").querySelectorAll(".shared-docs__file-text > span"),
      ).map((s) => s.textContent),
    ).toEqual(["Board"]);
    expect(row(shotId).querySelector('[data-testid="shared-doc-kind-image"]')?.getAttribute("title")).toBe("圖片");
    expect(row(shotId).querySelector("img")).toBeNull();
    expect(actionStates(review)).toEqual([
      ["shared-doc-edit", "編輯", ""],
      ["shared-doc-upload", "上傳新版本", ""],
      ["shared-doc-delete", "永久刪除", ""],
    ]);
    expect(actionStates(row(notesId))).toEqual(actionStates(review));
    const noEdit = [
      ["shared-doc-edit", "這種檔案不能直接編輯，請上傳新版本", "true"],
      ["shared-doc-upload", "上傳新版本", ""],
      ["shared-doc-delete", "永久刪除", ""],
    ];
    expect(actionStates(row(shotId))).toEqual(noEdit);
    expect(actionStates(row(contractId))).toEqual(noEdit);
    const linkActions = [
      ["shared-doc-edit", "修改網址", ""],
      ["shared-doc-upload", "連結請用編輯改網址", "true"],
      ["shared-doc-delete", "永久刪除", ""],
    ];
    expect(actionStates(api規格)).toEqual(linkActions);
    expect(actionStates(row(boardId))).toEqual(linkActions);
  });

  it("the ID badge copies the id and shows a check, the link button copies the in-site link and shows a check", async () => {
    const { container } = renderPage();
    const [, review] = await waitRows(container, 2);
    const badge = within(review!).getByTestId("shared-doc-id");
    const iconPaths = () => Array.from(badge.querySelectorAll("svg path")).map((p) => p.getAttribute("d"));
    expect(iconPaths()).toEqual(["m12 2 9 5-9 5-9-5 9-5Z", "m3 12 9 5 9-5", "m3 17 9 5 9-5"]);
    fireEvent.click(badge);
    await waitFor(() => expect(copyTextMock).toHaveBeenCalledWith("sd-3f9a2c71b0e4"));
    await waitFor(() => expect(iconPaths()).toEqual(["M20 6 9 17l-5-5"]));
    expect(badge.textContent).toBe("#sd-3f9a2c71b0e4");
    const link = within(review!).getByTestId("shared-doc-copy-link");
    fireEvent.click(link);
    await waitFor(() =>
      expect(copyTextMock).toHaveBeenLastCalledWith(
        `${window.location.origin}${window.location.pathname}#shared-docs/sd-3f9a2c71b0e4`,
      ),
    );
    await waitFor(() => expect(link.getAttribute("title")).toBe("已複製連結"));
    expect(sharedDocLink("sd-1")).toBe(
      `${window.location.origin}${window.location.pathname}#shared-docs/sd-1`,
    );
  });

  it("the name opens the content itself, a link in a new tab and a file or image in the preview over the list, and 最後修改 opens the doc's page", async () => {
    const contractId = await createUploaded("file", "Contract", "contract.pdf", "pdf");
    const shotId = await createUploaded("image", "Shot", "shot.png", "png");
    const { container } = renderPage();
    await waitRows(container, 4);
    const row = (id: string) => container.querySelector<HTMLElement>(`[data-doc-id="${id}"]`)!;
    const openPreview = async (id: string) => {
      fireEvent.click(within(row(id)).getByTestId("shared-doc-open"));
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
    await closePreview(review);

    const contract = await openPreview(contractId);
    expect(contract.querySelector(".md-preview__title")?.textContent).toBe("Contract");
    const contractDoc = await api.getSharedDoc(contractId);
    expect(contract.querySelector("a.md-preview__download")?.getAttribute("href")).toContain(contractDoc.url);
    await closePreview(contract);

    const shot = await openPreview(shotId);
    expect(shot.querySelector(".md-preview__title")?.textContent).toBe("Shot");
    const shotDoc = await api.getSharedDoc(shotId);
    expect(shot.querySelector("img")?.getAttribute("src")).toContain(shotDoc.url);
    await closePreview(shot);
    expect(window.location.hash).toBe("#shared-docs");

    const updated = within(row("sd-3f9a2c71b0e4")).getByTestId("shared-doc-updated");
    const now = Math.floor(Date.now() / 1000);
    expect([updated.tagName, updated.textContent, updated.getAttribute("aria-label"), updated.title]).toEqual([
      "BUTTON",
      `Mira · ${formatAbsolute(1763366700, now)}`,
      `Mira · ${formatAbsolute(1763366700, now)} · 查看版本紀錄`,
      "查看版本紀錄",
    ]);
    fireEvent.click(updated);
    expect(window.location.hash).toBe("#shared-docs/sd-3f9a2c71b0e4");
  });

  it("a member's token gets no 永久刪除", async () => {
    const member = await renderRealModeRow(fakeJwt({ scope: "agent", sub: "kip" }));
    expect(actionStates(member)).toEqual([
      ["shared-doc-edit", "編輯", ""],
      ["shared-doc-upload", "上傳新版本", ""],
    ]);
  });

  it("an owner token is offered 永久刪除", async () => {
    const ownerRow = await renderRealModeRow(fakeJwt({ scope: "owner", sub: "owner" }));
    expect(within(ownerRow).getByTestId("shared-doc-delete").getAttribute("title")).toBe("永久刪除");
  });

  it("永久刪除 asks first, then removes the doc for good", async () => {
    const { container, getByTestId } = renderPage();
    const [, review] = await waitRows(container, 2);
    fireEvent.click(within(review!).getByTestId("shared-doc-delete"));
    expect(getByTestId("shared-doc-delete-confirm").textContent).toContain(
      "要永久刪除「Review Rule」嗎？檔案與所有版本都會刪除，無法復原。",
    );
    fireEvent.click(getByTestId("shared-doc-delete-confirm-btn"));
    await waitRows(container, 1);
    expect((await api.listSharedDocs()).map((d) => d.id)).toEqual(["sd-0c41e97a2b58"]);
  });

  it("上傳檔案 adds the picked file as a new doc named after it, with that name open to edit", async () => {
    const upload = vi.spyOn(api, "uploadAttachment");
    const { container, getByTestId, queryByTestId } = renderPage();
    await waitRows(container, 2);
    fireEvent.click(getByTestId("shared-docs-upload"));
    const file = new File(["# Notes"], "release-notes.md", { type: "text/markdown" });
    pickFile(container, file);
    await waitRows(container, 3);
    expect(upload).toHaveBeenCalledWith(file, "release-notes.md");
    const created = (await api.listSharedDocs()).find((d) => d.filename === "release-notes.md")!;
    expect(created).toMatchObject({ kind: "file", name: "release-notes", description: "", versionCount: 1 });
    const row = container.querySelector<HTMLElement>(`[data-doc-id="${created.id}"]`)!;
    const nameInput = within(row).getByLabelText("改名") as HTMLInputElement;
    expect(nameInput.value).toBe("release-notes");
    expect(nameInput.maxLength).toBe(128);
    const tooLong = "名".repeat(130);
    fireEvent.change(nameInput, { target: { value: tooLong } });
    fireEvent.keyDown(nameInput, { key: "Enter" });
    expect((await waitFor(() => getByTestId("shared-docs-action-error"))).textContent).toBe(
      "shared doc name is 130 chars, over the 128-char limit",
    );
    expect((within(row).getByLabelText("改名") as HTMLInputElement).value).toBe(tooLong);
    fireEvent.change(nameInput, { target: { value: "Release notes" } });
    fireEvent.keyDown(nameInput, { key: "Enter" });
    await waitFor(() => expect(cellTexts(row)[1]).toBe("Release notes"));
    expect(within(row).queryByLabelText("改名")).toBeNull();
    expect(queryByTestId("shared-docs-action-error")).toBeNull();
    expect(await api.getSharedDoc(created.id)).toMatchObject({ name: "Release notes", versionCount: 1, text: "# Notes" });
    expect(window.location.hash).toBe("#shared-docs");

    pickFile(container, new File(["png"], "shot.png", { type: "image/png" }));
    await waitRows(container, 4);
    expect((await api.listSharedDocs()).find((d) => d.filename === "shot.png")).toMatchObject({
      kind: "image",
      name: "shot",
    });
  });

  it("新增連結 adds a link from the URL and name typed on the page, a blank name taking the URL's host and last segment", async () => {
    const { container, getByTestId, queryByTestId } = renderPage();
    await waitRows(container, 2);
    fireEvent.click(getByTestId("shared-docs-add-link"));
    const save = getByTestId("shared-docs-link-save") as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    fireEvent.change(getByTestId("shared-docs-link-url"), { target: { value: "https://example.com/board" } });
    fireEvent.change(getByTestId("shared-docs-link-name"), { target: { value: "Board" } });
    fireEvent.click(save);
    await waitRows(container, 3);
    expect(queryByTestId("shared-docs-link-form")).toBeNull();
    expect((await api.listSharedDocs()).find((d) => d.name === "Board")).toMatchObject({
      kind: "link",
      url: "https://example.com/board",
      description: "",
    });

    fireEvent.click(getByTestId("shared-docs-add-link"));
    fireEvent.change(getByTestId("shared-docs-link-url"), { target: { value: "https://example.com/wiki" } });
    fireEvent.submit(getByTestId("shared-docs-link-form"));
    await waitRows(container, 4);
    expect((await api.listSharedDocs()).find((d) => d.url === "https://example.com/wiki")?.name).toBe(
      "example.com/wiki",
    );

    const docsUrl = `https://docs.google.com/document/d/${"1aB2cD3eF4gH5iJ6kL7mN8oP9qR0sT".repeat(4)}/edit?usp=sharing&ouid=${"9".repeat(21)}`;
    expect(docsUrl.length).toBeGreaterThan(128);
    fireEvent.click(getByTestId("shared-docs-add-link"));
    fireEvent.change(getByTestId("shared-docs-link-url"), { target: { value: docsUrl } });
    fireEvent.submit(getByTestId("shared-docs-link-form"));
    await waitRows(container, 5);
    expect((await api.listSharedDocs()).find((d) => d.url === docsUrl)?.name).toBe("docs.google.com/edit");

    const longSegment = "季度規劃".repeat(40);
    const encodedUrl = `https://drive.example.com/files/${encodeURIComponent(longSegment)}`;
    fireEvent.click(getByTestId("shared-docs-add-link"));
    fireEvent.change(getByTestId("shared-docs-link-url"), { target: { value: encodedUrl } });
    fireEvent.submit(getByTestId("shared-docs-link-form"));
    await waitRows(container, 6);
    expect((await api.listSharedDocs()).find((d) => d.url === encodedUrl)?.name).toBe(
      `drive.example.com/${longSegment}`.slice(0, 128),
    );
    expect(window.location.hash).toBe("#shared-docs");
  });

  it("a 新增連結 the server refuses keeps the form open with the server's message", async () => {
    vi.spyOn(api, "createSharedDoc").mockRejectedValueOnce(
      new ApiError("http 400", 400, "validation_error", "url must be http or https"),
    );
    const { container, getByTestId } = renderPage();
    await waitRows(container, 2);
    fireEvent.click(getByTestId("shared-docs-add-link"));
    fireEvent.change(getByTestId("shared-docs-link-url"), { target: { value: "ftp://x" } });
    fireEvent.submit(getByTestId("shared-docs-link-form"));
    expect((await waitFor(() => getByTestId("shared-docs-link-error"))).textContent).toBe(
      "url must be http or https",
    );
    expect((getByTestId("shared-docs-link-url") as HTMLInputElement).value).toBe("ftp://x");
    expect(rows(container)).toHaveLength(2);
  });

  it("上傳新版本 on a row puts the picked file in as that doc's new version, the row busy until its own upload ends", async () => {
    const realUpload = api.uploadAttachment.bind(api);
    const gates: Array<() => void> = [];
    vi.spyOn(api, "uploadAttachment").mockImplementation(async (file, filename) => {
      await new Promise<void>((open) => gates.push(open));
      return realUpload(file, filename);
    });
    const { container, getByTestId } = renderPage();
    const [, review] = await waitRows(container, 2);
    const rowUpload = within(review!).getByTestId("shared-doc-upload");
    const topUpload = getByTestId("shared-docs-upload") as HTMLButtonElement;
    fireEvent.click(rowUpload);
    pickFile(container, new File(["# Review Rule v3"], "review-rule.md", { type: "text/markdown" }));
    fireEvent.click(topUpload);
    pickFile(container, new File(["x"], "other.md", { type: "text/markdown" }));
    await waitFor(() => expect(gates).toHaveLength(2));
    expect([rowUpload.getAttribute("aria-disabled"), topUpload.disabled]).toEqual(["true", true]);
    gates[1]!();
    await waitRows(container, 3);
    await waitFor(() => expect(topUpload.disabled).toBe(false));
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

  it("an upload that fails shows why on the list and changes nothing", async () => {
    vi.spyOn(api, "uploadAttachment").mockRejectedValue(
      new ApiError("http 400", 400, "validation_error", "attachment exceeds the 100 MB size limit"),
    );
    const { container, getByTestId } = renderPage();
    const [, review] = await waitRows(container, 2);
    fireEvent.click(within(review!).getByTestId("shared-doc-upload"));
    pickFile(container, new File(["x"], "review-rule.md", { type: "text/markdown" }));
    expect((await waitFor(() => getByTestId("shared-docs-action-error"))).textContent).toBe(
      "attachment exceeds the 100 MB size limit",
    );
    fireEvent.click(getByTestId("shared-docs-upload"));
    pickFile(container, new File(["x"], "new.md", { type: "text/markdown" }));
    await waitFor(() => expect((getByTestId("shared-docs-upload") as HTMLButtonElement).disabled).toBe(false));
    expect(getByTestId("shared-docs-action-error").textContent).toBe("attachment exceeds the 100 MB size limit");
    expect(rows(container)).toHaveLength(2);
    expect((await api.getSharedDoc("sd-3f9a2c71b0e4")).versionCount).toBe(2);
  });

  it("a dimmed 編輯 or 上傳新版本 does nothing, by click or by Enter/Space", async () => {
    const shotId = await createUploaded("image", "Shot", "shot.png", "png");
    const contractId = await createUploaded("file", "Contract", "contract.pdf", "pdf");
    const { container, queryByTestId } = renderPage();
    await waitRows(container, 4);
    const input = container.querySelector<HTMLInputElement>('[data-testid="shared-docs-file-input"]')!;
    const openPicker = vi.spyOn(input, "click");
    const dimmed = [
      within(container.querySelector<HTMLElement>(`[data-doc-id="${shotId}"]`)!).getByTestId("shared-doc-edit"),
      within(container.querySelector<HTMLElement>(`[data-doc-id="${contractId}"]`)!).getByTestId("shared-doc-edit"),
      within(container.querySelector<HTMLElement>('[data-doc-id="sd-0c41e97a2b58"]')!).getByTestId("shared-doc-upload"),
    ];
    for (const button of dimmed) {
      expect(button.getAttribute("aria-disabled")).toBe("true");
      button.focus();
      expect(document.activeElement).toBe(button);
      for (const key of ["Enter", " "]) {
        fireEvent.keyDown(button, { key });
        fireEvent.keyUp(button, { key });
      }
      fireEvent.click(button);
    }
    expect(openPicker).not.toHaveBeenCalled();
    expect(window.location.hash).toBe("#shared-docs");
    expect(container.querySelector(".shared-docs__inline-field")).toBeNull();
    expect(queryByTestId("shared-docs-action-error")).toBeNull();
  });

  it("編輯 on a link row edits its URL in place: ✓ saves it as a new version, a refused URL stays in the box, ✗ leaves it", async () => {
    const { container, getByTestId } = renderPage();
    const [apiSpec] = await waitRows(container, 2);
    fireEvent.click(within(apiSpec!).getByTestId("shared-doc-edit"));
    const input = within(apiSpec!).getByLabelText("修改網址") as HTMLInputElement;
    expect(input.value).toBe("https://github.com/example/officraft/blob/main/spec/openapi.json");
    fireEvent.click(input);
    expect(window.location.hash).toBe("#shared-docs");
    fireEvent.change(input, { target: { value: "https://example.com/other" } });
    fireEvent.mouseDown(within(apiSpec!).getByLabelText("取消"));
    expect(within(apiSpec!).queryByLabelText("修改網址")).toBeNull();
    expect(cellTexts(apiSpec!)[1]).toBe("API 規格GitHub 上的 spec/openapi.json，改 wire 前先讀");

    vi.spyOn(api, "replaceSharedDoc").mockRejectedValueOnce(
      new ApiError("http 400", 400, "validation_error", "url must be an absolute http or https URL"),
    );
    fireEvent.click(within(apiSpec!).getByTestId("shared-doc-edit"));
    fireEvent.change(within(apiSpec!).getByLabelText("修改網址"), { target: { value: "example.com/openapi.json" } });
    fireEvent.mouseDown(within(apiSpec!).getByLabelText("套用"));
    expect((await waitFor(() => getByTestId("shared-docs-action-error"))).textContent).toBe(
      "url must be an absolute http or https URL",
    );
    const kept = within(apiSpec!).getByLabelText("修改網址") as HTMLInputElement;
    expect(kept.value).toBe("example.com/openapi.json");
    fireEvent.change(kept, { target: { value: "https://example.com/openapi.json" } });
    fireEvent.mouseDown(within(apiSpec!).getByLabelText("套用"));
    await waitFor(async () =>
      expect(await api.getSharedDoc("sd-0c41e97a2b58")).toMatchObject({
        url: "https://example.com/openapi.json",
        versionCount: 2,
      }),
    );
    await waitFor(() => expect(within(apiSpec!).queryByLabelText("修改網址")).toBeNull());
    expect(window.location.hash).toBe("#shared-docs");
  });

  it("編輯 on a text file opens its content in an editor over the list: 取消, × and Esc close it unsaved, 儲存 puts it in as a new version", async () => {
    const original = "# Review Rule\n\n1. 範圍：變更檔案逐一說得出必要性。\n2. 測試：新行為有測試。\n";
    const { container, getByTestId, queryByTestId } = renderPage();
    const [, review] = await waitRows(container, 2);
    const openEditor = async () => {
      fireEvent.click(within(review!).getByTestId("shared-doc-edit"));
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

    editor = await openEditor();
    expect(editor.value).toBe(original);
    fireEvent.change(editor, { target: { value: "draft two" } });
    fireEvent.click(getByTestId("shared-doc-edit-overlay").querySelector(".md-preview__close")!);
    expect(queryByTestId("shared-doc-edit-overlay")).toBeNull();

    editor = await openEditor();
    fireEvent.change(editor, { target: { value: "draft three" } });
    fireEvent.keyDown(editor, { key: "Escape" });
    expect(queryByTestId("shared-doc-edit-overlay")).toBeNull();
    expect(await api.getSharedDoc("sd-3f9a2c71b0e4")).toMatchObject({ text: original, versionCount: 2 });

    editor = await openEditor();
    expect(editor.value).toBe(original);
    fireEvent.change(editor, { target: { value: "# Review Rule v3\n" } });
    fireEvent.click(getByTestId("shared-doc-editor-save"));
    await waitFor(() => expect(queryByTestId("shared-doc-edit-overlay")).toBeNull());
    expect(await api.getSharedDoc("sd-3f9a2c71b0e4")).toMatchObject({
      text: "# Review Rule v3\n",
      filename: "review-rule.md",
      versionCount: 3,
    });
    const saved = await api.getSharedDoc("sd-3f9a2c71b0e4");
    await waitFor(() =>
      expect(within(review!).getByTestId("shared-doc-updated").textContent).toBe(
        `CEO（你） · ${formatAbsolute(saved.updatedTs, Math.floor(Date.now() / 1000))}`,
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
        throw new ApiError("http 400", 400, "validation_error", "attachment exceeds the 100 MB size limit");
      })
      .mockImplementation(realUpload);
    const { container, getByTestId } = renderPage();
    const [, review] = await waitRows(container, 2);
    fireEvent.click(within(review!).getByTestId("shared-doc-edit"));
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

  it("編輯 on a text file too large to show says it cannot be edited here and offers the download instead of an editor", async () => {
    const real = await api.getSharedDoc("sd-3f9a2c71b0e4");
    vi.spyOn(api, "getSharedDoc").mockResolvedValue({ ...real, text: null, textOmitted: "too_large" });
    const { container, getByTestId, queryByTestId } = renderPage();
    const [, review] = await waitRows(container, 2);
    fireEvent.click(within(review!).getByTestId("shared-doc-edit"));
    expect((await waitFor(() => getByTestId("shared-doc-edit-too-large"))).textContent).toBe(
      "這份文字檔太大，無法在網頁上編輯；請下載修改後，用「上傳新版本」換上新的檔案。",
    );
    const download = getByTestId("shared-doc-download");
    expect([download.textContent, download.getAttribute("download")]).toEqual(["下載", "review-rule.md"]);
    expect(download.getAttribute("href")).toContain("/api/chat/attachment/att-5d0c11a2b3c4");
    expect(queryByTestId("shared-doc-editor")).toBeNull();
  });

  it("編輯 on a doc that cannot be read shows why in the overlay: gone says not found, any other failure gives the server's message", async () => {
    vi.spyOn(api, "getSharedDoc")
      .mockRejectedValueOnce(new ApiError("http 404", 404, "not_found", "shared doc not found"))
      .mockRejectedValueOnce(new ApiError("http 500", 500, "internal", "database is locked"));
    const { container, getByTestId } = renderPage();
    const [, review] = await waitRows(container, 2);
    const status = () => getByTestId("shared-doc-edit-overlay").querySelector(".md-preview__status")?.textContent;
    fireEvent.click(within(review!).getByTestId("shared-doc-edit"));
    await waitFor(() => expect(status()).toBe("找不到這個檔案"));
    fireEvent.click(getByTestId("shared-doc-edit-overlay").querySelector(".md-preview__close")!);
    fireEvent.click(within(review!).getByTestId("shared-doc-edit"));
    await waitFor(() => expect(status()).toBe("database is locked"));
  });
});
