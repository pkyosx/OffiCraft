// T-84c8 — a chat message body (ChatMessage.body) is the purest owner/agent
// free text in the app, and via webhooks it can carry text from an EXTERNAL
// system. It must render through the shared, XSS-safe `Markdown` component
// (same posture as the reply-card body, which already renders THIS field's
// fallback as markdown — ChatReplyCard.tsx).
//
// Fixture input space covered here (the shapes a real thread actually holds):
// short plain sentence · long multi-block message · fenced code block · bold ·
// `code` · lists · plain text carrying NO markdown at all · own (owner) vs
// peer message · message carrying attachments alongside its body.
//
// SCOPE HONESTY — what this file can and cannot pin:
//   CAN  (jsdom sees DOM structure): whether the markdown was PARSED, whether
//        raw HTML is injected, whether newlines survive as <br>.
//   CANNOT (vite.config.ts: environment "jsdom" — no stylesheets, no layout):
//        anything purely visual. Whether a long fenced code block bursts the
//        bubble at 390px is a CSS question jsdom structurally cannot answer;
//        it is pinned by real Playwright measurement instead (see
//        kyle-84c8-impl.md). Do not read a green run here as "the layout is
//        fine".

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";

let messages: ChatMessage[] = [];
vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
    messagesPeer: "b",
    peerLastReadTs: 0,
    send: vi.fn(() => Promise.resolve()),
    markRead: vi.fn(() => Promise.resolve()),
  }),
}));

