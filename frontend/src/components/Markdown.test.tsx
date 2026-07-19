// Markdown renderer — the minimal, XSS-safe subset used by seeds + owner task
// manuals. Regression focus: a numbered step whose sub-content is indented
// (sub-bullets / code) must stay ONE ordered list with continuous numbering,
// not collapse into many single-item lists each restarting at 1 (the bug Seth
// hit pasting a PR-review SOP: "全部都是 1. 開始").

import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { Markdown } from "./Markdown";

function renderMd(source: string): HTMLElement {
  const { container } = render(<Markdown source={source} />);
  return container;
}

describe("Markdown", () => {
  const SOP = [
    "1. **接手** — 看 PR 狀態:",
    "   - PR 已 merged → 結案",
    "   - PR 是 draft → 請作者 ready",
    "2. 確認 rhapsody 是否 review 過:",
    "   - 已經有 → 跳步驟 5",
    "   - 還沒有 → 往下",
    "3. 觸發 review — 加 `8thEdition` 為 reviewer:",
    "   ```",
    "   gh pr edit 1 --add-reviewer 8thEdition",
    "   ```",
    "4. 等 webhook",
    "5. 依 verdict 決定",
    "6. 收尾",
  ].join("\n");

  it("keeps a numbered list with indented sub-content as one continuously-numbered list", () => {
    const c = renderMd(SOP);
    // The bug produced six separate <ol>s (each restarting at 1); the fix is one.
    expect(c.querySelectorAll("ol").length).toBe(1);
    const items = c.querySelectorAll("ol > li");
    expect(items.length).toBe(6);
    // Source numbering is preserved on each item (survives even if a list ever
    // does split), so the browser never renumbers them all to 1.
    expect(items[0].getAttribute("value")).toBe("1");
    expect(items[5].getAttribute("value")).toBe("6");
  });

  it("nests a list item's indented sub-bullets instead of leaking them as siblings", () => {
    const c = renderMd(SOP);
    const first = c.querySelector("ol > li");
    const nested = first?.querySelector("ul");
    expect(nested).not.toBeNull();
    expect(nested?.querySelectorAll("li").length).toBe(2);
  });

  it("renders an indented fenced code block as a code element", () => {
    const c = renderMd(SOP);
    const code = c.querySelector("pre code");
    expect(code).not.toBeNull();
    expect(code?.textContent).toContain("gh pr edit 1 --add-reviewer 8thEdition");
  });

  it("renders a flat ordered list as one list with its items", () => {
    const c = renderMd("1. first\n2. second\n3. third");
    expect(c.querySelectorAll("ol").length).toBe(1);
    const items = c.querySelectorAll("ol > li");
    expect(items.length).toBe(3);
    expect(items[0].textContent).toContain("first");
    expect(items[2].textContent).toContain("third");
  });

  it("renders unordered lists, headings, blockquotes, and inline bold/code", () => {
    const c = renderMd(
      "## Title\n- one\n- two\n> quoted line\nplain **bold** and `code` text",
    );
    expect(c.querySelector("h2")?.textContent).toBe("Title");
    expect(c.querySelectorAll("ul > li").length).toBe(2);
    expect(c.querySelector("blockquote")?.textContent).toContain("quoted line");
    expect(c.querySelector("strong")?.textContent).toBe("bold");
    expect(c.querySelector("code")?.textContent).toBe("code");
  });

  it("renders unknown syntax as plain text without injecting markup", () => {
    const c = renderMd("<script>alert(1)</script> just text");
    expect(c.querySelector("script")).toBeNull();
    expect(c.textContent).toContain("<script>alert(1)</script> just text");
  });

  // T-13af: task card description / step DoD / reply-card body all pass
  // owner- or agent-authored text through this renderer — links are the one
  // inline element whose target is attacker-influenceable, so a bad scheme
  // must fall back to literal text instead of becoming a clickable <a>.
  it("renders a [text](url) link with a safe scheme as an anchor with hardened target/rel", () => {
    const c = renderMd("see [the docs](https://example.com/docs) for detail");
    const a = c.querySelector("a");
    expect(a).not.toBeNull();
    expect(a?.getAttribute("href")).toBe("https://example.com/docs");
    expect(a?.textContent).toBe("the docs");
    expect(a?.getAttribute("target")).toBe("_blank");
    expect(a?.getAttribute("rel")).toBe("noopener noreferrer");
  });

  it("renders a mailto: link as an anchor", () => {
    const c = renderMd("[contact](mailto:owner@example.com)");
    expect(c.querySelector("a")?.getAttribute("href")).toBe(
      "mailto:owner@example.com",
    );
  });

  it("falls back to literal text for an unsafe link scheme (javascript:)", () => {
    const c = renderMd("[click me](javascript:alert(1))");
    expect(c.querySelector("a")).toBeNull();
    expect(c.textContent).toContain("[click me](javascript:alert(1))");
  });

  // T-84c8 — the `breaks` option. Chat needs Enter to mean "new line"; every
  // other call site needs standard markdown soft-wrap. Both halves are pinned
  // because the DEFAULT is what protects the pre-existing call sites.
  describe("breaks option (T-84c8)", () => {
    it("DEFAULTS OFF: single newlines fold into one run, standard markdown", () => {
      const c = renderMd("line1\nline2\nline3");
      expect(c.querySelectorAll("br").length).toBe(0);
      expect(c.querySelectorAll("p").length).toBe(1);
      expect(c.querySelector("p")?.textContent).toBe("line1 line2 line3");
    });

    it("ON: single newlines become hard <br> breaks inside one paragraph", () => {
      const { container } = render(
        <Markdown source={"line1\nline2\nline3"} breaks />,
      );
      expect(container.querySelectorAll("br").length).toBe(2);
      expect(container.querySelectorAll("p").length).toBe(1);
      // Every line survived — and was not welded together with a space.
      expect(container.textContent).toContain("line1");
      expect(container.textContent).toContain("line3");
      expect(container.textContent).not.toContain("line1 line2");
    });

    it("ON: inline markdown still parses on each broken line", () => {
      const { container } = render(
        <Markdown source={"**bold**\n`code`"} breaks />,
      );
      expect(container.querySelector("strong")?.textContent).toBe("bold");
      expect(container.querySelector("code")?.textContent).toBe("code");
      expect(container.querySelectorAll("br").length).toBe(1);
    });

    it("ON: a fenced code block is untouched by breaks (no <br> inside <pre>)", () => {
      const { container } = render(
        <Markdown source={"```\na\nb\n```"} breaks />,
      );
      expect(container.querySelector("pre code")?.textContent).toBe("a\nb");
      expect(container.querySelectorAll("pre br").length).toBe(0);
    });
  });
});
