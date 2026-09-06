// Markdown renderer — the minimal, XSS-safe subset used by seeds + owner task
// manuals. Regression focus: a numbered step whose sub-content is indented
// (sub-bullets / code) must stay ONE ordered list with continuous numbering,
// not collapse into many single-item lists each restarting at 1 (the bug Seth
// hit pasting a PR-review SOP: "全部都是 1. 開始").

import { describe, it, expect } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { Markdown } from "./Markdown";
import { DiffOpenerContext } from "../hooks/useDiffOpener";
import type { DiffParams } from "../lib/diffLink";

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

  // T-2bb4 — owner wrote a `####` and got four literal hash marks on screen.
  // The renderer only ever knew #{1,3}, and "syntax I do not understand falls
  // through as plain text" is its safety rule, so the failure was SILENT: no
  // warning, no console line, nothing anywhere saying the fourth level does not
  // exist — and no page of the manual said so either.
  //
  // The boundary case is the one with teeth. `#######` (seven) must STILL fall
  // through as text: it is what proves the fix widened the ladder rather than
  // making "any run of hashes" a heading.
  it("renders all six heading levels, and a seventh hash is not a heading", () => {
    const c = renderMd(
      "# one\n## two\n### three\n#### four\n##### five\n###### six",
    );
    expect(c.querySelector("h1")?.textContent).toBe("one");
    expect(c.querySelector("h2")?.textContent).toBe("two");
    expect(c.querySelector("h3")?.textContent).toBe("three");
    expect(c.querySelector("h4")?.textContent).toBe("four");
    expect(c.querySelector("h5")?.textContent).toBe("five");
    expect(c.querySelector("h6")?.textContent).toBe("six");
    const seven = renderMd("####### seven");
    expect(seven.querySelector("h6")).toBeNull();
    expect(seven.textContent).toContain("####### seven");
  });

  // A #### that renders as an <h3> would look like a working heading while
  // quietly collapsing two levels into one — the option this ticket rejected.
  // Asserting the TAG (not just "some heading appeared") is what catches it.
  it("does not collapse #### onto ###", () => {
    const c = renderMd("### three\n\n#### four");
    expect(c.querySelectorAll("h3").length).toBe(1);
    expect(c.querySelectorAll("h4").length).toBe(1);
    expect(c.querySelector("h4")?.textContent).toBe("four");
  });

  // These three are what the change actually ALTERED, and the first draft of
  // this file tested only the places that stayed the same — the control group
  // was pinned to the untouched side. `###` already behaved this way in all
  // three positions (a list item's indented continuation is re-rendered, a
  // blockquote is re-rendered after its "> " is stripped, and a block opener
  // ends the paragraph above it), so widening the ladder extends that same
  // existing rule to levels 4-6. It is still an output change, and the manual's
  // own prose uses "> 🔴 …" quote blocks heavily, so somebody WILL write a
  // #### inside one.
  it("treats #### as a heading in the same places ### already was", () => {
    const quoted = renderMd("> #### in a quote");
    expect(quoted.querySelector("blockquote h4")?.textContent).toBe("in a quote");

    const nested = renderMd("- item\n    #### in a list item");
    expect(nested.querySelector("li h4")?.textContent).toBe("in a list item");

    const afterProse = renderMd("hello\n#### right after prose");
    expect(afterProse.querySelector("p")?.textContent).toBe("hello");
    expect(afterProse.querySelector("h4")?.textContent).toBe("right after prose");
  });

  // The regex reads the RAW line, so an indented "####" is not a heading. That
  // was true before this ticket and is unchanged by it — but nothing asserted
  // it, and a mutation that trimmed the line first left the whole file green.
  it("does not treat an indented #### as a heading", () => {
    const c = renderMd("  #### indented");
    expect(c.querySelector("h4")).toBeNull();
    expect(c.textContent).toContain("#### indented");
  });

  // The hashes must still be inert wherever they are not a block opener.
  it("leaves #### alone inside a code fence and mid-paragraph", () => {
    const fenced = renderMd("```\n#### not a heading\n```");
    expect(fenced.querySelector("h4")).toBeNull();
    expect(fenced.querySelector("code")?.textContent).toContain("#### not a heading");
    const inline = renderMd("see #### in this sentence");
    expect(inline.querySelector("h4")).toBeNull();
  });

  // Both found by the first real-page render of the embedded docs.
  //
  // `---` printed as three literal dashes: install.md alone has 5 of them and
  // the DOM had 0 <hr>. The renderer simply had no thematic-break rule.
  it("renders a `---` thematic break as an <hr>, not literal dashes", () => {
    const c = renderMd("上一節\n\n---\n\n下一節");
    expect(c.querySelectorAll("hr").length).toBe(1);
    expect(c.textContent).not.toContain("---");
    expect(c.querySelectorAll("p").length).toBe(2);
  });

  it("does not mistake a table delimiter row or fenced dashes for an <hr>", () => {
    const table = renderMd("| a | b |\n| --- | --- |\n| 1 | 2 |");
    expect(table.querySelectorAll("hr").length).toBe(0);
    expect(table.querySelectorAll("table").length).toBe(1);
    const fenced = renderMd("```\n---\n```");
    expect(fenced.querySelectorAll("hr").length).toBe(0);
    expect(fenced.querySelector("pre code")?.textContent).toBe("---");
  });

  // Inline `code` inside **bold** printed its backticks raw, while the SAME
  // token outside bold on the same page became a proper chip — a visible
  // same-page inconsistency, not a stylistic choice.
  it("parses inline code and links INSIDE a bold run", () => {
    const c = renderMd("**解析不到 `claude` 時會拒絕安裝**");
    const strong = c.querySelector("strong")!;
    expect(strong.querySelector("code")?.textContent).toBe("claude");
    expect(c.textContent).not.toContain("`");
    const linked = renderMd("**看 [首頁](https://example.com/)**");
    expect(linked.querySelector("strong a")?.getAttribute("href")).toBe(
      "https://example.com/",
    );
  });

  it("still lets a code span win over bold markers inside it", () => {
    const c = renderMd("`**not bold**` here");
    expect(c.querySelector("code")?.textContent).toBe("**not bold**");
    expect(c.querySelector("strong")).toBeNull();
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

  // Angle-bracketed destinations — `[text](<url>)`, CommonMark's way of writing
  // a destination that may carry spaces. It used to render as literal text
  // because the brackets were judged as part of the URL.
  describe("angle-bracketed link destination", () => {
    it("renders [text](<url>) as the same anchor as [text](url)", () => {
      const a = renderMd("see [the docs](<https://example.com/docs>)").querySelector("a");
      expect(a?.getAttribute("href")).toBe("https://example.com/docs");
      expect(a?.textContent).toBe("the docs");
      expect(a?.getAttribute("target")).toBe("_blank");
      expect(a?.getAttribute("rel")).toBe("noopener noreferrer");
    });

    it("renders an angle-bracketed mailto: destination as an anchor", () => {
      const c = renderMd("[contact](<mailto:owner@example.com>)");
      expect(c.querySelector("a")?.getAttribute("href")).toBe(
        "mailto:owner@example.com",
      );
    });

    it("autolinks nothing and keeps the anchor when the link sits inside prose and bold", () => {
      const a = renderMd("**看 [首頁](<https://example.com/>)**").querySelector("strong a");
      expect(a?.getAttribute("href")).toBe("https://example.com/");
    });

    // The scheme allowlist runs on what the brackets HOLD, not on the written
    // form: unwrapping must not become a way to smuggle a scheme past it.
    // Payloads deliberately carry no ")" — one that does ends the link token
    // early and is literal text in every version of this renderer, so it would
    // pass whether or not the allowlist still ran.
    it.each([
      "[x](<javascript:alert1>)",
      "[x](<data:text/html,hi>)",
      "[x](<vbscript:msgbox>)",
      "[x](<//evil.com/x>)",
    ])("falls back to literal text for an unsafe angle-bracketed destination: %s", (src) => {
      const c = renderMd(src);
      expect(c.querySelector("a")).toBeNull();
      expect(c.textContent).toContain(src);
    });

    // Pinned as-is, not chosen: a lone trailing ">" is not a bracket pair, so
    // it stays part of the destination exactly as it did before. Changing it
    // would change an existing [text](url) rendering, which this work does not.
    it("leaves a single unpaired '>' in the destination untouched", () => {
      const a = renderMd("[x](https://example.com/a>)").querySelector("a");
      expect(a?.getAttribute("href")).toBe("https://example.com/a>");
    });

    it("leaves an empty bracket pair as literal text", () => {
      const c = renderMd("[x](<>)");
      expect(c.querySelector("a")).toBeNull();
      expect(c.textContent).toContain("[x](<>)");
    });

    // The ONE behaviour change unwrapping causes outside the link cases above,
    // measured on both sides (main 85c2dd6a vs this branch) rather than argued:
    // `![alt](<url>)` used to be literal text and is now rendered the same way
    // `![alt](url)` ALREADY was on main — a "!" followed by an ordinary link.
    // Pinned because the two are only consistent by accident of sharing one
    // code path: narrow the unwrap to destinations not preceded by "!" and
    // every positive case above stays green while this one silently drops back
    // to literal text. What is NOT pinned here, because this work does not
    // change it: this renderer produces no <img> for either form.
    it("renders an angle-bracketed image the same way it already rendered a plain one", () => {
      const c = renderMd("![alt](<https://example.com/i.png>)");
      const a = c.querySelector("a");
      expect(a?.getAttribute("href")).toBe("https://example.com/i.png");
      expect(a?.textContent).toBe("alt");
      expect(c.textContent).toBe("!alt");
      expect(c.querySelector("img")).toBeNull();
    });

    // SECURITY PROPERTY, not an edge case: what the brackets hand over must not
    // itself contain "<" or ">". The unwrapping pattern says so with a negated
    // character class, and until this test that claim was unenforced — widening
    // it to `.*` left all other cases green, so the comment beside it was a
    // promise nothing kept. The payload below is the shape that matters: the
    // OUTER pair is well-formed, so a greedy pattern unwraps it happily and
    // yields a destination with a stray ">" inside, which then reaches the
    // scheme allowlist as a URL nobody wrote.
    it("refuses to unwrap a destination that itself contains an angle bracket", () => {
      const c = renderMd("[x](<https://example.com/a>b>)");
      expect(c.querySelector("a")).toBeNull();
      expect(c.textContent).toContain("[x](<https://example.com/a>b>)");
    });

    // A CAPABILITY THIS BRANCH ADDS, pinned separately because it is a
    // different kind of claim from the one above: whitespace around the
    // bracketed destination is tolerated, so a hand-typed link does not fail
    // for a reason the writer cannot see. It rests on a single `.trim()`, and
    // removing that token left every other case in this file green.
    it("tolerates whitespace around an angle-bracketed destination", () => {
      const a = renderMd("[x](  <https://example.com/a>  )").querySelector("a");
      expect(a?.getAttribute("href")).toBe("https://example.com/a");
      expect(a?.textContent).toBe("x");
    });
  });

  // T-59 — the compare url is a THIRD link class, and its whole promise is
  // that it does not stop being an ordinary link. Interception is the studio's
  // (see DiffModalHost.test.tsx); what is pinned HERE is that the renderer
  // hands out a real anchor either way, so copy-link, middle-click and
  // open-in-new-tab keep working, and that nothing is intercepted where there
  // is no studio to intercept it.
  describe("compare urls (T-59)", () => {
    const href = `${window.location.origin}/diff?before=att-0123456789ab&after=att-fedcba987654`;

    it("stays a real anchor with the same href, target and rel as any other link", () => {
      const c = renderMd(`[比較](${href})`);
      const a = c.querySelector("a");
      expect(a?.getAttribute("href")).toBe(href);
      expect(a?.getAttribute("target")).toBe("_blank");
      expect(a?.getAttribute("rel")).toBe("noopener noreferrer");
    });

    it("intercepts nothing outside the studio: no provider, no click handler", () => {
      const c = renderMd(`[比較](${href})`);
      // Marked only where the click IS swallowed — the standalone compare page
      // renders markdown too, and a compare link there must navigate.
      expect(c.querySelector("a")?.hasAttribute("data-diff-link")).toBe(false);
    });

    // The two features below (compare urls, bare-URL autolinking) were built on
    // separate branches and only meet here. Pasting a BARE compare url is how a
    // comparison actually travels, so the autolinked form has to reach the same
    // interception the written [text](url) form does — and the second case is
    // the negative control proving that reach was not widened.
    it("intercepts a BARE compare url too, not only the [text](url) form", () => {
      const opened: DiffParams[] = [];
      const { container } = render(
        <DiffOpenerContext.Provider value={(p) => opened.push(p)}>
          <Markdown source={`看這個 ${href} 就知道`} />
        </DiffOpenerContext.Provider>
      );
      const a = container.querySelector("a");
      expect(a?.getAttribute("href")).toBe(href);
      expect(a?.hasAttribute("data-diff-link")).toBe(true);
      fireEvent.click(a!);
      expect(opened.length).toBe(1);
      expect(opened[0].before).toBe("att-0123456789ab");
      expect(opened[0].after).toBe("att-fedcba987654");
    });

    it("leaves a bare ORDINARY url alone inside the studio: still navigates", () => {
      const opened: DiffParams[] = [];
      const { container } = render(
        <DiffOpenerContext.Provider value={(p) => opened.push(p)}>
          <Markdown source="see https://example.com/diff?before=x for detail" />
        </DiffOpenerContext.Provider>
      );
      const a = container.querySelector("a");
      expect(a?.getAttribute("href")).toBe("https://example.com/diff?before=x");
      expect(a?.hasAttribute("data-diff-link")).toBe(false);
      fireEvent.click(a!);
      expect(opened).toEqual([]);
    });
  });

  // T-59 — bare-URL autolinking. Owner ruling: a pasted URL is the most common
  // reference in a chat message and it rendered as dead text; turn it on
  // everywhere, no flag, no per-surface opt-in.
  //
  // "I typed a URL and got a link" is the shallow half and not where this
  // breaks. It breaks on (a) the pre-existing inline syntaxes and (b) where the
  // URL STOPS. So the material below is not invented: every tail case is a real
  // string scanned out of chat messages / task descriptions / reply cards on
  // the live system (2026-09-04, 4,829 URLs), and CORPUS_TAILS is that scan
  // turned into a table.
  describe("bare-URL autolinking (T-59)", () => {
    it("renders a bare http/https URL as an anchor with hardened target/rel", () => {
      const c = renderMd("see https://example.com/docs for detail");
      const a = c.querySelector("a");
      expect(a?.getAttribute("href")).toBe("https://example.com/docs");
      expect(a?.textContent).toBe("https://example.com/docs");
      expect(a?.getAttribute("target")).toBe("_blank");
      expect(a?.getAttribute("rel")).toBe("noopener noreferrer");
      expect(c.textContent).toBe("see https://example.com/docs for detail");
    });

    it("links every URL when one line carries several", () => {
      const c = renderMd("a https://one.example/x b http://two.example/y c");
      const hrefs = [...c.querySelectorAll("a")].map((a) => a.getAttribute("href"));
      expect(hrefs).toEqual(["https://one.example/x", "http://two.example/y"]);
      expect(c.textContent).toBe("a https://one.example/x b http://two.example/y c");
    });

    // ── Where the URL stops: the live-data table ───────────────────────────
    // [name, source line as it appears on the system, the ONE href it must
    // produce]. Real strings, not hand-written ones: the two biggest tail
    // groups on the system (a wrapping shell quote, and pasted JSON) were both
    // missing from the first hand-written list of trailing characters, and a
    // single-character trim cannot handle `",` or `"}]}` at all.
    const CORPUS_TAILS: [string, string, string][] = [
      [
        "no tail",
        "端 console 入口是 👉 https://claude.ai/code (需用你的 Anthropic 帳號)",
        "https://claude.ai/code",
      ],
      [
        "wrapping shell quote '",
        "curl 'https://gf-external-api.hardcoretech.link/api/v1/task-assignments?page=1' --insecure",
        "https://gf-external-api.hardcoretech.link/api/v1/task-assignments?page=1",
      ],
      [
        "pasted JSON \",",
        '{ "url": "https://github.com/example/repo/pull/1", "title": "x" }',
        "https://github.com/example/repo/pull/1",
      ],
      [
        "closing JSON string \"",
        'dedupe_key: "https://github.com/hardcoretech/gf-external-api/pull/849" t-8cf58dc',
        "https://github.com/hardcoretech/gf-external-api/pull/849",
      ],
      [
        "fullwidth paren ）",
        "（https://github.com/hardcoretech/fms/pull/20187） - 端點完整做完",
        "https://github.com/hardcoretech/fms/pull/20187",
      ],
      [
        "ascii paren )",
        "(必填):PR 連結(例如 https://github.com/hardcoretech/xxx/pull/123) - ",
        "https://github.com/hardcoretech/xxx/pull/123",
      ],
      [
        "fullwidth period 。",
        "updater = https://open-company-updater.hardcoretech.link/。 【已完成】A1",
        "https://open-company-updater.hardcoretech.link/",
      ],
      [
        "ascii paren then fullwidth period )。",
        "l.py 樣板(OC_BASE=http://127.0.0.1:8770)。",
        "http://127.0.0.1:8770",
      ],
      [
        "fullwidth parenthetical then period ）。",
        "PR 開好了：#1247 → https://github.com/pkyosx/open-company/pull/1247（已釘到任務卡）。 PR body",
        "https://github.com/pkyosx/open-company/pull/1247",
      ],
      [
        "JSON close \"}]}",
        'oints":[{"url":"https://a.nel.cloudflare.com/report/v4?s=PIrAAtCQjB7"}]}',
        "https://a.nel.cloudflare.com/report/v4?s=PIrAAtCQjB7",
      ],
      [
        "corner bracket 」",
        "Seth 說:「幫我review https://github.com/hardcoretech/gf-external-api/pull/849」 PR link:",
        "https://github.com/hardcoretech/gf-external-api/pull/849",
      ],
      [
        "ascii comma ,",
        "with eva's name https://github.com/hardcoretech/svc-spider-man/pull/159, can you check",
        "https://github.com/hardcoretech/svc-spider-man/pull/159",
      ],
      [
        "fullwidth question ？ after a CJK particle",
        "你有收到https://gofreight.slack.com/archives/D0718RYH3KJ/p1784624784590119嗎？",
        // 嗎 is an ordinary CJK character, not punctuation, and CJK is legal in
        // a URL path (zh.wikipedia links). The scan's answer keeps it too.
        "https://gofreight.slack.com/archives/D0718RYH3KJ/p1784624784590119嗎",
      ],
      [
        "JSON object close \"}",
        '輸出結果如下： {"url":"https://api.github.com/repos/hardcoretech/data-pensieve/pulls/comments/3819001572","pull_request_review_id":123}',
        "https://api.github.com/repos/hardcoretech/data-pensieve/pulls/comments/3819001572",
      ],
      [
        "fullwidth paren then colon ）：",
        "，PR https://github.com/pkyosx/OffiCraft/pull/320）： - CI 抓到一個",
        "https://github.com/pkyosx/OffiCraft/pull/320",
      ],
      [
        "code paste \");",
        'sers.getForUrl("https://officraft.hardcoretech.link/"); nodeRep',
        "https://officraft.hardcoretech.link/",
      ],
      [
        "corner bracket then period 」。",
        "create a task for https://hardcoretech.atlassian.net/browse/ACE-10268」。 ## 票面",
        "https://hardcoretech.atlassian.net/browse/ACE-10268",
      ],
      [
        "JSON close inside a shell quote \"}'",
        'when I visit http://seths-macbook-pro.local:7755/#office/chat/m-f663f3c5de9a"}\' --insecure',
        "http://seths-macbook-pro.local:7755/#office/chat/m-f663f3c5de9a",
      ],
      [
        "ellipsis ...",
        "Claude-Session: https://claude.ai/code/session_01Efb...",
        "https://claude.ai/code/session_01Efb",
      ],
      [
        "code paste then fullwidth period \")。",
        '行同一段 getForUrl("https://officraft.hardcoretech.link/")。 3. 回傳原文仍是',
        "https://officraft.hardcoretech.link/",
      ],
      [
        "shell quote then comma ',",
        "GoNEXUS Hub 'https://go-nexus-hub.core.gofreight.co', ]",
        "https://go-nexus-hub.core.gofreight.co",
      ],
      [
        "fullwidth colon ：",
        "見 https://github.com/pkyosx/OffiCraft/pull/388：",
        "https://github.com/pkyosx/OffiCraft/pull/388",
      ],
      [
        "trailing colon, port colon kept",
        "把 ingress 全部改成 http://127.0.0.1: 就好",
        "http://127.0.0.1",
      ],
    ];

    it.each(CORPUS_TAILS)(
      "stops the link at the right place — %s",
      (_name, source, href) => {
        const c = renderMd(source);
        const anchors = [...c.querySelectorAll("a")];
        expect(anchors.length).toBe(1);
        expect(anchors[0].getAttribute("href")).toBe(href);
        // The link's own text is the href — nothing was hidden or duplicated —
        // and the visible line still reads exactly as it was written.
        expect(anchors[0].textContent).toBe(href);
        expect(c.textContent).toBe(source);
      },
    );

    // The confrontation of two numbers that must agree. Rendering the whole
    // corpus as ONE document must produce exactly as many anchors as there are
    // rows — no row silently producing zero links (a swallowed URL) or two (a
    // split one). Checking each row alone cannot catch a row that yields two,
    // because a per-row count of 1 was what the row was asked for; this is the
    // cross-check that the earlier scan's own miscount showed is needed.
    it("produces exactly one link per corpus row when the whole corpus renders as one document", () => {
      const c = renderMd(CORPUS_TAILS.map(([, src]) => src).join("\n\n"));
      const hrefs = [...c.querySelectorAll("a")].map((a) => a.getAttribute("href"));
      expect(hrefs.length).toBe(CORPUS_TAILS.length);
      expect(hrefs).toEqual(CORPUS_TAILS.map(([, , href]) => href));
    });

    // ── Do not over-strip ─────────────────────────────────────────────────
    it("keeps a closing parenthesis the URL itself opened", () => {
      const c = renderMd("https://en.wikipedia.org/wiki/Foo_(bar)");
      expect(c.querySelector("a")?.getAttribute("href")).toBe(
        "https://en.wikipedia.org/wiki/Foo_(bar)",
      );
    });

    it("strips only the unmatched paren when a bracketed URL is itself wrapped in parens", () => {
      const c = renderMd("(https://en.wikipedia.org/wiki/Foo_(bar))");
      expect(c.querySelector("a")?.getAttribute("href")).toBe(
        "https://en.wikipedia.org/wiki/Foo_(bar)",
      );
      expect(c.textContent).toBe("(https://en.wikipedia.org/wiki/Foo_(bar))");
    });

    // The owner's verbatim case.
    it("does not render a wrapping closing parenthesis into the link", () => {
      const c = renderMd("(http://test.com)");
      expect(c.querySelector("a")?.getAttribute("href")).toBe("http://test.com");
      expect(c.textContent).toBe("(http://test.com)");
    });

    it("links a URL that ends the message with no trailing character at all", () => {
      const c = renderMd("上線了 http://test.com");
      expect(c.querySelector("a")?.getAttribute("href")).toBe("http://test.com");
    });

    // ── Nothing that already worked may change ────────────────────────────
    it("leaves a URL inside a code span as code, not a link", () => {
      const c = renderMd("call `https://example.com/api` from the shell");
      expect(c.querySelector("a")).toBeNull();
      expect(c.querySelector("code")?.textContent).toBe("https://example.com/api");
    });

    it("leaves a URL inside a fenced code block as code, not a link", () => {
      const c = renderMd("```\ncurl https://example.com/api\n```");
      expect(c.querySelector("a")).toBeNull();
      expect(c.querySelector("pre code")?.textContent).toContain(
        "https://example.com/api",
      );
    });

    it("leaves an already-written [text](url) link as ONE anchor labelled by its text", () => {
      const c = renderMd("see [the docs](https://example.com/docs) now");
      const anchors = [...c.querySelectorAll("a")];
      expect(anchors.length).toBe(1);
      expect(anchors[0].getAttribute("href")).toBe("https://example.com/docs");
      expect(anchors[0].textContent).toBe("the docs");
      expect(c.textContent).toBe("see the docs now");
    });

    it("keeps the repo-relative .md doc-link class a button on the surface that enables it", () => {
      const asked: string[] = [];
      const { container } = render(
        <Markdown
          source="[為什麼](docs/guide/why.md)"
          resolveDocLink={(t) => {
            asked.push(t);
            return () => {};
          }}
        />,
      );
      expect(container.querySelector("button.md-doclink")).not.toBeNull();
      expect(container.querySelector("a")).toBeNull();
      expect(asked).toEqual(["docs/guide/why.md"]);
    });

    // Expected, not a regression: renderInline re-parses a bold run's inside,
    // so a bold-wrapped URL is autolinked like any other. A large minority of
    // the URLs on the system are written this way.
    it("autolinks a bare URL inside a bold run", () => {
      const c = renderMd("**https://example.com/x**");
      const a = c.querySelector("strong a");
      expect(a?.getAttribute("href")).toBe("https://example.com/x");
    });

    // ── The safety allowlist did not loosen ───────────────────────────────
    it.each([
      ["javascript:", "javascript:alert(1)"],
      ["data:", "data:text/html,hello"],
      ["protocol-relative", "//evil.com/x"],
      ["vbscript:", "vbscript:msgbox"],
      ["scheme-less host", "evil.com/x"],
      ["file:", "file:///etc/passwd"],
    ])("leaves a bare %s reference inert plain text", (_name, target) => {
      const c = renderMd(`click ${target} now`);
      expect(c.querySelector("a")).toBeNull();
      expect(c.textContent).toBe(`click ${target} now`);
    });

    it("does not make a link out of a scheme with no host", () => {
      const c = renderMd("http:// and https://.");
      expect(c.querySelector("a")).toBeNull();
    });
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
      // The one that pins the DESIGN's headline argument. Every case above is
      // rejected for an INCIDENTAL reason — "(" / "," / "<" are outside the
      // character class, "//" and "/" fail the first-segment rule, "vbscript:
      // msgbox" has no ".md" tail — so all five stay red-free even if ":" were
      // added to DOC_REL_PATH_RE's character class. This one is rejected for
      // the reason the doc comment actually claims: the class has NO ":", so a
      // scheme cannot spell itself. Without it, a one-character widening of
      // that class hands `javascript:x.md` to the resolver with the whole
      // suite still green (reviewer-verified, review3 §2.4).
      ["scheme with a .md tail", "[x](javascript:x.md)", "javascript:x.md"],
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

    // The defect the FIRST real-page render caught (docs/guide/install.md's
    // `> [!WARNING]`): the quote body was rendered with renderInline, so any
    // block structure inside it was flattened onto one inline run — the fence
    // markers printed as literal backticks, "bash" became part of the command,
    // and the prose after the fence was swallowed into the code run. Every
    // assertion here is on STRUCTURE (a real <pre><code>, the prose OUTSIDE
    // it), which is exactly what an inline-only quote cannot produce.
    it("renders a fenced code block INSIDE a blockquote as real <pre><code>", () => {
      const c = renderMd(
        [
          "> [!WARNING]",
          "> 要接受重啟請明確加上:",
          ">",
          "> ```bash",
          "> curl -fsSL https://example.com/i.sh | bash -s -- --force",
          "> ```",
          ">",
          "> **不想動到現役服務的話,可以用不同 label 併裝:**",
        ].join("\n"),
      );
      const q = c.querySelector("blockquote")!;
      const pre = q.querySelector("pre code");
      expect(pre).not.toBeNull();
      // The fence markers and the language tag are CONSUMED, not printed.
      expect(q.textContent).not.toContain("```");
      expect(pre!.textContent).not.toContain("bash\n");
      expect(pre!.textContent).toBe(
        "curl -fsSL https://example.com/i.sh | bash -s -- --force",
      );
      // The prose after the fence is its own block, OUTSIDE the code element.
      expect(pre!.textContent).not.toContain("不想動到現役服務");
      expect(q.querySelector("strong")?.textContent).toContain(
        "不想動到現役服務",
      );
      // And it is still an alert.
      expect(q.className).toBe("md-alert md-alert--warning");
    });

    it("renders a list inside a blockquote as a real list", () => {
      const c = renderMd("> [!TIP]\n> 兩種做法:\n>\n> - 第一種\n> - 第二種");
      const q = c.querySelector("blockquote")!;
      expect(q.querySelectorAll("ul li").length).toBe(2);
      expect(q.textContent).not.toContain("- 第一種");
    });

    // T-68f1 review3 RC1 — pathological nesting must DEGRADE, never crash.
    //
    // Making a blockquote's content go through renderBlocks (the fix above)
    // turned a non-recursive path into a recursive one, and the depth is
    // chosen by the SOURCE TEXT. 17 of the 18 product call sites render
    // agent-authored text, so a single message of `"> ".repeat(2000)` used to
    // recurse ~2000 levels and make the React reconciler throw
    // `RangeError: Maximum call stack size exceeded` — the entire tree fails
    // to render, not just the quote. These two pin the ceiling: the render
    // must complete, and the nesting must actually be bounded (a "no throw"
    // assertion alone would also pass if someone raised the cap to 100000 on
    // a machine with a bigger stack).
    it("does not crash on pathologically deep blockquote nesting", () => {
      const deep = "> ".repeat(2000) + "deep";
      let c: HTMLElement | null = null;
      expect(() => {
        c = renderMd(deep);
      }).not.toThrow();
      const quotes = c!.querySelectorAll("blockquote").length;
      expect(quotes).toBeGreaterThan(0);
      expect(quotes).toBeLessThanOrEqual(16);
      // The text is not lost — past the ceiling the remainder is emitted as
      // literal text rather than being parsed further.
      expect(c!.textContent).toContain("deep");
    });

    it("does not crash on pathologically deep nested-list nesting", () => {
      const deep = Array.from(
        { length: 400 },
        (_, n) => " ".repeat(n * 2) + "- item",
      ).join("\n");
      let c: HTMLElement | null = null;
      expect(() => {
        c = renderMd(deep);
      }).not.toThrow();
      expect(c!.querySelectorAll("ul").length).toBeLessThanOrEqual(16);
      expect(c!.textContent).toContain("item");
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
