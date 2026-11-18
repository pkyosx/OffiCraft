import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { SharedDocsPage, sharedDocLink } from "./SharedDocsPage";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { formatAbsolute } from "../lib/dateFormat";

const copyTextMock = vi.hoisted(() => vi.fn(async (_text: string) => true));
vi.mock("../lib/clipboard", () => ({ copyText: copyTextMock }));

const owner = vi.hoisted(() => ({ value: true }));
vi.mock("../api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api")>()),
  viewerIsOwner: () => owner.value,
}));

function renderPage() {
  return render(
    <I18nProvider>
      <SharedDocsPage />
    </I18nProvider>,
  );
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
  owner.value = true;
  window.location.hash = "#shared-docs";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("SharedDocsPage", () => {
  it("lists active docs by name with ID, type, name, description, last modified and an archive action", async () => {
    const { container } = renderPage();
    const [api規格, review] = await waitRows(container, 2);
    const now = Math.floor(Date.now() / 1000);
    expect(cellTexts(api規格!)).toEqual([
      "sd-0c41e97a2b58",
      "",
      "API 規格",
      "GitHub 上的 spec/openapi.json，改 wire 前先讀",
      `Mira · ${formatAbsolute(1761648240, now)}`,
      "",
    ]);
    expect(cellTexts(review!)).toEqual([
      "sd-3f9a2c71b0e4",
      "",
      "Review Rule",
      "PR 審查時逐項核對的規則與退件標準",
      `Mira · ${formatAbsolute(1763366700, now)}`,
      "",
    ]);
    expect(api規格!.querySelector('[data-testid="shared-doc-kind-link"]')).not.toBeNull();
    expect(review!.querySelector('[data-testid="shared-doc-kind-file"]')).not.toBeNull();
    expect(review!.querySelector('[data-testid="shared-doc-archive"]')?.getAttribute("title")).toBe("封存");
  });

  it("the ID badge copies the id and the link button copies the in-site link to the doc", async () => {
    const { container } = renderPage();
    const [, review] = await waitRows(container, 2);
    fireEvent.click(within(review!).getByTestId("shared-doc-id"));
    await waitFor(() => expect(copyTextMock).toHaveBeenCalledWith("sd-3f9a2c71b0e4"));
    fireEvent.click(within(review!).getByTestId("shared-doc-copy-link"));
    await waitFor(() =>
      expect(copyTextMock).toHaveBeenLastCalledWith(
        `${window.location.origin}${window.location.pathname}#shared-docs/sd-3f9a2c71b0e4`,
      ),
    );
    expect(sharedDocLink("sd-1")).toBe(
      `${window.location.origin}${window.location.pathname}#shared-docs/sd-1`,
    );
  });

  it("archiving hides the doc; 顯示已封存 shows it dimmed with 已封存, 取消封存 and 永久刪除", async () => {
    const { container, getByTestId } = renderPage();
    const [, review] = await waitRows(container, 2);
    fireEvent.click(within(review!).getByTestId("shared-doc-archive"));
    await waitRows(container, 1);

    fireEvent.click(getByTestId("shared-docs-show-archived"));
    const [, archived] = await waitRows(container, 2);
    expect(archived!.className).toBe("shared-docs__row--archived");
    expect(within(archived!).getByTestId("shared-doc-archived-pill").textContent).toBe("已封存");
    expect(within(archived!).getByTestId("shared-doc-unarchive").textContent).toBe("取消封存");
    expect(within(archived!).getByTestId("shared-doc-delete").textContent).toBe("永久刪除");
    expect(within(archived!).queryByTestId("shared-doc-archive")).toBeNull();

    fireEvent.click(within(archived!).getByTestId("shared-doc-unarchive"));
    await waitFor(() =>
      expect(container.querySelector(".shared-docs__row--archived")).toBeNull(),
    );
  });

  it("a viewer who is not the owner gets no 永久刪除 on an archived doc", async () => {
    owner.value = false;
    await api.archiveSharedDoc("sd-3f9a2c71b0e4");
    const { container, getByTestId } = renderPage();
    await waitRows(container, 1);
    fireEvent.click(getByTestId("shared-docs-show-archived"));
    const [, archived] = await waitRows(container, 2);
    expect(within(archived!).getByTestId("shared-doc-unarchive")).toBeTruthy();
    expect(within(archived!).queryByTestId("shared-doc-delete")).toBeNull();
  });

  it("永久刪除 asks first, then removes the doc for good", async () => {
    await api.archiveSharedDoc("sd-3f9a2c71b0e4");
    const { container, getByTestId } = renderPage();
    await waitRows(container, 1);
    fireEvent.click(getByTestId("shared-docs-show-archived"));
    const [, archived] = await waitRows(container, 2);
    fireEvent.click(within(archived!).getByTestId("shared-doc-delete"));
    expect(getByTestId("shared-doc-delete-confirm").textContent).toContain(
      "要永久刪除「Review Rule」嗎？文件、所有版本與檔案都會刪除，無法復原。",
    );
    fireEvent.click(getByTestId("shared-doc-delete-confirm-btn"));
    await waitRows(container, 1);
    expect(await api.listSharedDocs(true)).toHaveLength(1);
  });

  it("新增共用文件 with a link creates the doc and opens it", async () => {
    const { container, getByTestId } = renderPage();
    await waitRows(container, 2);
    fireEvent.click(getByTestId("shared-docs-add"));
    const submit = getByTestId("shared-doc-form-submit") as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    fireEvent.click(getByTestId("shared-doc-source-link"));
    fireEvent.change(getByTestId("shared-doc-url-input"), {
      target: { value: "https://example.com/board" },
    });
    fireEvent.change(getByTestId("shared-doc-name-input"), { target: { value: "Board" } });
    fireEvent.change(getByTestId("shared-doc-description-input"), {
      target: { value: "the roadmap" },
    });
    fireEvent.click(submit);
    await waitFor(() => expect(window.location.hash).toMatch(/^#shared-docs\/sd-/));
    const created = (await api.listSharedDocs(false)).find((d) => d.name === "Board");
    expect(created).toMatchObject({
      kind: "link",
      description: "the roadmap",
      url: "https://example.com/board",
      versionCount: 1,
    });
    expect(window.location.hash).toBe(`#shared-docs/${created!.id}`);
  });

  it("新增共用文件 with a file uploads it first and the doc points at that upload", async () => {
    const upload = vi.spyOn(api, "uploadAttachment");
    const { container, getByTestId } = renderPage();
    await waitRows(container, 2);
    fireEvent.click(getByTestId("shared-docs-add"));
    const file = new File(["# Notes"], "notes.md", { type: "text/markdown" });
    fireEvent.change(getByTestId("shared-doc-file-input"), { target: { files: [file] } });
    fireEvent.change(getByTestId("shared-doc-name-input"), { target: { value: "Notes" } });
    fireEvent.click(getByTestId("shared-doc-form-submit"));
    await waitFor(() => expect(window.location.hash).toMatch(/^#shared-docs\/sd-/));
    expect(upload).toHaveBeenCalledWith(file, "notes.md");
    const attId = await upload.mock.results[0]!.value;
    const created = (await api.listSharedDocs(false)).find((d) => d.name === "Notes");
    expect(created).toMatchObject({ kind: "file", attachmentId: attId, filename: "notes.md" });
    expect((await api.getSharedDoc(created!.id)).text).toBe("# Notes");
  });
});
