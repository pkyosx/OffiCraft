import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SharedDocVersionsModal } from "./SharedDocVersionsModal";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { ApiError } from "../api/errors";
import { codeForStatus } from "../api/errorCodes";
import { formatAbsolute } from "../lib/dateFormat";

const s = zh.settings;
const BLOBS: Record<string, string> = {
  "/api/chat/attachment/att-5d0c11a2b3c4": "# Review Rule\n\nline two\n",
  "/api/chat/attachment/att-5d0c11a2b3b0": "# Review Rule\n",
};
let fetched: string[] = [];

function renderModal(docId = "sd-3f9a2c71b0e4", onClose = vi.fn(), onRestored = vi.fn()) {
  const view = render(
    <I18nProvider>
      <SharedDocVersionsModal
        docId={docId}
        actorName={(a) => (a === "mira" ? "Mira" : a)}
        onRestored={onRestored}
        onClose={onClose}
      />
    </I18nProvider>,
  );
  return { ...view, onClose, onRestored };
}

async function openVersion(getByTestId: (id: string) => HTMLElement, id: number) {
  fireEvent.click(await waitFor(() => getByTestId(`sd-versions-open-${id}`)));
  return waitFor(() => getByTestId("doc-history-modal"));
}

beforeEach(() => {
  __resetMock();
  fetched = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const path = url.split("?")[0]!;
      fetched.push(path);
      return new Response(BLOBS[path] ?? "", {
        status: path in BLOBS ? 200 : 404,
        headers: { "content-type": "text/markdown" },
      });
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("SharedDocVersionsModal", () => {
  it("lists each retained version by when and by whom, under the retention note", async () => {
    const { getByTestId } = renderModal();
    const now = Math.floor(Date.now() / 1000);
    const row = await waitFor(() => getByTestId("sd-versions-open-1"));
    expect(getByTestId("doc-history-list").querySelector(".doc-hist__note")?.textContent).toBe(
      zh.sharedDocs.versionsNote,
    );
    expect(row.querySelector(".doc-hist__meta")?.textContent).toBe(
      `${formatAbsolute(1762900000, now)}${s.historyByLabel} Mira${s.historyOpen}`,
    );
  });

  it("opens a version on its rendered content, named by its time and author", async () => {
    const { getByTestId } = renderModal();
    const reader = await openVersion(getByTestId, 1);
    const now = Math.floor(Date.now() / 1000);
    expect(reader.querySelector(".doc-hist-modal__ident")?.textContent).toBe(
      `${formatAbsolute(1762900000, now)}${s.historyByLabel} Mira`,
    );
    await waitFor(() =>
      expect(getByTestId("sd-versions-content-text").querySelector("h1")?.textContent).toBe("Review Rule"),
    );
    expect(getByTestId("doc-history-pane-content").getAttribute("aria-pressed")).toBe("true");
  });

  it("與目前的差異 shows the line diff of this version against the current file", async () => {
    const { getByTestId } = renderModal();
    await openVersion(getByTestId, 1);
    fireEvent.click(await waitFor(() => getByTestId("doc-history-pane-diff")));
    const diff = await waitFor(() => getByTestId("sd-versions-diff"));
    expect(
      Array.from(diff.querySelectorAll(".diff-view__text")).map((c) => (c.textContent ?? "").replace(/ /g, "")),
    ).toEqual(["# Review Rule", "", "line two"]);
  });

  it("還原 asks first; confirming makes the version current, tells the page and closes", async () => {
    const { getByTestId, onClose, onRestored } = renderModal();
    await openVersion(getByTestId, 1);
    fireEvent.click(getByTestId("doc-history-modal-restore"));
    const now = Math.floor(Date.now() / 1000);
    expect(getByTestId("doc-history-restore-confirm").textContent).toContain(
      `「${formatAbsolute(1762900000, now)}」`,
    );
    expect((await api.getSharedDoc("sd-3f9a2c71b0e4")).attachmentId).toBe("att-5d0c11a2b3c4");
    fireEvent.click(getByTestId("doc-history-restore-confirm-btn"));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(onRestored).toHaveBeenCalledTimes(1);
    expect((await api.getSharedDoc("sd-3f9a2c71b0e4")).attachmentId).toBe("att-5d0c11a2b3b0");
  });

  it("a refused restore shows the server's reason and keeps the reader and the confirmation open", async () => {
    vi.spyOn(api, "restoreSharedDocVersion").mockRejectedValue(
      new ApiError("http 404", 404, codeForStatus(404), "version 1 is gone"),
    );
    const { getByTestId, onClose, onRestored } = renderModal();
    await openVersion(getByTestId, 1);
    fireEvent.click(getByTestId("doc-history-modal-restore"));
    fireEvent.click(getByTestId("doc-history-restore-confirm-btn"));
    await waitFor(() =>
      expect(getByTestId("doc-history-restore-confirm").textContent).toContain("version 1 is gone"),
    );
    expect(getByTestId("doc-history-modal")).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
    expect(onRestored).not.toHaveBeenCalled();
  });

  it("返回版本列表 goes back to the list without closing", async () => {
    const { getByTestId, queryByTestId, onClose } = renderModal();
    await openVersion(getByTestId, 1);
    fireEvent.click(getByTestId("doc-history-modal-back"));
    await waitFor(() => getByTestId("sd-versions-open-1"));
    expect(queryByTestId("doc-history-modal")).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("an image version shows its picture and file name, with no diff to switch to", async () => {
    const first = await api.uploadAttachment(new Blob(["a"]), "home.png");
    const id = await api.createSharedDoc({ kind: "image", name: "首頁截圖", description: "", attachmentId: first });
    const second = await api.uploadAttachment(new Blob(["b"]), "home-v2.png");
    await api.replaceSharedDoc(id, { attachmentId: second });
    const [version] = await api.listSharedDocVersions(id);
    const { getByTestId, queryByTestId } = renderModal(id);
    const reader = await openVersion(getByTestId, version!.id);
    const img = await waitFor(() => getByTestId("sd-versions-content-image"));
    expect(img.getAttribute("src")).toContain(version!.url);
    expect(within(reader).getByText("home.png")).toBeTruthy();
    expect(queryByTestId("doc-history-pane-diff")).toBeNull();
  });

  it("a link version shows its URL above the current one, fetching nothing and offering no diff", async () => {
    await api.replaceSharedDoc("sd-0c41e97a2b58", { url: "https://example.com/v2" });
    const [version] = await api.listSharedDocVersions("sd-0c41e97a2b58");
    const { getByTestId, queryByTestId } = renderModal("sd-0c41e97a2b58");
    await openVersion(getByTestId, version!.id);
    const urls = await waitFor(() => getByTestId("sd-versions-urls"));
    expect(Array.from(urls.querySelectorAll("a")).map((a) => a.getAttribute("href"))).toEqual([
      "https://github.com/example/officraft/blob/main/spec/openapi.json",
      "https://example.com/v2",
    ]);
    expect(urls.querySelectorAll(".ta-versions__url-side")[1]?.textContent).toBe(zh.sharedDocs.versionsCurrentUrl);
    expect(queryByTestId("doc-history-pane-diff")).toBeNull();
    expect(fetched).toEqual([]);
  });

  it("says there is no earlier version rather than showing an empty list", async () => {
    const { getByTestId } = renderModal("sd-0c41e97a2b58");
    expect((await waitFor(() => getByTestId("sd-versions-empty"))).textContent).toBe(zh.sharedDocs.versionsEmpty);
  });
});
