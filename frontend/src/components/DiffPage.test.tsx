// DiffPage — the compare url opened as ITS OWN page (T-59).
//
// The reader here may have arrived from Slack with no session at all, so what
// this pins is as much about what is ABSENT as what is drawn: the comparison,
// a tab title that says what the page is, and nothing that would need a login
// to render.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { api } from "../api";
import { DiffPage } from "./DiffPage";
import { ApiError } from "../api/errors";
import { codeForStatus } from "../api/errorCodes";

const PARAMS = {
  before: "att-0123456789ab",
  after: "att-fedcba987654",
  link: "share-link-credential",
};

describe("DiffPage", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("draws the comparison and nothing that would need a session", async () => {
    const getDiff = vi.spyOn(api, "getDiff").mockResolvedValue({
      before: { address: "att-0123456789ab", text: "alpha\nbravo", label: "改動前", gone: false },
      after: { address: "att-fedcba987654", text: "alpha\nBRAVO", label: "改動後", gone: false },
    });

    render(
      <I18nProvider>
        <DiffPage params={PARAMS} />
      </I18nProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("diff-screen")).toBeTruthy());
    // The link travels with the read — it is the page's whole credential.
    expect(getDiff).toHaveBeenCalledWith(
      expect.objectContaining({ link: "share-link-credential" }),
    );
    // No nav, no badges: nothing on this page asks the server who is looking.
    expect(screen.queryByText(zh.nav.office)).toBeNull();
    expect(document.title).toBe(zh.diff.ariaLabel);
  });

  it("says the link is no longer valid when it has expired or been revoked", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(api, "getDiff").mockRejectedValue(
      new ApiError("http 410 for GET /api/diff", 410, codeForStatus(410), "gone"),
    );

    render(
      <I18nProvider>
        <DiffPage params={PARAMS} />
      </I18nProvider>,
    );

    expect((await screen.findByTestId("diff-screen-link-gone")).textContent).toBe(
      zh.shareLinks.linkGone,
    );
  });

  // T-59 — WHO MAY MINT. The mint is gated like every other route, and the
  // reader of a ?link= link is the one reader who may have no
  // session at all. Offering them the control would be a button that fails on
  // click, and a way to re-mint a link they were merely sent.
  it("offers NO share control to the external reader, who has no session", async () => {
    const mint = vi.spyOn(api, "createDiffShareLink");
    vi.spyOn(api, "getDiff").mockResolvedValue({
      before: { address: "att-0123456789ab", text: "alpha", label: "改動前", gone: false },
      after: { address: "att-fedcba987654", text: "beta", label: "改動後", gone: false },
    });

    render(
      <I18nProvider>
        <DiffPage params={PARAMS} />
      </I18nProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("diff-screen")).toBeTruthy());
    expect(screen.queryByTestId("diff-share-link")).toBeNull();
    expect(screen.queryByRole("button", { name: zh.shareLinks.trigger })).toBeNull();
    expect(mint).not.toHaveBeenCalled();
  });

  // The OTHER flavour of this page: no credential means main.tsx put it behind
  // AuthGate, so the reader is signed in and the mint will answer.
  it("offers the share menu on the internal flavour, which is behind the auth wall", async () => {
    const minted = "/diff?before=att-0123456789ab&after=att-fedcba987654&link=minted";
    vi.spyOn(api, "listShareLinks").mockResolvedValue([]);
    const mint = vi.spyOn(api, "createDiffShareLink").mockResolvedValue({
      url: minted,
      link: {
        id: "sl-0123456789ab",
        kind: "diff",
        attachmentId: null,
        attachmentFilename: null,
        before: "att-0123456789ab",
        after: "att-fedcba987654",
        labelBefore: "",
        labelAfter: "",
        createdBy: "owner",
        createdTs: 1792650600,
        expiresTs: 1793255400,
      },
    });
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    vi.spyOn(api, "getDiff").mockResolvedValue({
      before: { address: "att-0123456789ab", text: "alpha", label: "改動前", gone: false },
      after: { address: "att-fedcba987654", text: "beta", label: "改動後", gone: false },
    });

    const internal = { before: PARAMS.before, after: PARAMS.after };
    render(
      <I18nProvider>
        <DiffPage params={internal} />
      </I18nProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("diff-screen")).toBeTruthy());
    const button = screen.getByRole("button", { name: zh.shareLinks.trigger });
    // Icon-only (owner 2026-09-03「1. 用圖示」), and it sits above the
    // comparison rather than inside it.
    expect(button.textContent).toBe("");
    expect(button.compareDocumentPosition(screen.getByTestId("diff-screen")) & 4).toBe(4);

    fireEvent.click(button);
    fireEvent.click(await screen.findByTestId("share-generate"));
    await waitFor(() => expect(mint).toHaveBeenCalledWith(internal, expect.any(Number)));
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith(`${window.location.origin}${minted}`),
    );
  });
});
