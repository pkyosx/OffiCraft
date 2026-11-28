import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { render, fireEvent, waitFor, screen, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { ApiError } from "../api/errors";
import { codeForStatus } from "../api/errorCodes";
import { zh } from "../i18n/locales/zh";
import { ShareLinkMenu } from "./ShareLinkMenu";
import type { ShareLinkView } from "../types";

const ATT = "att-0123456789ab";

function link(id: string, expiresTs: number | null): ShareLinkView {
  return {
    id,
    kind: "attachment",
    attachmentId: ATT,
    attachmentFilename: "報告.md",
    before: null,
    after: null,
    labelBefore: null,
    labelAfter: null,
    createdBy: "owner",
    createdTs: 1792650600,
    expiresTs,
  };
}

let writeText: ReturnType<typeof vi.fn>;

function menuFor(attachmentId: string) {
  return (
    <I18nProvider>
      <ShareLinkMenu
        target={{ kind: "attachment", attachmentId }}
        targetName="報告.md"
        className="md-preview__share"
        testId="md-preview-share"
      />
    </I18nProvider>
  );
}

function mount() {
  const view = render(menuFor(ATT));
  fireEvent.click(screen.getByTestId("md-preview-share"));
  return view;
}

describe("ShareLinkMenu", () => {
  beforeEach(() => {
    writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("lists the target's live links newest first, with ∞ for a permanent one", async () => {
    let answer!: (links: ShareLinkView[]) => void;
    const list = vi
      .spyOn(api, "listShareLinks")
      .mockReturnValue(new Promise((resolve) => (answer = resolve)));

    mount();

    expect(screen.getByTestId("share-issued").textContent).toBe("已發出 · 讀取中…");
    answer([link("sl-old", null), link("sl-new", 1793255400)]);
    await waitFor(() => expect(screen.getAllByTestId("share-issued-row")).toHaveLength(2));
    expect(list).toHaveBeenCalledWith({ attachmentId: ATT });
    const rows = screen.getAllByTestId("share-issued-row");
    expect(rows.map((r) => r.getAttribute("data-share-link-id"))).toEqual(["sl-new", "sl-old"]);
    expect(within(rows[1]).getByRole("img", { name: zh.shareLinks.forever })).toBeTruthy();
    expect(screen.getByTestId("share-issued").textContent).toContain(`${zh.shareLinks.issued} · 2`);
  });

  it("mints a 7-day link by default, copies it absolutized and puts it on top", async () => {
    vi.spyOn(api, "listShareLinks").mockResolvedValue([link("sl-old", null)]);
    const mint = vi.spyOn(api, "createAttachmentShareLink").mockResolvedValue({
      url: `/api/chat/attachment/${ATT}?link=tok`,
      link: link("sl-fresh", 1793255400),
    });
    const before = Math.floor(Date.now() / 1000);

    mount();
    await screen.findAllByTestId("share-issued-row");
    fireEvent.click(screen.getByTestId("share-generate"));

    await waitFor(() => expect(mint).toHaveBeenCalledTimes(1));
    const [id, expiresTs] = mint.mock.calls[0];
    expect(id).toBe(ATT);
    expect(expiresTs).toBeGreaterThanOrEqual(before + 7 * 86_400);
    expect(expiresTs).toBeLessThanOrEqual(Math.floor(Date.now() / 1000) + 7 * 86_400);
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/api/chat/attachment/${ATT}?link=tok`),
    );
    expect(screen.getByTestId("share-generate").getAttribute("aria-label")).toBe(zh.shareLinks.copied);
    expect(
      screen.getAllByTestId("share-issued-row").map((r) => r.getAttribute("data-share-link-id")),
    ).toEqual(["sl-fresh", "sl-old"]);
  });

  it("sends null for 永久 and disables the number field", async () => {
    vi.spyOn(api, "listShareLinks").mockResolvedValue([]);
    const mint = vi.spyOn(api, "createAttachmentShareLink").mockResolvedValue({
      url: "/x",
      link: link("sl-forever", null),
    });

    mount();
    fireEvent.change(screen.getByTestId("share-expiry-unit"), { target: { value: "forever" } });

    expect((screen.getByTestId("share-expiry-amount") as HTMLInputElement).disabled).toBe(true);
    expect(
      within(screen.getByTestId("share-expiry-preview")).getByRole("img", { name: zh.shareLinks.forever }),
    ).toBeTruthy();
    fireEvent.click(screen.getByTestId("share-generate"));
    await waitFor(() => expect(mint).toHaveBeenCalledWith(ATT, null));
  });

  it("refuses more than 365 days before asking the server", async () => {
    vi.spyOn(api, "listShareLinks").mockResolvedValue([]);
    const mint = vi.spyOn(api, "createAttachmentShareLink");

    mount();
    fireEvent.change(screen.getByTestId("share-expiry-amount"), { target: { value: "366" } });

    expect(screen.getByTestId("share-error").textContent).toBe(zh.shareLinks.tooFar);
    expect((screen.getByTestId("share-generate") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId("share-expiry-amount"), { target: { value: "365" } });
    expect(screen.queryByTestId("share-error")).toBeNull();
    expect(mint).not.toHaveBeenCalled();
  });

  it.each([
    [422, "expires_ts must be later than now", "伺服器不接受這個到期時間，請確認這台裝置的時間是否正確。"],
    [404, "attachment 'att-0123456789ab' not found", "attachment 'att-0123456789ab' not found"],
  ])("says why a %i mint was refused, and copies nothing", async (status, serverText, shown) => {
    vi.spyOn(api, "listShareLinks").mockResolvedValue([]);
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(api, "createAttachmentShareLink").mockRejectedValue(
      new ApiError(`http ${status}`, status, codeForStatus(status), serverText),
    );

    mount();
    fireEvent.click(screen.getByTestId("share-generate"));

    expect((await screen.findByTestId("share-error")).textContent).toBe(shown);
    expect(writeText).not.toHaveBeenCalled();
  });

  it("keeps the new URL on screen when copying fails, and copies it on retry", async () => {
    vi.spyOn(api, "listShareLinks").mockResolvedValue([]);
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(api, "createAttachmentShareLink").mockResolvedValue({
      url: `/api/chat/attachment/${ATT}?link=tok`,
      link: link("sl-fresh", 1793255400),
    });
    writeText.mockRejectedValueOnce(new DOMException("denied", "NotAllowedError"));
    const url = `${window.location.origin}/api/chat/attachment/${ATT}?link=tok`;

    mount();
    fireEvent.click(screen.getByTestId("share-generate"));

    const field = (await screen.findByTestId("share-uncopied-url")) as HTMLInputElement;
    expect(field.value).toBe(url);
    expect(field.readOnly).toBe(true);
    expect(screen.getByTestId("share-generate").getAttribute("aria-label")).toBe("已產生連結，但複製失敗");
    fireEvent.click(screen.getByTestId("share-uncopied-copy"));

    await waitFor(() => expect(screen.queryByTestId("share-uncopied")).toBeNull());
    expect(writeText).toHaveBeenLastCalledWith(url);
    expect(screen.getByTestId("share-generate").getAttribute("aria-label")).toBe("已複製連結");
  });

  it("keeps a link minted for the previous file out of the next file's list", async () => {
    const list = vi.spyOn(api, "listShareLinks").mockResolvedValue([]);
    let finishMint!: (created: { url: string; link: ShareLinkView }) => void;
    vi.spyOn(api, "createAttachmentShareLink").mockReturnValue(
      new Promise((resolve) => (finishMint = resolve)),
    );

    const view = mount();
    await waitFor(() => expect(list).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByTestId("share-generate"));
    view.rerender(menuFor("att-fedcba987654"));
    fireEvent.click(screen.getByTestId("md-preview-share"));
    await waitFor(() => expect(list).toHaveBeenLastCalledWith({ attachmentId: "att-fedcba987654" }));
    await waitFor(() => expect(screen.getByTestId("share-issued").textContent).toBe("已發出 · 0"));

    finishMint({ url: `/api/chat/attachment/${ATT}?link=tok`, link: link("sl-previous", null) });

    await waitFor(() => expect(writeText).toHaveBeenCalledTimes(1));
    expect(screen.queryAllByTestId("share-issued-row")).toHaveLength(0);
    expect(screen.getByTestId("share-issued").textContent).toBe("已發出 · 0");
  });

  it("revokes only the confirmed link, after asking", async () => {
    vi.spyOn(api, "listShareLinks").mockResolvedValue([link("sl-a", null), link("sl-b", 1793255400)]);
    const revoke = vi.spyOn(api, "revokeShareLink").mockResolvedValue(link("sl-a", null));

    mount();
    const rows = await screen.findAllByTestId("share-issued-row");
    const rowA = rows.find((r) => r.getAttribute("data-share-link-id") === "sl-a")!;
    fireEvent.click(within(rowA).getByTestId("share-revoke"));

    expect(revoke).not.toHaveBeenCalled();
    expect(screen.getByTestId("share-revoke-confirm").textContent).toContain("報告.md");
    fireEvent.click(screen.getByTestId("share-revoke-confirm-btn"));

    await waitFor(() => expect(revoke).toHaveBeenCalledWith("sl-a"));
    await waitFor(() =>
      expect(
        screen.getAllByTestId("share-issued-row").map((r) => r.getAttribute("data-share-link-id")),
      ).toEqual(["sl-b"]),
    );
    expect(screen.queryByTestId("share-revoke-confirm")).toBeNull();
    expect(screen.getByTestId("share-menu")).toBeTruthy();
  });

  it("drops a link someone else already revoked instead of reporting a failure", async () => {
    vi.spyOn(api, "listShareLinks").mockResolvedValue([link("sl-a", null), link("sl-b", 1793255400)]);
    vi.spyOn(api, "revokeShareLink").mockRejectedValue(
      new ApiError("http 404", 404, codeForStatus(404), "share link 'sl-a' not found"),
    );

    mount();
    const rows = await screen.findAllByTestId("share-issued-row");
    const rowA = rows.find((r) => r.getAttribute("data-share-link-id") === "sl-a")!;
    fireEvent.click(within(rowA).getByTestId("share-revoke"));
    fireEvent.click(screen.getByTestId("share-revoke-confirm-btn"));

    await waitFor(() =>
      expect(
        screen.getAllByTestId("share-issued-row").map((r) => r.getAttribute("data-share-link-id")),
      ).toEqual(["sl-b"]),
    );
    expect(screen.queryByTestId("share-revoke-confirm")).toBeNull();
  });
});
