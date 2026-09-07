// ResumeSummaryCard · PAYLOAD PARITY.
//
// Owner, verbatim: 「就是你怎麼給 agent 的就怎麼給我,格式應該要我們兩個雙方都
// 看得懂」 / 「我不要規則 我要看到到時候 agent 實際看到的 魔鬼藏在細節裡」.
//
// 🔴 WHY THE FIXTURE IS A **WIRE** PAYLOAD AND NOT A VIEW MODEL.
// The failure this file exists to catch was a SEAM failure: `mappers.ts`
// carried `roster` and `machines` no further than its own function body, so
// the payload had two sections the cockpit could not draw — and NOTHING was
// red, because the view model simply did not declare them and every component
// test was written against a hand-built view model that did not have them
// either. A test that starts from a view model can only ever prove the
// component draws what it was handed; it is structurally blind to a seam that
// hands it less than the server sent.
//
// So this file starts from `WIRE` — the same JSON an agent's `resume_summary`
// receives — pushes it through the REAL `toMemberResumeSummary`, and asserts
// against the screen. One fixture, both layers: drop a field in the mapper or
// forget to draw it in the component and the SAME assertion goes red.
//
// 🔴 AND WHY THERE IS A COVERAGE ASSERTION AT THE BOTTOM.
// Per-field assertions only cover the fields somebody remembered to write an
// assertion for — which is exactly the position we were already in. The
// coverage block instead asserts the SHAPE of the mapped snapshot against a
// declared roll-call, so a field added to the view model later cannot slip
// through unrendered and unnoticed: it goes red on arrival, naming itself.
// The roll-call is a hard-coded list on purpose — the only way past it is to
// edit it, and that edit is the review this file wants to force.
//
// ── 🔴 WHAT THIS FILE STILL DOES NOT GUARD (read before trusting a green run) ─
// Most assertions below are SUBSTRING assertions (`toContain`). T-9871 converted
// only the ones on the chat row it touched — parties, reply-card status,
// attachments, timestamps — into whole-string equality composed from the same
// wire row. THE REST WERE LEFT AS THEY WERE, deliberately: they guard note /
// roster / machines / tasks, which is a different deliverable, and rewriting
// them inside a quote-rendering change would have enlarged the diff and risked
// weakening cover nobody was reviewing. Enumerate the CALL SITES (not this
// prose) with
//   grep -nE '\)\.(not\.)?toContain\(' src/components/ResumeSummaryCard.payload-parity.test.tsx
// — 34 when this note was written, 4 of them negative. Do not trust that count,
// run the query: this comment names the matcher, so a plain `grep toContain`
// counts these lines too.
//
// (a) THE POSITIVE ONES — what stays green that should not.
//     `expect(txt(row)).toContain(value)` asks only whether the value appears
//     SOMEWHERE in that element's text. On a roster row, a machine id printed in
//     the duty slot, a presence word printed twice, or a fabricated extra field
//     appended to the row all satisfy it — the row's SHAPE is unasserted, only
//     its ingredients. The same holds for the machines rows, the answered-card
//     steps, the reply card's options / answer text / answered-at, the folded
//     count, the cut label, and the section empty-state labels. And because
//     several of these read a WHOLE row's text, a field that stopped rendering
//     entirely stays green whenever a neighbouring field happens to print the
//     same characters.
//
// (b) THE FOUR NEGATIVE ONES ARE NOT THE SAME SHAPE, AND MUST NOT BE
//     "CONVERTED". `not.toContain` is an ABSENCE claim, and whole-string
//     equality cannot express absence — rewriting it deletes the guard outright.
//     They are, by what each protects:
//       · the chat meta line must not print the raw epoch `ts` beside the
//         server's rendered stamp (scoped to the meta line on purpose — the
//         epoch legitimately appears inside the server's cursor hint);
//       · the rendered `note` and cut `hint` must not show literal `**`, i.e.
//         the markdown was rendered rather than dumped;
//       · the container's innerHTML must not contain `<script` — the inert-HTML
//         claim for agent- and outsider-authored content.
//     Whoever sweeps (a) must leave these four alone or replace them with
//     another assertion that is still about ABSENCE.
//
// The sweep of (a) is tracked as its own ticket by the coordinator (2026-08-23
// ruling: same file ≠ same deliverable). Until it lands, this file proves the
// values ARRIVED on screen; it does not prove they arrived in the right slots.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import { runeLength } from "../api/docCap";
import { toMemberResumeSummary } from "../api/mappers";
import type { MemberResumeSummaryView } from "../api/adapter";
import type { WireResumeSummary, WireResumeTask } from "../api/wire";
import { ResumeSummaryCard } from "./ResumeSummaryCard";

const ANSWERED_CARD_STEPS: NonNullable<WireResumeTask["answered_card_steps"]> = [
  {
    step_id: "step-answered-1",
    step_name: "先讀 owner 回覆",
    card_id: "rc-answered-1",
  },
  {
    step_id: "step-answered-2",
    step_name: "依回覆調整方案",
    card_id: "rc-answered-2",
  },
];

function answeredCardStepChars(
  steps: NonNullable<WireResumeTask["answered_card_steps"]>,
): number {
  return steps.reduce(
    (sum, step) =>
      sum +
      runeLength(step.step_id) +
      runeLength(step.step_name) +
      runeLength(step.card_id),
    0,
  );
}

