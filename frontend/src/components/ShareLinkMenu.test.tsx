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

function mount() {
  render(
    <I18nProvider>
      <ShareLinkMenu
        target={{ kind: "attachment", attachmentId: ATT }}
        targetName="報告.md"
        className="md-preview__share"
        testId="md-preview-share"
      />
    </I18nProvider>,
  );
  fireEvent.click(screen.getByTestId("md-preview-share"));
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
    const list = vi
      .spyOn(api, "listShareLinks")
      .mockResolvedValue([link("sl-old", null), link("sl-new", 1793255400)]);

    mount();

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

  it("shows the server's reason when the mint is refused, and copies nothing", async () => {
    vi.spyOn(api, "listShareLinks").mockResolvedValue([]);
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(api, "createAttachmentShareLink").mockRejectedValue(
      new ApiError("http 422", 422, codeForStatus(422), "expires_ts must be later than now"),
    );

    mount();
    fireEvent.click(screen.getByTestId("share-generate"));

    expect((await screen.findByTestId("share-error")).textContent).toBe(
      "expires_ts must be later than now",
    );
    expect(writeText).not.toHaveBeenCalled();
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
});
