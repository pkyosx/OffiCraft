// MemberDetailPanel · webhook 可觀測性 事件統計 window (M4).
//
// Locked here:
//   1. Every webhook row-HEAD renders the 事件統計 entry as a constant label
//      next to the endpoint id chip (T-069d rework: owner wants the numbers
//      kept out of the row); clicking it opens a window with two stat
//      blocks: last-received recency and dropped count with the coarse drop
//      reason (T-2c1c: the delivered tile is gone, and the window title no
//      longer repeats the endpoint id chip — the window opens from that row).
//   2. A never-called endpoint's window reads ONLY the "never received" face
//      (title + hint, no stat blocks, no requests section).
//   3. An unknown drop reason falls back to the raw string (no crash).
//   4. The ✕ button closes the window.
//   5. The 最近請求 section lists the /in debug ring buffer newest-first with
//      an outcome badge per row (drops carry their reason label, truncation is
//      flagged); a row expands to its formatted headers + raw body; an empty
//      ring reads the empty face; a failed fetch reads the error face.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";
import type { WebhookEndpoint, WebhookRequestLog } from "../api/adapter";

let store: WebhookEndpoint[] = [];
let requests: WebhookRequestLog[] = [];
let requestsFail = false;

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
    patchMember: () => Promise.resolve({}),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
    listWebhooks: () => Promise.resolve(store.map((e) => ({ ...e }))),
    createWebhook: () => Promise.reject(new Error("unused")),
    updateWebhook: () => Promise.reject(new Error("unused")),
    deleteWebhook: () => Promise.resolve(),
    listWebhookRequests: () =>
      requestsFail
        ? Promise.reject(new Error("boom"))
        : Promise.resolve(requests.map((r) => ({ ...r }))),
    subscribeEvents: () => () => {},
  },
}));