// The snapshot as it comes off the wire. Values are deliberately DISTINCT and
// mutually non-substring, so an assertion cannot be satisfied by a neighbour's
// text — a field rendered into the wrong slot stays visible to the assertion,
// not just to the eye.
const WIRE: WireResumeSummary = {
  identity: "mira",
  generated_at: "2026-08-13 09:47:11 +08:00",
  note: "BOUNDED snapshot — recent chat + open tasks only.",
  overview: {
    chat_count: 501,
    chat_chars: 8237,
    tasks_returned: 613,
    tasks_open_total: 9042,
    tasks_detail_chars: 375,
    cards_waiting: 264,
    cards_answered_recent: 718,
    roster_chars: 4196,
    machines_chars: 1583,
    steps_on_answered_card: ANSWERED_CARD_STEPS.length,
    steps_on_answered_card_chars: answeredCardStepChars(ANSWERED_CARD_STEPS),
  },
  chat: [
    {
      id: "cm-1",
      from: "m-planner",
      from_name: "普朗克",
      to: "owner",
      to_name: "Seth",
      body: "已完成後端組裝,等你決定要不要照這個形狀往下做",
      ts: 1786000001,
      ts_display: "2026-08-12 22:13:04 +08:00",
      body_omitted_chars: 0,
      reply_card_status: "answered",
      reply_to: "",
      attachments: [],
      card: {
        // ai_pick sits on the SECOND option and BOTH options are circled —
        // deliberately. A first-option AI pick and a single-option answer are
        // exactly the two shapes a positional reader gets right by accident.
        options: [
          { text: "照這個形狀做", ai_pick: false },
          { text: "先擋著等下一輪", ai_pick: true },
        ],
        answer_option_idxs: [0, 1],
        answer_text: "先擋著,理由寫在卡上",
        answered_ts: 1786000600,
        answered_at_display: "2026-08-12 22:23:20 +08:00",
      },
    },
    {
      // The HONEST-EMPTY case: the server could not resolve this sender to a
      // roster row. The screen must show the ADDRESS alone — see the exact
      // -equality assertion below for why this row carries the whole rule.
      id: "cm-2",
      from: "m-ghost",
      from_name: "",
      to: "m-planner",
      to_name: "普朗克",
      body: "這則的寄件者解析不到名字",
      ts: 1786000700,
      ts_display: "2026-08-12 22:31:47 +08:00",
      // FOLDED: this message IS here, with this many characters kept back on
      // the server. Not the same thing as chat_earlier_omitted below.
      body_omitted_chars: 1284,
      reply_card_status: "",
      reply_to: "",
      attachments: [
        {
          id: "att-9",
          url: "/api/chat/attachment/att-9",
          filename: "交接筆記.md",
          mime: "text/markdown",
          is_image: false,
        },
      ],
    },
    // ── T-9871: the three shapes a REPLY arrives in ─────────────────────────
    // The snapshot bills `reply_to_chat`'s names and excerpt against the chat
    // budget on every wake, so these characters were already evicting other
    // messages from the payload while nothing on screen showed them.
    //
    // (1) QUOTED — the server resolved the original and both its parties. This
    // is the read that fills the quote's `from_name`/`to_name` at all.
    {
      id: "cm-3",
      from: "owner",
      from_name: "Seth",
      to: "m-planner",
      to_name: "普朗克",
      body: "那就先擋著,我補一句給你參考",
      ts: 1786000800,
      ts_display: "2026-08-12 22:40:11 +08:00",
      body_omitted_chars: 0,
      reply_card_status: "",
      reply_to: "cm-1",
      reply_to_chat: {
        id: "cm-1",
        from: "m-planner",
        from_name: "普朗克",
        to: "owner",
        to_name: "Seth",
        content: "已完成後端組裝,等你決定要不要照這個形狀往下做",
      },
      attachments: [],
    },
    // (2) GONE — `reply_to` is set (this IS a reply and always was) but the
    // server could not rebuild the quote on this read. One fixed sentence, no
    // retry, and the two are told apart by `reply_to`, never by guessing.
    {
      id: "cm-4",
      from: "m-planner",
      from_name: "普朗克",
      to: "owner",
      to_name: "Seth",
      body: "我回的是那則已經被刪掉的訊息",
      ts: 1786000900,
      ts_display: "2026-08-12 22:41:52 +08:00",
      body_omitted_chars: 0,
      reply_card_status: "",
      reply_to: "cm-vanished",
      attachments: [],
    },
    // (3) QUOTED, BUT EMPTY — the original carried only attachments, so its
    // quotable text is "" — a legal value, NOT a failure — and its sender
    // resolved to no roster row, so `from_name` is "". Both honest-empties on
    // one row: the quote must still draw, named by address alone.
    {
      id: "cm-5",
      from: "owner",
      from_name: "Seth",
      to: "m-ghost",
      to_name: "",
      body: "收到你那個附件了",
      ts: 1786000950,
      ts_display: "2026-08-12 22:43:07 +08:00",
      body_omitted_chars: 0,
      reply_card_status: "",
      reply_to: "cm-2",
      reply_to_chat: {
        id: "cm-2",
        from: "m-ghost",
        from_name: "",
        to: "m-planner",
        to_name: "普朗克",
        content: "",
      },
      attachments: [],
    },
  ],
  // TRUNCATED: whole messages that are NOT in this payload at all.
  chat_earlier_omitted: {
    omitted: true,
    hint: "call get_chat with with='m-planner' and BOTH before_ts=1786000001 and before_id='cm-1'",
  },
  tasks: [
    {
      id: "t-1",
      task_no: "T-9001",
      title: "整理 RESUME SUMMARY 面板",
      type_key: "frontend",
      status: "in_progress",
      priority: "medium",
      waiting_reason: "",
      current_step_id: "s-1",
      current_step_name: "串接 lazy fetch",
      progress_done: 1,
      progress_total: 3,
      updated_ts: 1786000900,
      detail_chars: 375,
      // T-91: the wake snapshot's task row carries the handover hold and the
      // reverse dependency edge. Present-and-empty here on purpose — this is
      // the ordinary row, so the parity fixture must show what an ordinary row
      // looks like, not omit the fields.
      lock: "",
      reassigned_from: "",
      reassigned_from_kind: "",
      blocking: [],
      answered_card_steps: ANSWERED_CARD_STEPS,
    },
  ],
  roster: [
    {
      id: "m-planner",
      name: "普朗克",
      kind: "staff",
      role_name: "規劃",
      duty: "把票拆成可以被執行的形狀",
      current_task: "",
      task_status: "",
      waiting_reason: "",
      progress_done: 0,
      progress_total: 0,
      machine: "mach-alpha",
      presence: "online",
    },
    {
      id: "ow-31",
      name: "O-31",
      kind: "outsource",
      role_name: "",
      duty: "",
      current_task: "把座艙那張卡接上快照",
      task_status: "in_progress",
      waiting_reason: "",
      progress_done: 2,
      progress_total: 7,
      machine: "mach-beta",
      presence: "offline",
    },
  ],
  machines: {
    you_are_on: "mach-alpha",
    list: [
      { machine_id: "mach-alpha", display_name: "阿爾法", online: true },
      { machine_id: "mach-beta", display_name: "貝塔", online: false },
    ],
  },
};

