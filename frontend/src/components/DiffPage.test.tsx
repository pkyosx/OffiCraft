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

const PARAMS = {
  before: "att-0123456789ab",
  after: "att-fedcba987654",
  sig: "signature",
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
    // The signature travels with the read — it is the page's whole credential.
    expect(getDiff).toHaveBeenCalledWith(expect.objectContaining({ sig: "signature" }));
    // No nav, no badges: nothing on this page asks the server who is looking.
    expect(screen.queryByText(zh.nav.office)).toBeNull();
    expect(document.title).toBe(zh.diff.ariaLabel);
  });

  // T-59 — WHO MAY MINT. `GET /api/diff/share-link` is gated like every other
  // route, and the reader of a ?sig= link is the one reader who may have no
  // session at all. Offering them the control would be a button that fails on
  // click, and a way to re-mint a link they were merely sent.
  it("offers NO external-link control to the signed reader, who has no session", async () => {
    const mint = vi.spyOn(api, "getDiffShareLink");
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
    expect(screen.queryByRole("button", { name: zh.diff.copyShareLink })).toBeNull();
    expect(mint).not.toHaveBeenCalled();
  });

  // The OTHER flavour of this page: no signature means main.tsx put it behind
  // AuthGate, so the reader is signed in and the mint will answer.
  it("offers the external-link icon on the unsigned flavour, which is behind the auth wall", async () => {
    const minted = "/diff?before=att-0123456789ab&after=att-fedcba987654&sig=minted";
    const mint = vi.spyOn(api, "getDiffShareLink").mockResolvedValue(minted);
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
    const button = screen.getByRole("button", { name: zh.diff.copyShareLink });
    // Icon-only (owner 2026-09-03「1. 用圖示」), and it sits above the
    // comparison rather than inside it.
    expect(button.textContent).toBe("");
    expect(button.compareDocumentPosition(screen.getByTestId("diff-screen")) & 4).toBe(4);

    fireEvent.click(button);
    await waitFor(() => expect(mint).toHaveBeenCalledWith(internal));
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith(`${window.location.origin}${minted}`),
    );
  });
});
