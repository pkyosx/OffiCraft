// DiffShareLinkButton — the comparison's share menu. The menu itself is pinned
// in ShareLinkMenu.test.tsx; what is pinned here is that a comparison mints,
// lists and names itself by its two sides.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { zh } from "../i18n/locales/zh";
import { DiffShareLinkButton } from "./DiffShareLinkButton";
import type { ShareLinkView } from "../types";

const PARAMS = {
  before: "att-0123456789ab",
  after: "doc:global_context/global/current/text",
  labelBefore: "改動前",
  link: "a-credential-the-page-was-opened-with",
};

const LINK: ShareLinkView = {
  id: "sl-0123456789ab",
  kind: "diff",
  attachmentId: null,
  attachmentFilename: null,
  before: PARAMS.before,
  after: PARAMS.after,
  labelBefore: "改動前",
  labelAfter: "",
  createdBy: "owner",
  createdTs: 1792650600,
  expiresTs: null,
};

const MINTED = "/diff?after=doc%3Ax&before=att-0123456789ab&link=minted";

function mount() {
  return render(
    <I18nProvider>
      <DiffShareLinkButton params={PARAMS} className="md-preview__share" />
    </I18nProvider>,
  );
}

describe("DiffShareLinkButton", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("lists this comparison's links by its two sides and mints for the same params", async () => {
    const list = vi.spyOn(api, "listShareLinks").mockResolvedValue([]);
    const mint = vi
      .spyOn(api, "createDiffShareLink")
      .mockResolvedValue({ url: MINTED, link: LINK });
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });

    mount();
    fireEvent.click(screen.getByRole("button", { name: zh.shareLinks.trigger }));
    await waitFor(() =>
      expect(list).toHaveBeenCalledWith({ before: PARAMS.before, after: PARAMS.after }),
    );
    fireEvent.click(screen.getByTestId("share-generate"));

    await waitFor(() => expect(mint).toHaveBeenCalledWith(PARAMS, expect.any(Number)));
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith(`${window.location.origin}${MINTED}`),
    );
  });

  it("names the comparison by its headings in the revoke confirmation", async () => {
    vi.spyOn(api, "listShareLinks").mockResolvedValue([LINK]);

    mount();
    fireEvent.click(screen.getByRole("button", { name: zh.shareLinks.trigger }));
    fireEvent.click(await screen.findByTestId("share-revoke"));

    expect(screen.getByTestId("share-revoke-confirm").textContent).toContain(
      `改動前 → ${PARAMS.after}`,
    );
  });
});
