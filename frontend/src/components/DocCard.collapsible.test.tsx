// DocCard's `collapsible` branch — pinned here BECAUSE NOTHING SHIPS IT (T-bac4).
//
// The page it was built for (啟動程序 stacking Claude + Codex) is gone; 啟動程序
// is an index of two rows now, one document per page. The prop survived that
// removal on purpose — its two labels are theme WORDING CODES a customised
// theme pack may already carry values for, and dropping codes makes those
// values vanish silently on import, which is an external effect outside the
// ticket that removed the page. DocCard's own docstring carries that reasoning.
//
// 🔴 WHAT THIS FILE BUYS. Unexercised code with a plausible docstring is how a
// future reader ends up "fixing" or reusing a path nobody has run. If the
// branch is going to outlive its caller, it has to stay honest under test. The
// independent review on this pack raised exactly that, and this is the answer:
// keep it, and pin it.
//
// ⚠️ IF YOU RETIRE THE PROP, retire this file and the `docExpand` /
// `docCollapse` wording codes in the same change — a test standing over a
// deleted branch is the same defect in the other direction.
//
// ⚠️ AND IF YOU PUT IT BACK INTO A PAGE, read T-fc57 first: collapsing the LAST
// card of a scrolled-to-the-bottom page shortens the page under the reader and
// the browser clamps scrollTop, sliding the heading down (measured 389.9 →
// 753.9 on the page that used to ship it). No scroll correction can undo it,
// which is why DocCard carries none. That is a property of the STACKED page,
// not of this branch alone — but the branch is what makes it reachable.

import { describe, it, expect } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { DocCard, type DocCardProps } from "./DocCard";

const s = zh.settings;
const DOC = { text: "# 這份文件的內文", isDefault: false };
const NOOP = async () => {};

function renderCard(over: Partial<DocCardProps> = {}) {
  const props: DocCardProps = {
    title: "啟動程序（Claude Code）",
    doc: DOC,
    crumbs: [{ label: "設定" }, { label: "啟動程序（Claude Code）" }],
    onSave: NOOP,
    ...over,
  };
  return render(
    <I18nProvider>
      <DocCard {...props} />
    </I18nProvider>
  );
}

describe("DocCard · collapsible", () => {
  it("is OFF by default — a page with one document shows it", () => {
    const utils = renderCard();
    expect(utils.container.querySelectorAll(".doc-md").length).toBe(1);
    expect(
      utils.container.querySelectorAll('[data-testid="doc-card-collapse"]')
        .length
    ).toBe(0);
  });

  it("starts CLOSED when asked, and the heading toggles it", () => {
    const utils = renderCard({ collapsible: true });

    // Closed means the body is NOT RENDERED, not merely hidden — that is the
    // whole reason the shape was reached for: two closed cards fit one phone
    // screen, two open ones do not.
    expect(utils.container.querySelectorAll(".doc-md").length).toBe(0);
    const toggle = utils.getByTestId("doc-card-collapse");
    expect(toggle.getAttribute("aria-expanded")).toBe("false");

    fireEvent.click(toggle);
    expect(utils.container.querySelectorAll(".doc-md").length).toBe(1);
    expect(
      utils.getByTestId("doc-card-collapse").getAttribute("aria-expanded")
    ).toBe("true");

    fireEvent.click(utils.getByTestId("doc-card-collapse"));
    expect(utils.container.querySelectorAll(".doc-md").length).toBe(0);
  });

  it("🔴 the toggle takes its accessible name from the VISIBLE TITLE", () => {
    // This is the defect an independent review found on T-6278 and it is the
    // reason to keep this branch under test at all: an aria-label once stood on
    // this button reading 展開這份文件, so EVERY collapsed card announced the
    // same name and a screen reader could not tell 啟動程序（Claude Code）from
    // （Codex CLI）— the very defect that page existed to fix, rebuilt in the
    // accessibility tree, plus WCAG 2.5.3 Label in Name.
    const utils = renderCard({ collapsible: true });
    expect(
      utils.getByRole("button", { name: "啟動程序（Claude Code）" })
    ).toBeTruthy();
    expect(utils.getByTestId("doc-card-collapse").getAttribute("aria-label")).toBeNull();
  });

  it("…EXCEPT when the title is renameable and therefore sits outside the button", () => {
    // Then there is no visible text inside the button to be its name, and a
    // nameless button is worse than a generic one. No page combines the two
    // today; the branch exists so that one cannot silently lose its rename.
    const utils = renderCard({
      collapsible: true,
      onRenameTitle: NOOP as never,
    });
    expect(utils.getByTestId("doc-card-collapse").getAttribute("aria-label")).toBe(
      s.docExpand
    );
    fireEvent.click(utils.getByTestId("doc-card-collapse"));
    expect(utils.getByTestId("doc-card-collapse").getAttribute("aria-label")).toBe(
      s.docCollapse
    );
  });
});