// These maps are the seam's explicit wire-to-view roll-call. Comparing only
// Object.keys(MAPPED()) cannot see a wire field that the mapper ignores: the
// ignored field never reaches the view model. Keeping both directions here
// means a new wire key must be mapped AND declared, while a new view key must
// still have a wire source. This is deliberately a fixture-level contract,
// not a test of the generated TypeScript type (optional fields are where the
// original defect hid).
const RESUME_WIRE_TO_VIEW = {
  identity: "identity",
  generated_at: "generatedAt",
  note: "note",
  overview: "overview",
  chat: "chat",
  chat_earlier_omitted: "chatEarlierOmitted",
  tasks: "tasks",
  roster: "roster",
  machines: "machines",
} as const;

const RESUME_OVERVIEW_WIRE_TO_VIEW = {
  chat_count: "chatCount",
  chat_chars: "chatChars",
  tasks_returned: "tasksReturned",
  tasks_open_total: "tasksOpenTotal",
  tasks_detail_chars: "tasksDetailChars",
  cards_waiting: "cardsWaiting",
  cards_answered_recent: "cardsAnsweredRecent",
  roster_chars: "rosterChars",
  machines_chars: "machinesChars",
  steps_on_answered_card: "stepsOnAnsweredCard",
  steps_on_answered_card_chars: "stepsOnAnsweredCardChars",
} as const;

const RESUME_TASK_WIRE_TO_VIEW = {
  id: "id",
  task_no: "taskNo",
  title: "title",
  type_key: "typeKey",
  status: "status",
  priority: "priority",
  waiting_reason: "waitingReason",
  current_step_id: "currentStepId",
  current_step_name: "currentStepName",
  progress_done: "progressDone",
  progress_total: "progressTotal",
  updated_ts: "updatedTs",
  detail_chars: "detailChars",
  answered_card_steps: "answeredCardSteps",
  lock: "lock",
  reassigned_from: "reassignedFrom",
  reassigned_from_kind: "reassignedFromKind",
  blocking: "blocking",
} as const;

const ANSWERED_CARD_STEP_WIRE_TO_VIEW = {
  step_id: "stepId",
  step_name: "stepName",
  card_id: "cardId",
} as const;

function assertWireKeysAreMapped(
  wire: object,
  wireToView: Record<string, string>,
  view: object,
) {
  expect(Object.keys(wire).sort()).toEqual(Object.keys(wireToView).sort());
  expect(Object.values(wireToView).sort()).toEqual(Object.keys(view).sort());
}

const MAPPED = () => toMemberResumeSummary(WIRE);

/** One test needs the SAME snapshot with a single marker down, to prove the
 * cockpit draws nothing rather than an empty line. Everything else reads the
 * fixture unchanged, so the override is a null-by-default slot rather than a
 * second mock. */
const OVERRIDE: { value: MemberResumeSummaryView | null } = { value: null };

vi.mock("../api", () => ({
  api: {
    getMemberResumeSummary: () => Promise.resolve(OVERRIDE.value ?? MAPPED()),
  },
}));

/** Render the card and open it — the section only fetches on first expand. */
async function open() {
  const utils = render(
    <I18nProvider>
      <ResumeSummaryCard agentId="mira" />
    </I18nProvider>,
  );
  fireEvent.click(utils.getByTestId("mp-resume-toggle"));
  await waitFor(() =>
    expect(utils.queryByTestId("mp-resume-overview")).not.toBeNull(),
  );
  return utils;
}

const R = zh.mp.resumeSummary;
const txt = (el: Element | null) => (el?.textContent ?? "").replace(/\s+/g, " ").trim();

beforeEach(() => {
  document.body.innerHTML = "";
  // Every test but one reads the fixture unchanged; reset here so an override
  // cannot leak into the next test and quietly weaken it.
  OVERRIDE.value = null;
});

