// Markdown renderer — the minimal, XSS-safe subset used by seeds + owner task
// manuals. Regression focus: a numbered step whose sub-content is indented
// (sub-bullets / code) must stay ONE ordered list with continuous numbering,
// not collapse into many single-item lists each restarting at 1 (the bug Seth
// hit pasting a PR-review SOP: "全部都是 1. 開始").

import { describe, it, expect } from "vitest";
import { render, fireEvent } from "@testing-library/react";
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

  // 使用說明 (product guide) — block-level images, opt-in via resolveImageSrc.
  // The DEFAULT (no resolver) is what protects every pre-existing call site:
  // `![…](…)` must stay literal text there, never load an image.
  describe("resolveImageSrc option (product guide images)", () => {
    it("DEFAULTS OFF: a block image renders as literal text, no <img>", () => {
      const c = renderMd("![map](/api/docs/assets/map.png)");
      expect(c.querySelector("img")).toBeNull();
      expect(c.textContent).toContain("![map](/api/docs/assets/map.png)");
    });

    it("ON: a block image renders an <img> with the resolved src + alt", () => {
      const { container } = render(
        <Markdown
          source={"![map](/api/docs/assets/map.png)"}
          resolveImageSrc={(s) => `${s}?token=T`}
        />,
      );
      const img = container.querySelector("img");
      expect(img?.getAttribute("src")).toBe("/api/docs/assets/map.png?token=T");
      expect(img?.getAttribute("alt")).toBe("map");
    });

    it("ON: an unsafe/foreign image src falls through as literal text", () => {
      const { container } = render(
        <Markdown
          source={"![x](data:image/png;base64,AAAA)"}
          resolveImageSrc={(s) => s}
        />,
      );
      expect(container.querySelector("img")).toBeNull();
      expect(container.textContent).toContain("![x](data:image/png;base64,AAAA)");
    });
  });

  // T-bc3e — GFM tables. The trigger was an owner screenshot: an agent posted
  // a table in chat and the bubble showed the raw pipes. The renderer stays
  // minimal: header + |---| delimiter + rows become a real <table>; anything
  // that fails the GFM gate (no delimiter row, malformed delimiter, header /
  // delimiter column-count mismatch) falls through as plain text — same
  // safe-by-construction posture as every other unknown syntax.
  describe("GFM tables (T-bc3e)", () => {
    it("renders header + delimiter + data rows as a real table", () => {
      const c = renderMd(
        "| Name | Role |\n| --- | --- |\n| Kyle | dev |\n| Seth | owner |",
      );
      expect(c.querySelectorAll("table").length).toBe(1);
      const ths = c.querySelectorAll("thead th");
      expect(ths.length).toBe(2);
      expect(ths[0].textContent).toBe("Name");
      const rows = c.querySelectorAll("tbody tr");
      expect(rows.length).toBe(2);
      expect(rows[1].querySelectorAll("td")[1].textContent).toBe("owner");
      // The raw delimiter row must NOT leak into the rendered output.
      expect(c.textContent).not.toContain("---");
    });

    it("accepts rows without leading/trailing pipes (GFM optional decoration)", () => {
      const c = renderMd("a | b\n--- | ---\n1 | 2");
      expect(c.querySelectorAll("table").length).toBe(1);
      expect(c.querySelectorAll("thead th").length).toBe(2);
      expect(c.querySelector("tbody td")?.textContent).toBe("1");
    });

    it("runs cell content through renderInline (bold / code / safe links work)", () => {
      const c = renderMd(
        "| a | b |\n| --- | --- |\n| **bold** | `code` and [docs](https://x.dev) |",
      );
      const cell = c.querySelectorAll("tbody td");
      expect(cell[0].querySelector("strong")?.textContent).toBe("bold");
      expect(cell[1].querySelector("code")?.textContent).toBe("code");
      expect(cell[1].querySelector("a")?.getAttribute("href")).toBe(
        "https://x.dev",
      );
    });

    it("applies :--- / :---: / ---: alignment to header and body cells", () => {
      const c = renderMd(
        "| l | c | r | n |\n| :--- | :---: | ---: | --- |\n| 1 | 2 | 3 | 4 |",
      );
      const ths = c.querySelectorAll("thead th");
      expect((ths[0] as HTMLElement).style.textAlign).toBe("left");
      expect((ths[1] as HTMLElement).style.textAlign).toBe("center");
      expect((ths[2] as HTMLElement).style.textAlign).toBe("right");
      expect((ths[3] as HTMLElement).style.textAlign).toBe("");
      const tds = c.querySelectorAll("tbody td");
      expect((tds[1] as HTMLElement).style.textAlign).toBe("center");
      expect((tds[2] as HTMLElement).style.textAlign).toBe("right");
    });

    it("normalizes ragged data rows to the header width (GFM pad/truncate)", () => {
      const c = renderMd(
        "| a | b | c |\n| --- | --- | --- |\n| only |\n| 1 | 2 | 3 | extra |",
      );
      const rows = c.querySelectorAll("tbody tr");
      expect(rows.length).toBe(2);
      expect(rows[0].querySelectorAll("td").length).toBe(3);
      expect(rows[1].querySelectorAll("td").length).toBe(3);
      expect(rows[1].textContent).not.toContain("extra");
    });

    it("falls through as text when header/delimiter column counts mismatch", () => {
      const c = renderMd("| a | b | c |\n| --- | --- |\n| 1 | 2 |");
      expect(c.querySelector("table")).toBeNull();
      expect(c.textContent).toContain("| a | b | c |");
    });

    it("falls through as text for a header row with no delimiter row", () => {
      const c = renderMd("| just | a | header |");
      expect(c.querySelector("table")).toBeNull();
      expect(c.textContent).toContain("| just | a | header |");
    });

    it("falls through as text when the delimiter row is malformed", () => {
      const c = renderMd("| a | b |\n| --x-- | --- |\n| 1 | 2 |");
      expect(c.querySelector("table")).toBeNull();
      expect(c.textContent).toContain("--x--");
    });

    it("falls through for a delimiter cell with a misplaced colon (--:-)", () => {
      // `--:-` is built only of [|:-] characters, so it slips past any cheap
      // charset check — the per-cell `:?-+:?` shape rule must reject it.
      const c = renderMd("| a | b |\n| --:- | --- |\n| 1 | 2 |");
      expect(c.querySelector("table")).toBeNull();
      expect(c.textContent).toContain("--:-");
    });

    it("renders a header-plus-delimiter-only table (empty body) without crashing", () => {
      const c = renderMd("| a | b |\n| --- | --- |");
      expect(c.querySelectorAll("table").length).toBe(1);
      expect(c.querySelectorAll("thead th").length).toBe(2);
      expect(c.querySelector("tbody")).toBeNull();
    });

    // The chat surface: `breaks` turns every intra-paragraph newline into
    // <br> — table lines must be exempt (they are consumed whole, never via
    // renderParagraph), and a paragraph butting directly against a table must
    // still yield a table instead of swallowing it as prose.
    it("breaks mode: table renders with no <br> inside it, prose around it still hard-breaks", () => {
      const { container } = render(
        <Markdown
          source={"line1\nline2\n| a | b |\n| --- | --- |\n| 1 | 2 |"}
          breaks
        />,
      );
      expect(container.querySelectorAll("table").length).toBe(1);
      expect(container.querySelectorAll("table br").length).toBe(0);
      expect(container.querySelectorAll("tbody tr").length).toBe(1);
      // line1/line2 remain a hard-broken paragraph before the table.
      expect(container.querySelectorAll("p br").length).toBe(1);
      expect(container.textContent).not.toContain("|");
    });
  });
  // ── T-68f1 · resolveDocLink: the THIRD link class ────────────────────────
  //
  // The 使用說明 page ships the repo's doc tree, whose links are repo-RELATIVE
  // (`docs/guide/why.md`). SAFE_URL_RE only knows http/https/mailto, so those
  // rendered as literal `[a](b)` source text — that is the defect. The fix adds
  // an opt-in resolver, NOT a wider allowlist, so this suite is two-sided:
  //   (a) the feature works, and is threaded into every block that renders
  //       inline text (paragraph / heading / list / table / quote);
  //   (b) the DEFAULT (every chat / task / manual call site, all carrying
  //       agent-authored untrusted text) is unchanged, and the dangerous URL
  //       shapes NEVER even reach the resolver — asserted on the spy's call
  //       log, not just on the absence of a clickable node, because "the
  //       resolver declined" and "the resolver was never asked" look identical
  //       in the DOM.
  describe("resolveDocLink — in-app doc links (T-68f1)", () => {
    /** A resolver that accepts everything, recording what it was asked. */
    function spyResolver(accept = true) {
      const asked: string[] = [];
      const clicked: string[] = [];
      const resolve = (target: string) => {
        asked.push(target);
        return accept ? () => clicked.push(target) : null;
      };
      return { asked, clicked, resolve };
    }

    it("DEFAULTS OFF: a repo-relative .md link stays literal text everywhere else", () => {
      const c = renderMd("看 [為什麼](docs/guide/why.md) 這份");
      expect(c.querySelector("a")).toBeNull();
      expect(c.querySelector("button")).toBeNull();
      expect(c.textContent).toContain("[為什麼](docs/guide/why.md)");
    });

    it("ON: a repo-relative .md link becomes a clickable in-app button", () => {
      const spy = spyResolver();
      const { container } = render(
        <Markdown
          source="看 [為什麼](docs/guide/why.md) 這份"
          resolveDocLink={spy.resolve}
        />,
      );
      const btn = container.querySelector("button.md-doclink");
      expect(btn).not.toBeNull();
      expect(btn?.textContent).toBe("為什麼");
      // No href exists to redirect to — the destination is an ACTION.
      expect(btn?.getAttribute("href")).toBeNull();
      expect(spy.asked).toEqual(["docs/guide/why.md"]);
      fireEvent.click(btn!);
      expect(spy.clicked).toEqual(["docs/guide/why.md"]);
    });

    it("ON: a resolver that declines (unknown slug) keeps the literal fallback", () => {
      const spy = spyResolver(false);
      const { container } = render(
        <Markdown
          source="[開發文件](docs/dev/agent-env.md)"
          resolveDocLink={spy.resolve}
        />,
      );
      expect(container.querySelector("button")).toBeNull();
      expect(container.textContent).toContain("[開發文件](docs/dev/agent-env.md)");
      // It WAS eligible — the resolver is what said no.
      expect(spy.asked).toEqual(["docs/dev/agent-env.md"]);
    });

    // The security core. Each of these is a shape that would become clickable
    // if the new branch were written as "no scheme ⇒ relative path". They must
    // stay literal AND never be offered to the resolver.
    it.each([
      ["javascript:", "[click me](javascript:alert(1))", "javascript:alert(1)"],
      ["data:", "[x](data:text/html,<script>alert(1)</script>)", "data:text/html,"],
      ["protocol-relative", "[x](//evil.com/doc.md)", "//evil.com/doc.md"],
      ["site-absolute", "[x](/etc/passwd.md)", "/etc/passwd.md"],
      ["vbscript:", "[x](vbscript:msgbox)", "vbscript:msgbox"],
    ])(
      "ON: %s targets stay literal text and never reach the resolver",
      (_name, source, literal) => {
        const spy = spyResolver();
        const { container } = render(
          <Markdown source={source} resolveDocLink={spy.resolve} />,
        );
        expect(container.querySelector("a")).toBeNull();
        expect(container.querySelector("button")).toBeNull();
        expect(container.textContent).toContain(literal);
        expect(spy.asked).toEqual([]);
      },
    );

    it("ON: http(s) links keep the external anchor and bypass the resolver", () => {
      const spy = spyResolver();
      const { container } = render(
        <Markdown
          source="[releases](https://github.com/pkyosx/OffiCraft/releases)"
          resolveDocLink={spy.resolve}
        />,
      );
      const a = container.querySelector("a");
      expect(a?.getAttribute("href")).toBe(
        "https://github.com/pkyosx/OffiCraft/releases",
      );
      expect(a?.getAttribute("target")).toBe("_blank");
      expect(a?.getAttribute("rel")).toBe("noopener noreferrer");
      expect(spy.asked).toEqual([]);
    });

    // renderInline is reached from six different block paths; a resolver that
    // is threaded into only the paragraph path would look fine on the first
    // test above and silently drop every link inside a list or a table.
    it("ON: resolves inside headings, lists, tables and blockquotes too", () => {
      const spy = spyResolver();
      const { container } = render(
        <Markdown
          source={[
            "## 看 [標題連結](interface.md)",
            "",
            "- 清單 [列表連結](tasks.md)",
            "",
            "| a | b |",
            "| --- | --- |",
            "| [表格連結](mobile.md) | x |",
            "",
            "> 引言 [引言連結](install.md)",
          ].join("\n")}
          resolveDocLink={spy.resolve}
        />,
      );
      expect(spy.asked).toEqual([
        "interface.md",
        "tasks.md",
        "mobile.md",
        "install.md",
      ]);
      expect(container.querySelectorAll("h2 button.md-doclink").length).toBe(1);
      expect(container.querySelectorAll("li button.md-doclink").length).toBe(1);
      expect(container.querySelectorAll("td button.md-doclink").length).toBe(1);
      expect(
        container.querySelectorAll("blockquote button.md-doclink").length,
      ).toBe(1);
    });
  });

  // ── T-68f1 · GitHub alert blockquotes ────────────────────────────────────
  // `> [!NOTE]` is used by docs/guide/{install,mobile}.md. Before the fix the
  // marker printed verbatim as "[!NOTE]" at the head of the quote.
  describe("GitHub alert blockquotes (T-68f1)", () => {
    it("consumes the [!TYPE] marker and keeps the severity as a class", () => {
      const c = renderMd("> [!WARNING]\n> 成員有很大的行動自由。");
      const q = c.querySelector("blockquote");
      expect(q?.textContent).toBe("成員有很大的行動自由。");
      expect(c.textContent).not.toContain("[!WARNING]");
      expect(q?.className).toBe("md-alert md-alert--warning");
    });

    it("leaves an ordinary blockquote untouched (no class, no stripping)", () => {
      const c = renderMd("> 一般引言\n> 第二行");
      const q = c.querySelector("blockquote");
      expect(q?.textContent).toBe("一般引言 第二行");
      expect(q?.getAttribute("class")).toBeNull();
    });

    it("does not eat a bracketed line that is not an alert marker", () => {
      const c = renderMd("> [!NOPE] 這不是 alert");
      expect(c.querySelector("blockquote")?.textContent).toBe(
        "[!NOPE] 這不是 alert",
      );
      expect(c.querySelector("blockquote")?.getAttribute("class")).toBeNull();
    });
  });
});
