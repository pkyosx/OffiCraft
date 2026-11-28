import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, screen, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ShareLinksPage } from "./ShareLinksPage";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { formatAbsolute } from "../lib/dateFormat";

function renderPage() {
  return render(
    <I18nProvider>
      <ShareLinksPage />
    </I18nProvider>,
  );
}

const rowIds = () =>
  screen.getAllByTestId("share-link-row").map((r) => r.getAttribute("data-share-link-id"));

beforeEach(() => {
  __resetMock();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ShareLinksPage", () => {
  it("lists every live link newest first: kind, target, creator and expiry (∞ when permanent)", async () => {
    const expires = Math.floor(Date.now() / 1000) + 3 * 86_400;
    const file = await api.createAttachmentShareLink("att-0123456789ab", expires);
    const diff = await api.createDiffShareLink(
      { before: "att-0123456789ab", after: "att-fedcba987654", labelBefore: "v2", labelAfter: "v3" },
      null,
    );

    renderPage();

    await waitFor(() => expect(rowIds()).toEqual([diff.link.id, file.link.id]));
    const [diffRow, fileRow] = screen.getAllByTestId("share-link-row");
    expect(within(diffRow).getByRole("img", { name: zh.shareLinks.kindDiff })).toBeTruthy();
    expect(within(diffRow).getByTestId("share-link-name").textContent).toBe("v2 → v3");
    expect(within(diffRow).getByRole("img", { name: zh.shareLinks.forever })).toBeTruthy();
    expect(within(fileRow).getByRole("img", { name: zh.shareLinks.kindAttachment })).toBeTruthy();
    expect(within(fileRow).getByTestId("share-link-expires").textContent).toBe(
      formatAbsolute(expires, Math.floor(Date.now() / 1000)),
    );
  });

  it("revokes one link only after confirming, and the others stay", async () => {
    const keep = await api.createAttachmentShareLink("att-0123456789ab", null);
    const drop = await api.createAttachmentShareLink("att-0123456789ab", null);
    const revoke = vi.spyOn(api, "revokeShareLink");

    renderPage();
    await waitFor(() => expect(rowIds()).toEqual([drop.link.id, keep.link.id]));
    fireEvent.click(within(screen.getAllByTestId("share-link-row")[0]).getByTestId("share-link-revoke"));

    expect(revoke).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("share-links-revoke-confirm-btn"));

    await waitFor(() => expect(rowIds()).toEqual([keep.link.id]));
    expect(revoke).toHaveBeenCalledWith(drop.link.id);
    expect(screen.queryByTestId("share-links-revoke-confirm")).toBeNull();
  });

  it("says so when no link is in force", async () => {
    renderPage();
    expect((await screen.findByTestId("share-links-empty")).textContent).toBe(zh.shareLinks.empty);
  });
});