describe("ResumeSummaryCard renders the SAME snapshot the agent receives", () => {
  it("shows the anchor the whole payload is read against — generated_at, VERBATIM", async () => {
    // Without it, every ts_display below is a timestamp with nothing to be
    // measured against. Byte-equal to the wire: this component owns no clock.
    const u = await open();
    expect(txt(u.getByTestId("mp-resume-generated-at-value"))).toBe(
      WIRE.generated_at,
    );
  });

  it("names BOTH parties of every message — display name AND the id you reply to", async () => {
    // 🔴 This is the assertion the "→ / ←" arrow could not satisfy. The arrow
    // showed neither an id nor a name: two different senders were the same
    // glyph, and a reader could not tell who said anything.
    //
    // 🔴 WHOLE-STRING equality, and the expectation is BUILT FROM THE SAME WIRE
    // ROW the screen was built from — `name + id`, in that order, for every
    // message in the payload. A `toContain("普朗克")` passes on a row that also
    // prints something else, on a row that prints the name twice, and on a row
    // that prints the name where the id belongs; it also cannot see a party
    // that stopped being rendered at all as long as one neighbour still says
    // the word. The composed form has none of those holes, and it grows with
    // the fixture instead of naming two rows out of it.
    const u = await open();
    const froms = u.getAllByTestId("mp-resume-chat-from").map(txt);
    const tos = u.getAllByTestId("mp-resume-chat-to").map(txt);

    expect(froms).toEqual(WIRE.chat!.map((c) => `${c.from_name}${c.from}`));
    expect(tos).toEqual(WIRE.chat!.map((c) => `${c.to_name}${c.to}`));
  });

  it("shows the id ALONE when the server resolved no name — never the id dressed as one", async () => {
    // EXACT equality, not `toContain`: `toContain("m-ghost")` is satisfied by
    // a back-filled name too, so it would pass on the very bug this guards.
    const u = await open();
    expect(txt(u.getAllByTestId("mp-resume-chat-from")[1])).toBe("m-ghost");
  });

  it("shows each message's time using the SERVER's rendered stamp, not one of its own", async () => {
    // 🔴 Both halves matter. The first says a time is on screen at all (the
    // card used to read `m.ts` nowhere and show none). The second says it is
    // the SERVER's string — a cockpit-side formatter would state the same
    // instant in the viewer's zone while the agent reads the server's, and a
    // "some time is displayed" assertion would happily pass on that.
    const u = await open();
    const stamps = u.getAllByTestId("mp-resume-chat-ts").map(txt);
    expect(stamps).toEqual(WIRE.chat!.map((c) => c.ts_display));
    // And the raw epoch is never what the message's own time line shows — it
    // is the machine-readable twin, not something a reader was meant to
    // decode. Scoped to the meta line ON PURPOSE: the epoch legitimately
    // appears elsewhere on screen, inside the server's verbatim cursor hint,
    // and a whole-document assertion would have demanded we corrupt that hint
    // to stay green. (This is not hypothetical — the document-wide version of
    // this line went red on exactly that.)
    const metas = [...u.container.querySelectorAll(".mp-resume__chatmeta")].map(
      txt,
    );
    for (const meta of metas) {
      for (const c of WIRE.chat!) {
        expect(meta).not.toContain(String(c.ts));
      }
    }
  });

  it("draws every circled option on the card that opened it, tagging the ai_pick option and not the first", async () => {
    const u = await open();
    // Each option chip, WHOLE — the wording plus exactly the tags it earned.
    // The AI tag rides the SECOND option here (that is where ai_pick is), and
    // 已選 rides BOTH, because both indices are in answer_option_idxs. A reader
    // that still tagged options[0], or that drew one 已選 for a two-option
    // answer, disagrees with one of these strings.
    expect(u.getAllByTestId("mp-resume-card-option").map(txt)).toEqual([
      "照這個形狀做已選",
      "先擋著等下一輪AI 建議已選",
    ]);
    expect(
      u
        .getAllByTestId("mp-resume-card-option")
        .map((el) => el.getAttribute("data-picked")),
    ).toEqual(["true", "true"]);
    expect(txt(u.getByTestId("mp-resume-card-answer-text"))).toBe(
      "補充文字 先擋著,理由寫在卡上",
    );
    expect(txt(u.getByTestId("mp-resume-card-answered-at"))).toBe(
      "回覆於 2026-08-12 22:23:20 +08:00",
    );
    // ONE home for the card: it rides the message, so there is exactly one.
    expect(u.getAllByTestId("mp-resume-chat-card")).toHaveLength(1);
  });

  it("carries the message's reply-card status and attachments across", async () => {
    // WHOLE-STRING, composed from the same wire row plus the label the card
    // prints beside it. `toContain(reply_card_status)` was satisfied by a row
    // that printed the status with no label at all, or with the wrong one — and
    // an "answered" that arrived unlabelled next to a timestamp is exactly the
    // kind of thing a reader mis-reads.
    const u = await open();
    expect(txt(u.getByTestId("mp-resume-chat-cardstatus"))).toBe(
      `${R.replyCardStatusLabel}: ${WIRE.chat![0].reply_card_status}`,
    );
    expect(txt(u.getByTestId("mp-resume-chat-attachments"))).toBe(
      `${R.cardAttachmentsLabel} ${WIRE.chat![1].attachments![0].filename}`,
    );
  });

  // ── T-9871 · THE QUOTE THE SNAPSHOT ALREADY PAID FOR ────────────────────────
  // `resumeChatMessageChars` bills the quote's from_name + to_name + content
  // against the chat budget, so every wake spent characters — and evicted whole
  // messages to afford them — on a quote this card drew nothing of. Reading a
  // reply here meant reading half a conversation: the card showed neither what
  // was replied to NOR that the message was a reply at all.
  it("draws the quote a reply carries: both parties of the QUOTED message, and its excerpt", async () => {
    const u = await open();
    const wire = WIRE.chat![2];
    const q = wire.reply_to_chat!;
    const quotes = u.getAllByTestId("mp-resume-chat-quote");

    // One quote per REPLY — the three reply rows of the fixture, and not one
    // for the messages that are not replies. Anti-vacuity for everything below.
    expect(quotes).toHaveLength(
      WIRE.chat!.filter((c) => (c.reply_to ?? "") !== "").length,
    );

    // The quoted message's OWN parties — not this message's, and not this
    // thread's peer. Composed from the quote object itself, whole-string.
    expect(txt(u.getAllByTestId("mp-resume-chat-quote-from")[0])).toBe(
      `${q.from_name}${q.from}`,
    );
    expect(txt(u.getAllByTestId("mp-resume-chat-quote-to")[0])).toBe(
      `${q.to_name}${q.to}`,
    );
    expect(txt(u.getAllByTestId("mp-resume-chat-quote-body")[0])).toBe(
      q.content,
    );
    // 🔴 THE SAME PAIR HAS TO REACH THE A11Y TREE. The strip's only structural
    // signal that it is a quotation is its role + name; a screen-reader user who
    // gets neither hears the quoted sentence as something this sender is saying
    // now. Same assertion shape as ChatArea.reply-to.test.tsx.
    expect(quotes[0].getAttribute("aria-label")).toBe(
      zh.chat.replyQuoteRoleWho(`${q.from_name} → ${q.to_name}`),
    );

    // 🔴 THE QUOTE IS NOT THE MESSAGE. Both are 「寄件者 → 收件者」 lines in the
    // same card, and this row is the one where they DISAGREE (the reply runs
    // owner → m-planner, the quoted line ran m-planner → owner), so a quote
    // accidentally fed the replying message's own parties would be caught here
    // and nowhere else.
    const row = u.getAllByTestId("mp-resume-chat-row")[2];
    expect(txt(row.querySelector('[data-testid="mp-resume-chat-from"]'))).toBe(
      `${wire.from_name}${wire.from}`,
    );
    expect(txt(u.getAllByTestId("mp-resume-chat-quote-from")[0])).not.toBe(
      `${wire.from_name}${wire.from}`,
    );
  });

  it("says the original is gone — the fixed sentence — when the reply has no quote", async () => {
    // TWO STATES, NO THIRD. `reply_to` set with `reply_to_chat` absent is the
    // server's own answer on THIS read: the original no longer exists. Nothing
    // is in flight, so there is no third "still loading" shape to draw, and the
    // sentence is the chat pane's — one claim, one wording, in both places.
    const u = await open();
    const gone = u.getAllByTestId("mp-resume-chat-quote-gone");
    expect(gone).toHaveLength(
      WIRE.chat!.filter(
        (c) => (c.reply_to ?? "") !== "" && c.reply_to_chat == null,
      ).length,
    );
    expect(txt(gone[0])).toBe(zh.chat.replyQuoteGone);
    // …and the accessible name drops to the UNNAMED one: with no snapshot there
    // is nobody to name, and a label still naming somebody would be a name this
    // read did not resolve.
    const goneStrip = u.getAllByTestId("mp-resume-chat-quote")[1];
    expect(goneStrip.getAttribute("aria-label")).toBe(zh.chat.replyQuoteRole);
    // …and that row draws NO quoted parties: a gone quote has nobody to name.
    const row = u.getAllByTestId("mp-resume-chat-row")[3];
    expect(row.querySelector('[data-testid="mp-resume-chat-quote-from"]')).toBeNull();
  });

  it("draws an attachment-only original as a NAMED quote with an empty line, not as a gone one", async () => {
    // `content: ""` is legal (the quoted message carried only attachments) and
    // is a different fact from "the original is gone". Folding one into the
    // other tells the reader the conversation lost a message it still has.
    // The same row also carries `from_name: ""` — the address alone, never the
    // id dressed as a name.
    const u = await open();
    const q = WIRE.chat![4].reply_to_chat!;
    const row = u.getAllByTestId("mp-resume-chat-row")[4];
    expect(
      txt(row.querySelector('[data-testid="mp-resume-chat-quote-body"]')),
    ).toBe(q.content);
    expect(
      txt(row.querySelector('[data-testid="mp-resume-chat-quote-from"]')),
    ).toBe(q.from);
    expect(
      row.querySelector('[data-testid="mp-resume-chat-quote-gone"]'),
    ).toBeNull();
    // The accessible name follows the SAME honest-empty rule by falling back to
    // the ADDRESS — the a11y tree may not invent a name the payload does not
    // carry, and it may not go silent either.
    expect(
      row
        .querySelector('[data-testid="mp-resume-chat-quote"]')!
        .getAttribute("aria-label"),
    ).toBe(zh.chat.replyQuoteRoleWho(`${q.from} → ${q.to_name}`));
  });

  it("draws no quote at all on a message that is not a reply", async () => {
    // The row's shape must not gain an empty quote slot: `reply_to: ""` means
    // this message answers nothing, and an empty bordered strip above every
    // ordinary message would read as a quote that failed to load.
    const u = await open();
    const row = u.getAllByTestId("mp-resume-chat-row")[0];
    expect(row.querySelector('[data-testid="mp-resume-chat-quote"]')).toBeNull();
  });

  it("draws BOTH the folded marker and the absent marker", async () => {
    const u = await open();
    const folded = txt(u.getByTestId("mp-resume-chat-body-omitted"));
    const cut = txt(u.getByTestId("mp-resume-chat-earlier-omitted"));

    expect(folded).toContain(String(WIRE.chat![1].body_omitted_chars));
    expect(cut).toContain(R.chatCutLabel);
  });

  // 🔴 THE ABSENT MARKER SITS ABOVE THE MESSAGES, AND THE POSITION IS THE
  // CLAIM (owner 2026-08-15: 「chat 是由舊到新 所以更舊以前的資訊要他撈取這個字
  // 應該是在訊息一開始不是結尾」). The list runs OLD → NEW, so the edge this
  // marker describes — "there may be more, further back" — is the TOP one. It
  // rendered below the last message until now, which was right only while the
  // list ran new → old; the order changed and the marker was not moved, so it
  // pointed at the wrong end and a reader scrolling up to the start had no way
  // to know the line was cut there.
  //
  // Asserting DOM ORDER, not mere presence: a presence check stays green with
  // the marker back at the bottom, which is exactly the state being fixed.
  it("puts the absent marker BEFORE the messages, because the list runs old → new", async () => {
    const u = await open();
    const cut = u.getByTestId("mp-resume-chat-earlier-omitted");
    const rows = u.getAllByTestId("mp-resume-chat-row");
    expect(rows.length).toBeGreaterThan(0);
    // MUTANT CHECK: move the marker back below the list and this goes red on
    // every row — verified by moving the JSX block, not assumed.
    for (const [i, row] of rows.entries()) {
      expect(
        cut.compareDocumentPosition(row) & Node.DOCUMENT_POSITION_FOLLOWING,
        `chat row ${i} must come AFTER the absent marker`,
      ).toBeTruthy();
    }
  });

  // 🔴 AND IT SURVIVES AN EMPTY CHAT — the payload that needs it most.
  //
  // Moving the marker to the top the first time put it INSIDE the "there are
  // messages" branch, which deleted it from the one snapshot whose reader is
  // most likely to be misled: budget pressure can evict EVERY message, and the
  // screen then said 「沒有訊息」 and nothing else. "This line is empty" and
  // "this line was cut before anything fit" are different facts, and the wire
  // distinguishes them — chat: [] with the cut flag RAISED is the second one.
  //
  // The regression shipped past a full green suite because every fixture in
  // this file has messages in it. Independent review found it; no test could.
  it("still draws the absent marker when the chat came back EMPTY", async () => {
    OVERRIDE.value = { ...MAPPED(), chat: [] };
    const u = await open();
    // The empty state is present — this is the payload we mean.
    expect(u.queryByTestId("mp-resume-chat-row")).toBeNull();
    // ...and the reader is still told the line was cut, with the recovery
    // instruction intact. MUTANT CHECK: nest this marker back inside the
    // non-empty branch and both of these go red.
    expect(txt(u.getByTestId("mp-resume-chat-earlier-omitted"))).toContain(
      R.chatCutLabel,
    );
    expect(txt(u.getByTestId("mp-resume-chat-earlier-omitted-hint"))).toContain(
      "get_chat",
    );
  });

  // 🔴 THE SERVER'S PROSE IS RENDERED, NOT DUMPED. `note` and the cut `hint`
  // are ONE text written for both readers (owner: 「應該好好寫 讓兩邊看得懂」),
  // and the only formatting plain text has is line breaks, `**` and backticks.
  // Printed as bare text nodes they collapsed into a single run-on paragraph
  // with the markup showing — byte-verbatim and unreadable, which is the exact
  // failure this ticket was opened for.
  //
  // Asserting the RENDERED SHAPE (<strong>, <br>, <code>), not the characters:
  // a text assertion stays green on the wall of prose, because the bytes never
  // changed. That is what made this invisible to every existing test.
  it("renders the note and the hint as MARKUP, not as a wall of text", async () => {
    OVERRIDE.value = {
      ...MAPPED(),
      note: "第一行\n第二行有**重點**與 `get_task`",
      chatEarlierOmitted: {
        omitted: true,
        hint: "去抓：呼叫 `get_chat`，**成對**送 `before_ts` 與 `before_id`",
      },
    };
    const u = await open();
    const note = u.getByTestId("mp-resume-note");
    const hint = u.getByTestId("mp-resume-chat-earlier-omitted-hint");

    // Emphasis and code are elements, not literal ** and backticks on screen.
    expect(note.querySelector("strong")).not.toBeNull();
    expect(note.querySelector("code")).not.toBeNull();
    expect(hint.querySelector("strong")).not.toBeNull();
    expect(hint.querySelector("code")).not.toBeNull();
    expect(txt(note)).not.toContain("**");
    expect(txt(hint)).not.toContain("**");

    // `breaks` is the half a plain <Markdown> would silently drop: the source
    // separates its lines with SINGLE newlines, which standard markdown joins
    // into one paragraph. MUTANT CHECK: remove the `breaks` prop and this line
    // goes red while every assertion above stays green.
    expect(note.querySelector("br")).not.toBeNull();
  });

  // 🔴 A FOLDED message and an ABSENT one must not be described with shared
  // vocabulary. They are different failures. One says "this is here, shortened,
  // the rest is on the server"; the other says "these may not be here at all,
  // go and fetch them". A reader who reads one as the other concludes it has
  // seen a conversation it has not seen.
  //
  // 🔴 WHY THE COMPARISON UNIT IS NOT "split on spaces and punctuation".
  // That was the previous shape of this guard and it was NEAR-VACUOUS: Chinese
  // writes no word boundaries, so two zh strings come out as one token each and
  // can only collide by being character-for-character identical. It was PROVED
  // vacuous — an independent review re-worded `bodyOmittedLead` to share six
  // characters and the whole of its meaning with the cut label, and this file
  // stayed green. And `en`, which is the half a whitespace split would actually
  // work on, was never compared at all.
  //
  // So: Han text is compared CHARACTER by character (the smallest unit zh
  // actually has), Latin text WORD by word, case-folded, and BOTH shipped
  // locales are checked. Overlap in either alphabet, in either locale, is red.
  const units = (s: string) => {
    const out = new Set<string>();
    for (const w of s.toLowerCase().match(/[a-z0-9_]{2,}/g) ?? []) out.add(w);
    for (const ch of s) if (/\p{Script=Han}/u.test(ch)) out.add(ch);
    return out;
  };

  // 🔴 TWO markers, compared PAIRWISE. There was briefly a third
  // (`budgetOverLabel` — OVER BUDGET); it went with the marker itself on
  // 2026-08-13, once the per-line floor was removed and the budget became a real
  // ceiling that the block cannot exceed. The pair that remains is the pair that
  // has always mattered: sharing a word between FOLDED and ABSENT is how a
  // reader concludes it has seen a conversation it has not seen.
  it.each([
    ["zh", zh.mp.resumeSummary],
    ["en", en.mp.resumeSummary],
  ])(
    "🔴 [%s] words FOLDED and ABSENT with no vocabulary in common",
    (_locale, r) => {
      const sets: [string, Set<string>][] = [
        // The per-message mark AND the once-per-block legend that explains it
        // — both belong to the FOLDED vocabulary, so both are compared.
        ["folded", units(r.bodyOmittedMark + " " + r.bodyOmittedNote)],
        ["absent", units(r.chatCutLabel)],
      ];
      // A guard that compares empty sets proves nothing — every vocabulary has
      // to exist before "they do not overlap" means anything.
      for (const [name, set] of sets) {
        expect(`${name}:${set.size > 2}`).toBe(`${name}:true`);
      }
      for (const [aName, a] of sets) {
        for (const [bName, b] of sets) {
          if (aName >= bName) continue;
          expect(`${aName}∩${bName}=${[...a].filter((u) => b.has(u)).join(",")}`)
            .toBe(`${aName}∩${bName}=`);
        }
      }
    },
  );

  it("shows the server's own recovery hint VERBATIM, not a cockpit paraphrase", async () => {
    // The hint names the exact cursor pair to send. A re-worded copy here
    // would be a second procedure that cannot be kept in step with the
    // endpoint — and would look right while being wrong.
    const u = await open();
    expect(txt(u.getByTestId("mp-resume-chat-earlier-omitted-hint"))).toBe(
      WIRE.chat_earlier_omitted!.hint,
    );
  });

  it("🔴 draws the ROSTER block — the section the seam used to drop", async () => {
    const u = await open();
    const rows = u.getAllByTestId("mp-resume-roster-row").map(txt);
    expect(rows).toHaveLength(WIRE.roster!.length);
    for (const [i, r] of WIRE.roster!.entries()) {
      expect(rows[i]).toContain(r.id);
      expect(rows[i]).toContain(r.name);
      expect(rows[i]).toContain(r.presence);
      expect(rows[i]).toContain(r.machine);
      if (r.duty !== "") expect(rows[i]).toContain(r.duty);
      if (r.role_name !== "") expect(rows[i]).toContain(r.role_name);
      // A contractor's bound task NEVER appears without its status: 0/0 alone
      // cannot tell "a bound task with no steps yet" from "no bound task".
      if (r.current_task !== "") {
        expect(rows[i]).toContain(r.current_task);
        expect(rows[i]).toContain(r.task_status);
        expect(rows[i]).toContain(`${r.progress_done}/${r.progress_total}`);
      }
    }
  });

  it("🔴 draws the MACHINE block — the other section the seam used to drop", async () => {
    const u = await open();
    const rows = u.getAllByTestId("mp-resume-machine-row").map(txt);
    expect(rows).toHaveLength(WIRE.machines!.list.length);
    for (const [i, m] of WIRE.machines!.list.entries()) {
      // The STABLE id, always — a machine is addressed by id, never by the
      // name a host reports for itself.
      expect(rows[i]).toContain(m.machine_id);
      expect(rows[i]).toContain(m.display_name);
      expect(rows[i]).toContain(m.online ? R.machineOnline : R.machineOffline);
    }
    expect(txt(u.getByTestId("mp-resume-you-are-on"))).toContain(
      WIRE.machines!.you_are_on,
    );
  });

  it("reports every size figure the agent's copy reports, each in its own slot", async () => {
    const u = await open();
    const o = WIRE.overview!;
    const pairs: [string, number][] = [
      ["chatCount", o.chat_count],
      ["chatChars", o.chat_chars],
      ["tasksReturned", o.tasks_returned],
      ["tasksOpenTotal", o.tasks_open_total],
      ["tasksDetailChars", o.tasks_detail_chars],
      ["cardsWaiting", o.cards_waiting],
      ["cardsAnsweredRecent", o.cards_answered_recent],
      ["rosterChars", o.roster_chars!],
      ["machinesChars", o.machines_chars!],
      ["stepsOnAnsweredCard", o.steps_on_answered_card!],
      ["stepsOnAnsweredCardChars", o.steps_on_answered_card_chars!],
    ];
    for (const [key, value] of pairs) {
      expect(txt(u.getByTestId(`mp-resume-stat-${key}`))).toBe(String(value));
    }
  });

  it("keeps answered-card size figures tied to the pointers it carries", () => {
    const steps = WIRE.tasks!.flatMap((task) => task.answered_card_steps ?? []);
    expect(WIRE.overview!.steps_on_answered_card).toBe(steps.length);
    expect(WIRE.overview!.steps_on_answered_card_chars).toBe(
      answeredCardStepChars(steps),
    );
  });

  it("shows answered-card pointers as attention, not completed work", async () => {
    const u = await open();
    const hold = u.getByTestId("mp-resume-task-answered-card");
    expect(txt(hold)).toContain(R.answeredCardSteps);
    const rows = u.getAllByTestId("mp-resume-answered-card-step").map(txt);
    expect(rows).toHaveLength(WIRE.tasks![0].answered_card_steps!.length);
    for (const [i, step] of WIRE.tasks![0].answered_card_steps!.entries()) {
      expect(rows[i]).toContain(step.step_name);
      expect(rows[i]).toContain(step.card_id);
    }
    // The task remains in its server-reported working state. The pointer is
    // not a verdict that the answer approved the step or that work is done.
    expect(txt(u.getByTestId("mp-resume-task-answered-card").parentElement)).toContain(
      WIRE.tasks![0].status,
    );
  });

  it("keeps every section NAMED even while its body is folded away", async () => {
    // Which sections the snapshot carries is itself part of what is being
    // compared against the agent's copy. A collapsed section that vanished
    // entirely would be indistinguishable from one the payload never had.
    const u = await open();
    for (const id of [
      "mp-resume-chat-section",
      "mp-resume-tasks-section",
      "mp-resume-roster-section",
      "mp-resume-machines-section",
    ]) {
      fireEvent.click(u.getByTestId(`${id}-toggle`));
    }
    expect(txt(screen.getByTestId("mp-resume-chat-section"))).toContain(
      R.chatSection,
    );
    expect(txt(screen.getByTestId("mp-resume-roster-section"))).toContain(
      R.rosterSection,
    );
    expect(txt(screen.getByTestId("mp-resume-machines-section"))).toContain(
      R.machinesSection,
    );
    expect(txt(screen.getByTestId("mp-resume-tasks-section"))).toContain(
      R.tasksSection,
    );
    // …and the folded bodies really are gone, so the check above is not just
    // reading a section that never collapsed.
    expect(screen.queryAllByTestId("mp-resume-roster-row")).toHaveLength(0);
  });

  it("renders agent-authored text as markdown WITHOUT letting inline HTML through", async () => {
    // Bodies come from agents and from outside the studio. `Markdown` builds
    // React elements only, so markup in a snapshot stays inert text.
    const u = await open();
    const body = u.container.querySelector(".mp-resume__chatbody");
    expect(body).not.toBeNull();
    expect(u.container.innerHTML).not.toContain("<script");
  });
});

