// DiffModalHost — a compare link inside the studio opens the comparison IN
// PLACE (T-59).
//
// This is the owner's acceptance sentence, so it is pinned as behaviour and not
// as wiring: the reader clicks a link in the message they are reading, the
// comparison appears over it, and closing puts them back on the same message,
// still mounted, with the address bar untouched.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { Markdown } from "./Markdown";
import { DiffModalHost } from "./DiffModalHost";
import type { DiffPairView } from "../types";

const PAIR: DiffPairView = {
  before: { address: "att-0123456789ab", text: "alpha\nbravo", label: "改動前", gone: false },
  after: { address: "att-fedcba987654", text: "alpha\nBRAVO", label: "改動後", gone: false },
};

const compareHref = (origin = window.location.origin) =>
  `${origin}/diff?before=att-0123456789ab&after=att-fedcba987654`;

function readingAMessage(href: string) {
  const getDiff = vi.spyOn(api, "getDiff").mockResolvedValue(PAIR);
  render(
    <I18nProvider>
      <DiffModalHost>
        <div>
          <p>這則訊息裡有一個比較</p>
          <Markdown source={`看這個 [比較](${href})`} />
        </div>
      </DiffModalHost>
    </I18nProvider>,
  );
  return { getDiff, link: screen.getByText("比較") };
}

describe("DiffModalHost", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("opens the comparison over the message, and closing returns to it", async () => {
    const before = window.location.href;
    const { link } = readingAMessage(compareHref());

    fireEvent.click(link);
    await waitFor(() => expect(screen.getByTestId("diff-screen")).toBeTruthy());
    // The message the reader was on never went anywhere — no navigation, no
    // lost position, which is the whole point of doing this in place.
    expect(screen.getByText("這則訊息裡有一個比較")).toBeTruthy();
    expect(window.location.href).toBe(before);

    fireEvent.keyDown(window, { key: "Escape" });
    await waitFor(() => expect(screen.queryByTestId("diff-screen")).toBeNull());
    expect(screen.getByText("這則訊息裡有一個比較")).toBeTruthy();
    expect(window.location.href).toBe(before);
  });

  it("leaves a modified click alone so open-in-new-tab still works", async () => {
    const { getDiff, link } = readingAMessage(compareHref());
    for (const modifier of [{ metaKey: true }, { ctrlKey: true }, { shiftKey: true }]) {
      const event = fireEvent.click(link, modifier);
      // Not swallowed: the browser is left to do what the reader asked for.
      expect(event).toBe(true);
    }
    expect(getDiff).not.toHaveBeenCalled();
    expect(screen.queryByTestId("diff-screen")).toBeNull();
  });

  it("does not swallow a compare-shaped link from another origin", () => {
    const { getDiff, link } = readingAMessage(compareHref("https://evil.example"));
    fireEvent.click(link);
    expect(getDiff).not.toHaveBeenCalled();
    expect(screen.queryByTestId("diff-screen")).toBeNull();
  });

  it("does not swallow an ordinary link", () => {
    const { getDiff, link } = readingAMessage("https://example.com/docs");
    fireEvent.click(link);
    expect(getDiff).not.toHaveBeenCalled();
    expect(screen.queryByTestId("diff-screen")).toBeNull();
  });
});