function mkMember(): Member {
  return {
    id: "mira",
    memberId: "MB-AST001",
    name: "Mira",
    role: "assistant",
    status: "offline",
    lifecycle: "offline",
    model: "opus",
    effort: "medium",
    kind: "assistant",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-mira",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

function mkEndpoint(over: Partial<WebhookEndpoint> = {}): WebhookEndpoint {
  return {
    endpointId: "pr-events",
    purpose: "",
    status: "enabled",
    createdTs: 0,
    token: "mock-token-000000000000",
    platform: "generic",
    hasSigningSecret: false,
    lastReceivedTs: 0,
    deliveredCount: 0,
    droppedCount: 0,
    lastDropReason: "",
    ...over,
  };
}

function mkRequest(over: Partial<WebhookRequestLog> = {}): WebhookRequestLog {
  return {
    ts: Date.now() / 1000 - 60,
    outcome: "delivered",
    headers: JSON.stringify({ "X-Github-Event": ["pull_request"] }),
    body: '{"action":"opened"}',
    truncated: false,
    ...over,
  };
}

async function renderStatsWindow(
  endpoint: WebhookEndpoint,
  rows: WebhookRequestLog[] = [],
  fail = false
) {
  store = [endpoint];
  requests = rows;
  requestsFail = fail;
  const utils = render(
    <I18nProvider>
      <MemberDetailPanel member={mkMember()} onBack={() => {}} onRename={() => {}} />
    </I18nProvider>
  );
  fireEvent.click(utils.getByTestId("mp-webhook-toggle"));
  const entry = await utils.findByTestId(
    `mp-webhook-stats-${endpoint.endpointId}`
  );
  // T-069d rework: the row-head entry reads the constant 事件統計 label —
  // no counters in the row regardless of traffic.
  expect(entry.textContent).toBe(zh.mp.webhook.statsTitle);
  expect(utils.queryByTestId("mp-webhook-stats-modal")).toBeNull();
  fireEvent.click(entry);
  // Flush the modal's async ring-buffer fetch so no state lands outside act.
  await act(async () => {});
  const body = utils.getByTestId("mp-webhook-stats-body");
  return { utils, body };
}

const w = zh.mp.webhook;

describe("MemberDetailPanel · webhook 事件統計 window", () => {
  it("opens from the row entry with recency + dropped stat blocks and the drop-reason label", async () => {
    const { utils, body } = await renderStatsWindow(
      mkEndpoint({
        lastReceivedTs: Date.now() / 1000 - 300,
        deliveredCount: 12,
        droppedCount: 2,
        lastDropReason: "sig_failed",
      })
    );
    const blocks = Array.from(
      body.querySelectorAll(".mp-webhook__stat")
    ).map((el) => el.textContent);
    expect(blocks).toEqual([
      `${w.statsLastReceivedLabel}${w.statsAgo("5m")}`,
      `${w.statsDroppedLabel}2${w.dropReasonSigFailed}`,
    ]);
    // T-2c1c: the window head is just the title + ✕ — no endpoint id chip
    // (the window opens from that endpoint's row, repeating it says nothing).
    const head = utils
      .getByTestId("mp-webhook-stats-modal")
      .querySelector(".mp-webhook__statshead");
    expect(head?.textContent).not.toContain("pr-events");
  });

  it("shows only the never-received face for an endpoint with no traffic", async () => {
    const { body } = await renderStatsWindow(mkEndpoint());
    expect(body.textContent).toBe(`${w.statsNever}${w.statsNeverHint}`);
    expect(body.querySelector(".mp-webhook__stat")).toBeNull();
  });

  it("falls back to the raw string for an unknown drop reason", async () => {
    const { body } = await renderStatsWindow(
      mkEndpoint({
        lastReceivedTs: Date.now() / 1000 - 90,
        droppedCount: 1,
        lastDropReason: "weird_future_reason",
      })
    );
    expect(body.textContent).toContain("weird_future_reason");
  });

  it("closes via the ✕ button", async () => {
    const { utils } = await renderStatsWindow(mkEndpoint());
    fireEvent.click(utils.getByTestId("mp-webhook-stats-close"));
    expect(utils.queryByTestId("mp-webhook-stats-modal")).toBeNull();
  });

  it("lists the recent requests newest-first with outcome badges and a truncation tag, and a row expands to headers + body", async () => {
    const { utils } = await renderStatsWindow(
      mkEndpoint({ lastReceivedTs: Date.now() / 1000 - 60, deliveredCount: 2 }),
      [
        mkRequest(),
        mkRequest({
          ts: Date.now() / 1000 - 3600,
          outcome: "dropped:sig_failed",
          body: '{"probe":true}',
          truncated: true,
        }),
        mkRequest({ ts: Date.now() / 1000 - 7200, outcome: "ping" }),
      ]
    );
    const row0 = await utils.findByTestId("mp-webhook-request-0");
    expect(row0.textContent).toContain(w.outcomeDelivered);
    const row1 = utils.getByTestId("mp-webhook-request-1");
    expect(row1.textContent).toContain(
      `${w.outcomeDropped} · ${w.dropReasonSigFailed}`
    );
    expect(row1.textContent).toContain(w.requestTruncated);
    expect(utils.getByTestId("mp-webhook-request-2").textContent).toContain(
      w.outcomePing
    );

    expect(utils.queryByTestId("mp-webhook-request-detail-1")).toBeNull();
    fireEvent.click(row1);
    const detail = utils.getByTestId("mp-webhook-request-detail-1");
    expect(detail.textContent).toContain("X-Github-Event: pull_request");
    expect(detail.textContent).toContain('{"probe":true}');
    fireEvent.click(row1);
    expect(utils.queryByTestId("mp-webhook-request-detail-1")).toBeNull();
  });

  it("reads the empty face for an endpoint with counters but an empty ring, and the error face when the fetch fails", async () => {
    const { utils } = await renderStatsWindow(
      mkEndpoint({ lastReceivedTs: Date.now() / 1000 - 60, deliveredCount: 1 })
    );
    await utils.findByText(w.requestsEmpty);
    utils.unmount();

    const failed = await renderStatsWindow(
      mkEndpoint({ lastReceivedTs: Date.now() / 1000 - 60, deliveredCount: 1 }),
      [],
      true
    );
    const failedSection = failed.utils.getByTestId("mp-webhook-requests");
    await failed.utils.findByText(w.requestsError);
    expect(failedSection.textContent).toContain(w.requestsError);
  });
});