describe("the roll-call: a field the seam gains cannot stay undrawn", () => {
  it("sees wire-only fields before the mapper can drop them", () => {
    const mapped = MAPPED();
    assertWireKeysAreMapped(WIRE, RESUME_WIRE_TO_VIEW, mapped);
    assertWireKeysAreMapped(
      WIRE.overview!,
      RESUME_OVERVIEW_WIRE_TO_VIEW,
      mapped.overview,
    );
    assertWireKeysAreMapped(
      WIRE.tasks![0],
      RESUME_TASK_WIRE_TO_VIEW,
      mapped.tasks[0],
    );
    assertWireKeysAreMapped(
      WIRE.tasks![0].answered_card_steps![0],
      ANSWERED_CARD_STEP_WIRE_TO_VIEW,
      mapped.tasks[0].answeredCardSteps[0],
    );
  });

  // 🔴 Per-field assertions only ever cover the fields somebody remembered.
  // These pin the SHAPE, so the next field added to the view model announces
  // itself here instead of quietly joining roster/machines on the floor.
  it("the mapped snapshot carries exactly the sections this file asserts on", () => {
    expect(Object.keys(MAPPED()).sort()).toEqual(
      [
        "chat",
        "chatEarlierOmitted",
        "generatedAt",
        "identity",
        "machines",
        "note",
        "overview",
        "roster",
        "tasks",
      ].sort(),
    );
  });

  it("a mapped chat message carries exactly the fields this file asserts on", () => {
    expect(Object.keys(MAPPED().chat[0]).sort()).toEqual(
      [
        "attachments",
        "body",
        "bodyOmittedChars",
        "card",
        "from",
        "fromName",
        "id",
        "replyCardId",
        "replyCardStatus",
        // T-4e95: the two halves of a reply, and they are deliberately two.
        // `replyTo` is the id and says THAT this message is a reply; it never
        // disappears. `replyToChat` is the quoted sender + a server-shortened
        // line and says WHAT it replied to; it is rebuilt on every read and is
        // legitimately null while `replyTo` is set (the original is gone).
        // Collapsing them into one field is the change this roll-call is here
        // to make announce itself.
        "replyTo",
        "replyToChat",
        "to",
        "toName",
        "ts",
        "tsDisplay",
      ].sort(),
    );
  });

  it("a mapped roster row and machine block carry exactly the fields this file asserts on", () => {
    expect(Object.keys(MAPPED().roster[0]).sort()).toEqual(
      [
        "currentTask",
        "duty",
        "id",
        "kind",
        "machine",
        "name",
        "presence",
        "progressDone",
        "progressTotal",
        "roleName",
        "taskStatus",
        "waitingReason",
      ].sort(),
    );
    expect(Object.keys(MAPPED().machines!).sort()).toEqual(
      ["list", "youAreOn"].sort(),
    );
    expect(Object.keys(MAPPED().machines!.list[0]).sort()).toEqual(
      ["displayName", "machineId", "online"].sort(),
    );
  });

  it("the mapper carries roster and machines ACROSS, rather than declaring them and dropping them", () => {
    // The narrowest statement of the original defect, at the layer it lived
    // in — so it stays red even if the component is rewritten around it.
    const m = MAPPED();
    expect(m.roster.map((r) => r.id)).toEqual(WIRE.roster!.map((r) => r.id));
    expect(m.machines!.list.map((x) => x.machineId)).toEqual(
      WIRE.machines!.list.map((x) => x.machine_id),
    );
    expect(m.generatedAt).toBe(WIRE.generated_at);
    expect(m.chatEarlierOmitted.hint).toBe(WIRE.chat_earlier_omitted!.hint);
  });
});
