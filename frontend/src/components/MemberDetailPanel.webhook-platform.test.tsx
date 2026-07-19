// MemberDetailPanel · webhook 平台驗證區塊 (M4 §2).
//
// Locked here:
//   1. The create form carries a platform dropdown (generic/slack/github,
//      default generic). Choosing slack/github reveals a REQUIRED Signing
//      Secret field + a platform helper; generic hides it entirely.
//   2. Create sends platform + signingSecret through the api client.
//   3. A row NEVER shows the secret value — and (T-069d) no longer carries
//      the constant platform/"已設 secret" wording either; the row head slot
//      belongs to the compact 事件統計 summary.
//
// Uses a small stateful in-memory api mock (create → refetch roundtrip). The
// mock stores the secret out-of-band and only echoes has_signing_secret, so a
// leaked plaintext would be observable in the DOM — the test asserts it isn't.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";
import type {
  WebhookEndpoint,
  WebhookCreateInput,
  WebhookUpdate,
} from "../api/adapter";

// ── stateful webhook store (mirrors the server: secret is write-only) ──
let store: WebhookEndpoint[] = [];
const secretVault = new Map<string, string>();
const createWebhook = vi.fn(
  async (_memberId: string, input: WebhookCreateInput) => {
    const platform = input.platform ?? "generic";
    const secret = input.signingSecret?.trim() ?? "";
    const hasSecret = platform !== "generic" && secret !== "";
    if (hasSecret) secretVault.set(input.endpointId, secret);
    const created: WebhookEndpoint = {
      endpointId: input.endpointId,
      purpose: input.purpose ?? "",
      status: "enabled",
      createdTs: 0,
      token: "mock-token-000000000000",
      platform,
      hasSigningSecret: hasSecret,
      lastReceivedTs: 0,
      deliveredCount: 0,
      droppedCount: 0,
      lastDropReason: "",
    };
    store = [...store, created];
    return { ...created }; // never echo the secret
  }
);
const updateWebhook = vi.fn(
  async (_memberId: string, endpointId: string, patch: WebhookUpdate) => {
    const e = store.find((x) => x.endpointId === endpointId)!;
    if (patch.status !== undefined) e.status = patch.status;
    if (patch.purpose !== undefined) e.purpose = patch.purpose;
    if (patch.signingSecret !== undefined) {
      const s = patch.signingSecret.trim();
      if (s) {
        secretVault.set(endpointId, s);
        e.hasSigningSecret = true;
      }
    }
    return { ...e };
  }
);

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
    patchMember: (_id: string, patch: object) =>
      Promise.resolve({ ...mkMember(), ...(patch as Partial<Member>) }),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
    listWebhooks: () => Promise.resolve(store.map((e) => ({ ...e }))),
    createWebhook: (memberId: string, input: WebhookCreateInput) =>
      createWebhook(memberId, input),
    updateWebhook: (memberId: string, endpointId: string, patch: WebhookUpdate) =>
      updateWebhook(memberId, endpointId, patch),
    deleteWebhook: () => Promise.resolve(),
    subscribeEvents: () => () => {},
  },
}));

function mkMember(over: Partial<Member> = {}): Member {
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
    ...over,
  };
}

function renderPanel() {
  return render(
    <I18nProvider>
      <MemberDetailPanel member={mkMember()} onBack={() => {}} onRename={() => {}} />
    </I18nProvider>
  );
}

beforeEach(() => {
  store = [];
  secretVault.clear();
  createWebhook.mockClear();
  updateWebhook.mockClear();
});

const w = zh.mp.webhook;

