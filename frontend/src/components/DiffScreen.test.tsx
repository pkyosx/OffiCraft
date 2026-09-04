// DiffScreen — the compare body: two addresses in, one comparison on screen
// (T-59).
//
// What is pinned here:
//   1. ONE read, with the url's own params — the reader resolves nothing;
//   2. the comparison is drawn in the right DIRECTION, with each side's heading;
//   3. a side that resolved to NOTHING is said, never drawn as an empty side;
//   4. a failed read and a side that is GONE are two different sentences;
//   5. either heading opens that side on its own, from the pair already in hand.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { api } from "../api";
import { mockApiError } from "../api/errorCodes";
import { DiffScreen } from "./DiffScreen";
import type { DiffPairView } from "../types";

const PARAMS = {
  before: "att-0123456789ab",
  after: "doc:global_context/global/current/text",
};

const PAIR: DiffPairView = {
  before: { address: PARAMS.before, text: "alpha\nbravo\ncharlie", label: "9/2 21:12", gone: false },
  after: { address: PARAMS.after, text: "alpha\nBRAVO\ncharlie", label: "目前存檔內容", gone: false },
};

function open(pair: DiffPairView = PAIR) {
  const getDiff = vi.spyOn(api, "getDiff").mockResolvedValue(pair);
  render(
    <I18nProvider>
      <DiffScreen params={PARAMS} />
    </I18nProvider>,
  );
  return getDiff;
}

describe("DiffScreen", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("asks for both sides ONCE, with the addresses the url spelled", async () => {
    const getDiff = open();
    await waitFor(() => expect(screen.getByTestId("diff-screen")).toBeTruthy());
    expect(getDiff).toHaveBeenCalledTimes(1);
    expect(getDiff).toHaveBeenCalledWith({
      before: PARAMS.before,
      after: PARAMS.after,
      labelBefore: undefined,
      labelAfter: undefined,
      sig: undefined,
    });
  });

  it("draws the two sides in the right direction, each under its own heading", async () => {
    open();
    await waitFor(() => expect(screen.getByTestId("diff-screen")).toBeTruthy());
    const diff = screen.getByTestId("diff-screen");
    // Presence alone survives the two sides being swapped, and a swap is the
    // failure that lies loudest: the reader sees the new version struck out and
    // the old one added, and concludes the change did the opposite of what it
    // did.
    expect(diff.querySelector('[data-kind="removed"] .diff-view__text')?.textContent).toBe(
      "bravo",
    );
    expect(diff.querySelector('[data-kind="added"] .diff-view__text')?.textContent).toBe(
      "BRAVO",
    );
    expect(diff.querySelector(".diff-view__label--before")?.textContent).toContain(
      "9/2 21:12",
    );
    expect(diff.querySelector(".diff-view__label--after")?.textContent).toContain(
      "目前存檔內容",
    );
  });

  it("falls back to the diff's own words for an unlabelled BLOB side", async () => {
    // A blob id is not a heading, and there is nothing else to say about it.
    open({
      before: { address: "att-0123456789ab", text: "alpha", gone: false },
      after: { address: "att-fedcba987654", text: "beta", gone: false },
    });
    await waitFor(() => expect(screen.getByTestId("diff-screen")).toBeTruthy());
    const diff = screen.getByTestId("diff-screen");
    expect(diff.querySelector(".diff-view__label--before")?.textContent).toBe(
      `-${zh.diff.beforeLabel}`,
    );
    expect(diff.querySelector(".diff-view__label--after")?.textContent).toBe(
      `+${zh.diff.afterLabel}`,
    );
  });

  it("names an unlabelled DOCUMENT side in the reader's own language", async () => {
    // The server deliberately sends no label for these: 「初始版本」 written once
    // at mint time would be that language for every later reader.
    open({
      before: { address: "doc:global_context/global/seed/text", text: "alpha", gone: false },
      after: { address: "doc:global_context/global/12/text", text: "beta", gone: false },
    });
    await waitFor(() => expect(screen.getByTestId("diff-screen")).toBeTruthy());
    const diff = screen.getByTestId("diff-screen");
    expect(diff.querySelector(".diff-view__label--before")?.textContent).toBe(
      `-${zh.chat.mdPreview.diffSideSeed}`,
    );
    expect(diff.querySelector(".diff-view__label--after")?.textContent).toBe(
      `+${zh.chat.mdPreview.diffSideRevision("12")}`,
    );
  });

  it("marks a LIVE side as live, and stamps when it was read", async () => {
    // The same link opened next month compares against a different document.
    // The reader has to see that on screen rather than infer it, and the stamp
    // is what keeps the sentence true once the screen is shared or photographed.
    open({
      before: { address: "att-0123456789ab", text: "alpha", gone: false },
      after: { address: "doc:global_context/global/current/text", text: "beta", gone: false },
    });
    await waitFor(() => expect(screen.getByTestId("diff-screen")).toBeTruthy());
    const heading = screen
      .getByTestId("diff-screen")
      .querySelector(".diff-view__label--after")?.textContent;
    expect(heading).toContain(zh.chat.mdPreview.diffSideCurrent);
    expect(heading).toMatch(/讀取於 .+，之後會不一樣/);
  });

  it("says a side is gone instead of comparing the survivor against nothing", async () => {
    open({
      before: { address: PARAMS.before, text: "", gone: true },
      after: { address: PARAMS.after, text: "alpha\nbravo", label: "目前存檔內容", gone: false },
    });
    await waitFor(() =>
      expect(screen.getByText(zh.chat.mdPreview.diffSideGone)).toBeTruthy(),
    );
    // Drawing the half it DID get would mark every one of its lines as added —
    // a confident wrong answer to "what changed".
    expect(screen.queryByTestId("diff-screen")).toBeNull();
    expect(screen.queryByText("bravo")).toBeNull();
  });

  it("tells a failed read apart from a side that is gone", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(api, "getDiff").mockRejectedValue(
      mockApiError("http 500 for GET /api/diff", 500, "boom"),
    );
    render(
      <I18nProvider>
        <DiffScreen params={PARAMS} />
      </I18nProvider>,
    );
    await waitFor(() => expect(screen.getByText(zh.chat.mdPreview.error)).toBeTruthy());
    expect(screen.queryByText(zh.chat.mdPreview.diffSideGone)).toBeNull();
  });

  it("opens either side on its own from the pair in hand, and comes back", async () => {
    const getDiff = open();
    await waitFor(() => expect(screen.getByTestId("diff-screen")).toBeTruthy());

    fireEvent.click(screen.getByTestId("diff-view-side-before"));
    expect(screen.getByTestId("diff-screen-side-title").textContent).toBe("9/2 21:12");
    expect(screen.getByTestId("diff-screen-side").querySelector("pre")?.textContent).toBe(
      PAIR.before.text,
    );
    // The comparison is gone while one side is open — two diff surfaces at once
    // would leave "which one am I reading" to the reader.
    expect(screen.queryByTestId("diff-screen")).toBeNull();

    fireEvent.click(screen.getByTestId("diff-screen-side-back"));
    expect(screen.getByTestId("diff-screen")).toBeTruthy();

    fireEvent.click(screen.getByTestId("diff-view-side-after"));
    expect(screen.getByTestId("diff-screen-side").querySelector("pre")?.textContent).toBe(
      PAIR.after.text,
    );
    // A VIEW of what is already in hand: re-reading a live side could answer
    // differently and leave the two screens disagreeing about what it says.
    expect(getDiff).toHaveBeenCalledTimes(1);
  });
});
