// M2-3 member file & image gallery panel (batch-16 upgrade: member-perspective
// scope + 圖片/檔案 tabs).
//
// Covers: the dedicated gallery fetch (listChatAttachments — the server
// flattens + sender-labels the rows; no client aggregation), the 圖片/檔案 tab
// split with per-tab honest empty states, inter-agent rows surfacing with their
// server-resolved sender names, sender + time per row, the preview/download
// split (previewable mime → open in a new tab; opaque binary → download), and
// closing.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatGalleryPanel, isPreviewableMime } from "./ChatGalleryPanel";
import type { Member } from "../types";
import type { GalleryAttachment } from "../api/adapter";

let galleryRows: GalleryAttachment[] = [];
const listChatAttachments = vi.fn(
  async (_withId: string): Promise<GalleryAttachment[]> => galleryRows,
);
const getChatAttachmentShareLink = vi.fn(
  async (id: string): Promise<string> => `/api/chat/attachment/${id}?sig=test-sig`,
);

vi.mock("../api", () => ({
  api: {
    listChatAttachments: (withId: string) => listChatAttachments(withId),
    getChatAttachmentShareLink: (id: string) => getChatAttachmentShareLink(id),
    subscribeEvents: () => () => {},
  },
}));

function mkMember(): Member {
  return {
    id: "m1",
    memberId: "m1",
    name: "Mira",
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "assistant",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-m1",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

function row(
  id: string,
  mime: string,
  from: string,
  fromName: string,
  ts: number,
  filename = `${id}.bin`,
): GalleryAttachment {
  return {
    id,
    url: `/api/chat/attachment/${id}`,
    filename,
    mime,
    isImage: mime.startsWith("image/"),
    messageId: `msg-${id}`,
    from,
    fromName,
    to: from === "owner" ? "m1" : "owner",
    ts,
  };
}

function renderPanel(
  onClose: () => void = () => {},
  resolveSender?: (id: string) => string,
) {
  return render(
    <I18nProvider>
      <ChatGalleryPanel
        member={mkMember()}
        resolveSender={resolveSender}
        onClose={onClose}
      />
    </I18nProvider>,
  );
}

const itemsIn = (container: HTMLElement) => [
  ...container.querySelectorAll<HTMLAnchorElement>(".chat__gallery-item"),
];

describe("ChatGalleryPanel", () => {
  beforeEach(() => {
    galleryRows = [];
    listChatAttachments.mockClear();
    localStorage.clear();
  });

  it("fetches the member's flattened gallery (listChatAttachments)", async () => {
    renderPanel();
    await waitFor(() => expect(listChatAttachments).toHaveBeenCalledWith("m1"));
  });

  it("splits 圖片/檔案 tabs and renders sender + time per row (incl. inter-agent)", async () => {
    galleryRows = [
      // Server order: newest→oldest. An inter-agent row (Bob→Mira) rides the
      // SAME list — it must surface, labelled with the SERVER-resolved name.
      row("a3", "application/pdf", "m2", "Bob", 300, "from-bob.pdf"),
      row("a2", "application/pdf", "m1", "Mira", 200, "r.pdf"),
      row("a1", "image/png", "owner", "", 100, "shot.png"),
    ];
    const { container } = renderPanel();
    // Default tab: 圖片 — only the image shows.
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(itemsIn(container)[0].querySelector("img")).toBeTruthy();
    // The owner's row reads 我 (zh default locale); time from the message ts.
    expect(itemsIn(container)[0].textContent).toContain("我");
    expect(
      itemsIn(container)[0].querySelector(".chat__gallery-sub")?.textContent,
    ).toMatch(/\d{1,2}:\d{2}/);
    expect(container.textContent).not.toContain("r.pdf");
    // Switch to 檔案 — both files show, newest first, sender names shown; the
    // raw member ids never render (only display names).
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(2));
    expect(itemsIn(container)[0].textContent).toContain("from-bob.pdf");
    expect(itemsIn(container)[0].textContent).toContain("Bob");
    expect(itemsIn(container)[1].textContent).toContain("Mira");
    expect(container.textContent).not.toContain("m2");
    expect(container.textContent).not.toContain("shot.png");
  });

  it("splits open behavior: previewable → new tab, opaque binary → download", async () => {
    galleryRows = [
      row("a1", "image/png", "owner", "", 100, "img.png"),
      row("a2", "text/markdown", "owner", "", 100, "notes.md"),
      row("a3", "application/pdf", "owner", "", 100, "doc.pdf"),
      row("a4", "application/zip", "owner", "", 100, "bundle.zip"),
    ];
    const { container } = renderPanel();
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(3));
    const byName = (name: string) =>
      itemsIn(container).find((a) => a.textContent?.includes(name))!;
    for (const name of ["notes.md", "doc.pdf"]) {
      const a = byName(name);
      expect(a.target).toBe("_blank");
      expect(a.hasAttribute("download")).toBe(false);
    }
    const zip = byName("bundle.zip");
    expect(zip.target).toBe("");
    expect(zip.getAttribute("download")).toBe("bundle.zip");
    // The image tab keeps the new-tab preview behavior.
    fireEvent.click(screen.getByRole("tab", { name: "圖片" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(itemsIn(container)[0].target).toBe("_blank");
  });

  it("shows per-tab honest empty states once loaded", async () => {
    galleryRows = [row("a1", "application/pdf", "m1", "Mira", 100, "only.pdf")];
    const first = renderPanel();
    // 圖片 tab (default) is empty even though a FILE exists.
    expect(await screen.findByText("還沒有圖片")).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() =>
      expect(screen.queryByText("還沒有圖片")).toBeNull(),
    );
    expect(screen.getByText("only.pdf")).toBeTruthy();
    first.unmount();
    // And the 檔案 tab's own empty state when nothing at all exists.
    galleryRows = [];
    renderPanel();
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    expect(await screen.findByText("還沒有檔案")).toBeTruthy();
  });

  // ── Uploader filter (batch 18) ────────────────────────────────────────────
  // Chip row under the tabs: 「全部」 + one chip per ACTUAL sender (dynamic,
  // never hardcoded); stacks with the 圖片/檔案 tab split.

  const chipRow = () => screen.getByRole("group", { name: "依上傳者篩選" });
  const chip = (label: string) =>
    [...chipRow().querySelectorAll<HTMLButtonElement>("button")].find(
      (b) => b.textContent === label,
    )!;

  it("derives uploader chips from the actual rows and filters to one sender", async () => {
    galleryRows = [
      row("a4", "image/png", "m2", "Bob", 400, "bob.png"),
      row("a3", "image/png", "owner", "", 300, "mine.png"),
      row("a2", "image/png", "m1", "Mira", 200, "mira.png"),
      row("a1", "application/pdf", "m2", "Bob", 100, "bob.pdf"),
    ];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(3));
    // Chips: 全部 + the three actual senders — owner reads 「我」, others by
    // their server-resolved names; no raw internal ids leak.
    const labels = [...chipRow().querySelectorAll("button")].map(
      (b) => b.textContent,
    );
    expect(labels).toEqual(["全部", "Bob", "我", "Mira"]);
    // Pick Bob → only Bob's image remains on the 圖片 tab.
    fireEvent.click(chip("Bob"));
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(itemsIn(container)[0].textContent).toContain("bob.png");
    expect(container.textContent).not.toContain("mine.png");
    expect(container.textContent).not.toContain("mira.png");
  });

  it("resolves an unnamed outsource sender through resolveSender, not the raw id", async () => {
    // The server leaves from_name "" for an outsource sender (never in the
    // members roster) — the caller-provided resolver (ChatArea's nameOf
    // codename chain) names the row and its uploader chip; without a resolver
    // hit the raw id would show.
    galleryRows = [
      row("a1", "image/png", "ow-533c0c4f9dba", "", 100, "work.png"),
    ];
    const { container } = renderPanel(undefined, (id) =>
      id === "ow-533c0c4f9dba" ? "外包 · X-1" : id,
    );
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(itemsIn(container)[0].textContent).toContain("外包 · X-1");
    const labels = [...chipRow().querySelectorAll("button")].map(
      (b) => b.textContent,
    );
    expect(labels).toEqual(["全部", "外包 · X-1"]);
    expect(container.textContent).not.toContain("ow-533c0c4f9dba");
  });

  it("stacks the uploader filter with the 圖片/檔案 tabs", async () => {
    galleryRows = [
      row("a3", "image/png", "m2", "Bob", 300, "bob.png"),
      row("a2", "application/pdf", "m2", "Bob", 200, "bob.pdf"),
      row("a1", "application/pdf", "m1", "Mira", 100, "mira.pdf"),
    ];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    fireEvent.click(chip("Bob"));
    // 圖片 × Bob → bob.png only.
    await waitFor(() =>
      expect(itemsIn(container)[0]?.textContent).toContain("bob.png"),
    );
    expect(itemsIn(container).length).toBe(1);
    // 檔案 × Bob → bob.pdf only (Mira's pdf stays filtered out).
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() =>
      expect(itemsIn(container)[0]?.textContent).toContain("bob.pdf"),
    );
    expect(itemsIn(container).length).toBe(1);
    expect(container.textContent).not.toContain("mira.pdf");
    // Back to 全部 on the same tab → both files show again (no filter).
    fireEvent.click(chip("全部"));
    await waitFor(() => expect(itemsIn(container).length).toBe(2));
    expect(container.textContent).toContain("mira.pdf");
  });

  it("shows the honest empty state when the picked uploader has nothing on the tab", async () => {
    galleryRows = [
      row("a2", "image/png", "m1", "Mira", 200, "mira.png"),
      row("a1", "application/pdf", "m2", "Bob", 100, "bob.pdf"),
    ];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    // Bob sent no image → 圖片 tab under the Bob filter is honestly empty.
    fireEvent.click(chip("Bob"));
    expect(await screen.findByText("還沒有圖片")).toBeTruthy();
    expect(itemsIn(container).length).toBe(0);
    // The chip row itself stays (it filters the whole gallery, not the tab).
    expect(chip("Bob")).toBeTruthy();
    // His file is still one tab away.
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    expect(await screen.findByText("bob.pdf")).toBeTruthy();
  });

  it("複製分享連結 copies the absolutized ?sig= URL and flashes 已複製", async () => {
    galleryRows = [row("a1", "image/png", "owner", "", 100, "shot.png")];
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    fireEvent.click(screen.getByRole("button", { name: "複製分享連結" }));
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith(
        `${window.location.origin}/api/chat/attachment/a1?sig=test-sig`,
      ),
    );
    expect(getChatAttachmentShareLink).toHaveBeenCalledWith("a1");
    // Transient copied feedback replaces the button label.
    expect(screen.getByRole("button", { name: "已複製連結" })).toBeTruthy();
  });

  it("closes via the close button and via Escape", async () => {
    const onClose = vi.fn();
    renderPanel(onClose);
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByLabelText("關閉檔案庫"));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});

describe("isPreviewableMime (pure)", () => {
  it("mirrors the server's preview table", () => {
    expect(isPreviewableMime("image/webp")).toBe(true);
    expect(isPreviewableMime("text/html")).toBe(true);
    expect(isPreviewableMime("text/markdown")).toBe(true);
    expect(isPreviewableMime("application/pdf")).toBe(true);
    expect(isPreviewableMime("application/zip")).toBe(false);
    expect(isPreviewableMime("application/octet-stream")).toBe(false);
  });
});