describe("MemberDetailPanel · webhook platform + signing secret", () => {
  it("shows the required secret field for slack; hides it for generic", async () => {
    const utils = renderPanel();
    fireEvent.click(utils.getByTestId("mp-webhook-toggle"));
    fireEvent.click(await utils.findByTestId("mp-webhook-add"));

    // Every form row is label-first.
    expect(utils.getByText(w.endpointIdLabel)).toBeTruthy();
    expect(utils.getByText(w.purposeLabel)).toBeTruthy();
    expect(utils.getByText(w.platformLabel)).toBeTruthy();

    // Default generic → no secret field.
    expect(utils.queryByTestId("mp-webhook-secret-input")).toBeNull();

    // Choose slack → the secret field + required hint appear.
    fireEvent.change(utils.getByTestId("mp-webhook-platform-select"), {
      target: { value: "slack" },
    });
    const secret = utils.getByTestId("mp-webhook-secret-input");
    expect(secret).toBeTruthy();
    expect(secret.getAttribute("aria-required")).toBe("true");
    expect(utils.getByText(w.signingSecretLabel)).toBeTruthy();
    expect(utils.getByText(w.helperSlack)).toBeTruthy();

    // Create stays disabled (endpoint id + secret both still blank).
    const createBtn = utils.getByTestId("mp-webhook-create") as HTMLButtonElement;
    expect(createBtn.disabled).toBe(true);

    // Back to generic → the secret field disappears.
    fireEvent.change(utils.getByTestId("mp-webhook-platform-select"), {
      target: { value: "generic" },
    });
    expect(utils.queryByTestId("mp-webhook-secret-input")).toBeNull();
  });

  it("keeps create disabled for slack until endpoint id AND secret are set", async () => {
    const utils = renderPanel();
    fireEvent.click(utils.getByTestId("mp-webhook-toggle"));
    fireEvent.click(await utils.findByTestId("mp-webhook-add"));
    fireEvent.change(utils.getByTestId("mp-webhook-platform-select"), {
      target: { value: "slack" },
    });
    const createBtn = utils.getByTestId("mp-webhook-create") as HTMLButtonElement;

    // endpoint id only → still disabled (missing secret)
    fireEvent.change(
      utils.getByPlaceholderText(w.endpointIdPlaceholder),
      { target: { value: "slack-in" } }
    );
    expect(createBtn.disabled).toBe(true);

    // add the secret → enabled
    fireEvent.change(utils.getByTestId("mp-webhook-secret-input"), {
      target: { value: "shhh-signing-secret" },
    });
    expect(createBtn.disabled).toBe(false);
  });

  it("creates a slack endpoint with platform + secret, then renders the row WITHOUT platform/secret wording and WITHOUT leaking the secret", async () => {
    const utils = renderPanel();
    fireEvent.click(utils.getByTestId("mp-webhook-toggle"));
    fireEvent.click(await utils.findByTestId("mp-webhook-add"));
    fireEvent.change(utils.getByTestId("mp-webhook-platform-select"), {
      target: { value: "slack" },
    });
    fireEvent.change(utils.getByPlaceholderText(w.endpointIdPlaceholder), {
      target: { value: "slack-in" },
    });
    fireEvent.change(utils.getByTestId("mp-webhook-secret-input"), {
      target: { value: "shhh-signing-secret" },
    });
    fireEvent.click(utils.getByTestId("mp-webhook-create"));

    await waitFor(() =>
      expect(createWebhook).toHaveBeenCalledWith("mira", {
        endpointId: "slack-in",
        purpose: "",
        platform: "slack",
        signingSecret: "shhh-signing-secret",
      })
    );

    // T-069d: the row appears WITHOUT the old platform badge / secret-set
    // marker — that head slot now belongs to the compact stats summary.
    await utils.findByTestId("mp-webhook-stats-slack-in");
    expect(utils.queryByTestId("mp-webhook-platform-slack-in")).toBeNull();
    expect(utils.queryByTestId("mp-webhook-secretset-slack-in")).toBeNull();
    expect(utils.container.textContent).not.toContain(w.platformSlack);

    // The secret plaintext is NEVER in the DOM.
    expect(utils.container.textContent).not.toContain("shhh-signing-secret");
  });

  it("creates a generic endpoint with no secret and no secret-set marker", async () => {
    const utils = renderPanel();
    fireEvent.click(utils.getByTestId("mp-webhook-toggle"));
    fireEvent.click(await utils.findByTestId("mp-webhook-add"));
    fireEvent.change(utils.getByPlaceholderText(w.endpointIdPlaceholder), {
      target: { value: "generic-in" },
    });
    fireEvent.click(utils.getByTestId("mp-webhook-create"));

    await waitFor(() =>
      expect(createWebhook).toHaveBeenCalledWith("mira", {
        endpointId: "generic-in",
        purpose: "",
        platform: "generic",
      })
    );
    await utils.findByTestId("mp-webhook-stats-generic-in");
    expect(utils.queryByTestId("mp-webhook-platform-generic-in")).toBeNull();
    expect(utils.queryByTestId("mp-webhook-secretset-generic-in")).toBeNull();
    // generic rows never expose the rotate-secret entry
    expect(utils.queryByTestId("mp-webhook-rotate-generic-in")).toBeNull();
  });

  it("rotates the signing secret through updateWebhook (write-only, never echoed)", async () => {
    store = [
      {
        endpointId: "gh-in",
        purpose: "",
        status: "enabled",
        createdTs: 0,
        token: "mock-token-111111111111",
        platform: "github",
        hasSigningSecret: true,
        lastReceivedTs: 0,
        deliveredCount: 0,
        droppedCount: 0,
        lastDropReason: "",
      },
    ];
    const utils = renderPanel();
    fireEvent.click(utils.getByTestId("mp-webhook-toggle"));

    fireEvent.click(await utils.findByTestId("mp-webhook-rotate-gh-in"));
    fireEvent.change(utils.getByTestId("mp-webhook-rotate-input-gh-in"), {
      target: { value: "rotated-secret-999" },
    });
    fireEvent.click(utils.getByTestId("mp-webhook-rotate-save-gh-in"));

    await waitFor(() =>
      expect(updateWebhook).toHaveBeenCalledWith("mira", "gh-in", {
        signingSecret: "rotated-secret-999",
      })
    );
    // Rotated plaintext is not reflected anywhere in the DOM.
    await waitFor(() =>
      expect(within(utils.container).queryByDisplayValue("rotated-secret-999")).toBeNull()
    );
    expect(utils.container.textContent).not.toContain("rotated-secret-999");
  });
});