function mkMember(id = "b", name = "Beto"): Member {
  return {
    id,
    memberId: id,
    name,
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "assistant",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-b",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

function mkMsg(over: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: "m1",
    from: "b",
    to: "owner",
    body: "",
    ts: new Date(2026, 6, 13, 9, 0, 0, 0).getTime() / 1000,
    attachments: [],
    replyCardId: null,
    ...over,
  };
}

function renderChat() {
  return render(
    <I18nProvider>
      <ChatArea member={mkMember()} members={[mkMember()]} />
    </I18nProvider>,
  );
}

const NOW = new Date(2026, 6, 13, 10, 0, 0, 0);

beforeEach(() => {
  localStorage.clear();
  vi.useFakeTimers({ now: NOW, toFake: ["Date"] });
  Element.prototype.scrollIntoView = vi.fn();
  messages = [];
});

afterEach(() => {
  vi.useRealTimers();
});

/** The rendered markdown container for the first message body. */
function bodyEl(container: HTMLElement): HTMLElement {
  const el = container.querySelector<HTMLElement>(".chat__msg-text");
  if (!el) throw new Error("no .chat__msg-text rendered");
  return el;
}

describe("chat message body renders markdown (T-84c8)", () => {
  it("routes the body through the shared Markdown component, not raw text", () => {
    messages = [mkMsg({ body: "**部署步驟**" })];
    const { container } = renderChat();
    // POSITIVE CONTROL: the body element exists at all, and carries the
    // doc-md skin the other markdown surfaces use. If the render site were
    // deleted this throws instead of vacuously passing.
    const el = bodyEl(container);
    expect(el.classList.contains("doc-md")).toBe(true);
  });

  it("renders **bold** as a <strong> element (peer message)", () => {
    messages = [mkMsg({ body: "請先讀 **部署步驟** 再動手" })];
    const { container } = renderChat();
    const el = bodyEl(container);
    expect(el.querySelector("strong")?.textContent).toBe("部署步驟");
    // Paired with the positive above: the literal markers are gone BECAUSE
    // they became an element, not because the text vanished.
    expect(el.textContent).toContain("請先讀");
    expect(el.textContent).not.toContain("**部署步驟**");
  });

  it("renders `code` as a <code> element", () => {
    messages = [mkMsg({ body: "先拉最新的 `origin/main`" })];
    const { container } = renderChat();
    const el = bodyEl(container);
    expect(el.querySelector("code")?.textContent).toBe("origin/main");
    expect(el.textContent).not.toContain("`origin/main`");
  });

  it("renders a fenced code block as <pre><code> preserving its lines verbatim", () => {
    messages = [
      mkMsg({
        body: "跑這段：\n\n```bash\nexport A=1\nkubectl get pods\n```\n\n就好",
      }),
    ];
    const { container } = renderChat();
    const el = bodyEl(container);
    const pre = el.querySelector("pre");
    expect(pre).not.toBeNull();
    // The <pre> keeps BOTH lines and the newline between them — a code block
    // folded into one line would still have a <pre>, so pin the text.
    expect(pre?.querySelector("code")?.textContent).toBe(
      "export A=1\nkubectl get pods",
    );
    // The fence markers themselves must not survive as literal text.
    expect(el.textContent).not.toContain("```");
    // Surrounding prose still renders (the block did not swallow the message).
    expect(el.textContent).toContain("跑這段：");
    expect(el.textContent).toContain("就好");
  });

  it("renders a list as real <ul>/<li> elements", () => {
    messages = [mkMsg({ body: "檢查：\n\n- 先查表\n- 確認 runner\n- 再叫我" })];
    const { container } = renderChat();
    const el = bodyEl(container);
    const items = el.querySelectorAll("ul > li");
    expect(items.length).toBe(3);
    expect(items[0].textContent).toBe("先查表");
    expect(items[2].textContent).toBe("再叫我");
  });

  it("renders an ordered list preserving the source numbering", () => {
    messages = [mkMsg({ body: "1. 先拉\n2. 再確認\n3. 最後跑" })];
    const { container } = renderChat();
    const el = bodyEl(container);
    const items = el.querySelectorAll("ol > li");
    expect(items.length).toBe(3);
    expect(items[1].textContent).toBe("再確認");
  });

  // ── the fixture shape most likely to REGRESS ────────────────────────────
  // A chat bubble was `white-space: pre-wrap` BEFORE this change, so a plain
  // multi-line message (no markdown at all — by far the most common shape)
  // already rendered one line per line. Standard markdown folds single
  // newlines into spaces; shipping that would silently turn every such
  // message into a run-on line. `breaks` is what stops it, and this is the
  // test that would catch its removal.
  it("preserves single newlines in a plain, markdown-free message (no run-on line)", () => {
    messages = [
      mkMsg({ body: "先確認環境變數。\n不要直接在 prod 跑。\n有問題隨時問我。" }),
    ];
    const { container } = renderChat();
    const el = bodyEl(container);
    // Three source lines ⇒ two hard breaks inside one paragraph.
    expect(el.querySelectorAll("br").length).toBe(2);
    expect(el.querySelectorAll("p").length).toBe(1);
    // POSITIVE CONTROL: all three lines are actually present…
    expect(el.textContent).toContain("先確認環境變數。");
    expect(el.textContent).toContain("有問題隨時問我。");
    // …and were NOT welded together with a space (the exact markdown default
    // this guards against).
    expect(el.textContent).not.toContain("先確認環境變數。 不要直接在 prod 跑。");
  });

  it("a short plain sentence renders as a single paragraph with no stray markup", () => {
    messages = [mkMsg({ body: "好。" })];
    const { container } = renderChat();
    const el = bodyEl(container);
    expect(el.querySelectorAll("p").length).toBe(1);
    expect(el.textContent).toBe("好。");
    expect(el.querySelectorAll("br").length).toBe(0);
  });

  // ── own vs peer ────────────────────────────────────────────────────────
  it("renders markdown in the OWNER's own outgoing message too", () => {
    messages = [
      mkMsg({ from: "owner", to: "b", body: "我用 `--dry-run` 先試 **不碰 prod**" }),
    ];
    const { container } = renderChat();
    // POSITIVE CONTROL: this really is the own-message branch (the class the
    // accent bubble keys on), so the markdown assertions below are about a
    // "me" bubble and not a mislabelled peer one.
    expect(container.querySelector(".chat__msg--me")).not.toBeNull();
    const el = bodyEl(container);
    expect(el.querySelector("code")?.textContent).toBe("--dry-run");
    expect(el.querySelector("strong")?.textContent).toBe("不碰 prod");
  });

  // ── body + attachments ─────────────────────────────────────────────────
  it("renders markdown in a message that ALSO carries attachments", () => {
    messages = [
      mkMsg({
        body: "**看這張**",
        attachments: [
          {
            id: "att-1",
            url: "/api/chat/attachment/att-1",
            filename: "shot.png",
            mime: "image/png",
            isImage: true,
          },
        ],
      }),
    ];
    const { container } = renderChat();
    const el = bodyEl(container);
    expect(el.querySelector("strong")?.textContent).toBe("看這張");
    // POSITIVE CONTROL: the attachment strip still renders alongside the body
    // — markdown did not displace it.
    expect(container.querySelector(".chat__msg-attachments")).not.toBeNull();
  });

  // ── security ───────────────────────────────────────────────────────────
  // Chat bodies can originate from a webhook (an external, fully untrusted
  // system), so this is the highest-value guard in the file.
  it("never injects raw HTML from a malicious body (webhook-grade untrusted text)", () => {
    messages = [
      mkMsg({ body: "<script>alert(1)</script><img src=x onerror=alert(2)>" }),
    ];
    const { container } = renderChat();
    const el = bodyEl(container);
    expect(el.querySelector("script")).toBeNull();
    expect(el.querySelector("img")).toBeNull();
    // POSITIVE CONTROL: the payload was RENDERED (as inert text) rather than
    // dropped — a body that rendered nothing would satisfy the two nulls
    // above without proving anything.
    expect(el.textContent).toContain("<script>alert(1)</script>");
    expect(el.textContent).toContain("<img src=x onerror=alert(2)>");
  });

  it("does not turn a javascript: link into an anchor", () => {
    messages = [mkMsg({ body: "[click me](javascript:alert(1))" })];
    const { container } = renderChat();
    const el = bodyEl(container);
    expect(el.querySelector("a")).toBeNull();
    expect(el.textContent).toContain("[click me](javascript:alert(1))");
  });

  it("renders a safe http link as a hardened anchor", () => {
    messages = [mkMsg({ body: "see [docs](https://example.com/d)" })];
    const { container } = renderChat();
    const a = bodyEl(container).querySelector("a");
    expect(a?.getAttribute("href")).toBe("https://example.com/d");
    expect(a?.getAttribute("rel")).toBe("noopener noreferrer");
    expect(a?.getAttribute("target")).toBe("_blank");
  });
});
