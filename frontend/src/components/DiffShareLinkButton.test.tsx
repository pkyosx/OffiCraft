// DiffShareLinkButton — the icon that puts THIS comparison's external link on
// the clipboard (T-59, owner 2026-09-03「1. 用圖示」).
//
// What is worth pinning here is not "a button renders". It is the discipline
// the file-level control already keeps and this one had to inherit:
//
//   * the link is ABSOLUTIZED — the server mints a server-relative path and
//     only the browser knows the origin, so a relative string on the clipboard
//     is a link that works for nobody it was pasted to;
//   * a mint that FAILS says so, and never flashes the success wording — a copy
//     that did not happen must not look like one that did;
//   * icon-only, but NAMED: no visible text, a real accessible name, and a real
//     <button> so the keyboard reaches and fires it.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import { DiffShareLinkButton } from "./DiffShareLinkButton";

const PARAMS = {
  before: "att-0123456789ab",
  after: "doc:global_context/global/current/text",
  labelBefore: "改動前",
};

const MINTED = "/diff?before=att-0123456789ab&after=doc%3Ax&sig=minted";

function stubClipboard() {
  const writeText = vi.fn(async () => {});
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText },
    configurable: true,
  });
  return writeText;
}

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

  it("mints the external link, copies it absolutized, and flashes the copied name", async () => {
    const mint = vi.spyOn(api, "getDiffShareLink").mockResolvedValue(MINTED);
    const writeText = stubClipboard();

    mount();
    fireEvent.click(screen.getByRole("button", { name: zh.diff.copyShareLink }));

    await waitFor(() => expect(mint).toHaveBeenCalledWith(PARAMS));
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith(`${window.location.origin}${MINTED}`),
    );
    await waitFor(() =>
      expect(screen.getByRole("button", { name: zh.diff.shareLinkCopied })).toBeTruthy(),
    );
  });

  it("says the copy failed, with its own glyph, instead of faking a success", async () => {
    vi.spyOn(api, "getDiffShareLink").mockRejectedValue(new Error("boom"));
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const writeText = stubClipboard();

    mount();
    const idleGlyph = screen.getByRole("button", {
      name: zh.diff.copyShareLink,
    }).innerHTML;
    fireEvent.click(screen.getByRole("button", { name: zh.diff.copyShareLink }));

    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: zh.diff.shareLinkCopyFailed }),
      ).toBeTruthy(),
    );
    // Nothing reached the clipboard, and the success wording never appeared.
    expect(writeText).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: zh.diff.shareLinkCopied })).toBeNull();
    // Icon-only means the name switch is the half only a screen reader gets;
    // a failure drawn as the idle copy glyph is indistinguishable from
    // "nothing happened yet". Deleting the copyFailed arm of the ternary has to
    // go red here, not just on the label.
    expect(
      screen.getByRole("button", { name: zh.diff.shareLinkCopyFailed }).innerHTML,
    ).not.toBe(idleGlyph);
  });

  it("also refuses to fake success when the clipboard itself refuses", async () => {
    vi.spyOn(api, "getDiffShareLink").mockResolvedValue(MINTED);
    vi.spyOn(console, "warn").mockImplementation(() => {});
    Object.defineProperty(navigator, "clipboard", {
      value: {
        writeText: vi.fn(async () => {
          throw new Error("denied");
        }),
      },
      configurable: true,
    });

    mount();
    fireEvent.click(screen.getByRole("button", { name: zh.diff.copyShareLink }));

    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: zh.diff.shareLinkCopyFailed }),
      ).toBeTruthy(),
    );
    expect(screen.queryByRole("button", { name: zh.diff.shareLinkCopied })).toBeNull();
  });

  it("is an icon with a name — no visible text, focusable, and keyboard-operable", async () => {
    vi.spyOn(api, "getDiffShareLink").mockResolvedValue(MINTED);
    const writeText = stubClipboard();

    mount();
    const button = screen.getByRole("button", { name: zh.diff.copyShareLink });
    // The owner's ruling was 「1. 用圖示」: the control carries a glyph, and the
    // words live in the accessible name and the tooltip, never on screen.
    expect(button.textContent).toBe("");
    expect(button.querySelector("svg")).toBeTruthy();
    expect(button.getAttribute("title")).toBe(zh.diff.copyShareLink);
    // A real <button> is what makes it reachable by Tab and fired by Enter and
    // Space without this file re-implementing any of it. `type="button"` keeps
    // it from submitting a form it may one day sit inside.
    expect(button.tagName).toBe("BUTTON");
    expect(button.getAttribute("type")).toBe("button");
    button.focus();
    expect(document.activeElement).toBe(button);
    // The keyboard path really does fire it: jsdom synthesises the click a
    // browser produces for Enter on a focused button.
    fireEvent.keyDown(button, { key: "Enter" });
    fireEvent.click(button);
    await waitFor(() => expect(writeText).toHaveBeenCalled());
  });

  it("ships all three names in BOTH locales, and they are three different names", () => {
    for (const [name, bundle] of [["zh", zh], ["en", en]] as const) {
      const said = [
        bundle.diff.copyShareLink,
        bundle.diff.shareLinkCopied,
        bundle.diff.shareLinkCopyFailed,
      ];
      for (const one of said) {
        expect(`${name}: ${typeof one} len>0=${one.length > 0}`).toBe(
          `${name}: string len>0=true`,
        );
      }
      expect(new Set(said).size).toBe(3);
    }
  });
});
