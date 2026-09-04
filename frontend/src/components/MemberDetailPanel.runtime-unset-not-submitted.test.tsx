// MemberDetailPanel · 「沒有主動選過執行環境」不可以被送成一個具體值 (T-ae8b).
//
// WHY this file exists as its own guard, and not as one more case in
// MemberDetailPanel.model-effort.test.tsx: that file is about the model/effort
// editor, and the defect here is not about model or effort at all — it is about
// what rides ALONGSIDE them.
//
// The defect: a member whose runtime was never chosen persists "" (unset), and
// unset is the whole point of this ticket — placement resolves it against the
// host's measured capabilities, so a codex-only machine grows a Codex member.
// A select cannot render nothing, so the settings dialog FILLS the control with
// `claude`. That fill used to be submitted whenever `launchChanged` was true —
// and `launchChanged` is true when the owner edits ONLY the model or ONLY the
// effort. One model tweak therefore wrote a concrete `claude` over the unset
// value and permanently switched that member off auto-resolution, from a dialog
// where the owner never touched the runtime control and was never asked.
//
// The entry is NARROW and that narrowness is load-bearing: accepting the dialog
// with nothing changed does NOT do this — both save paths compute
// `launchChanged` first and send no PATCH at all when it is false. The first
// case below pins that, so a future reader cannot widen the story into "pressing
// confirm nails down your runtime", which is not true.
//
// The three cases are one triple: an omission is only evidence if the same
// harness can be shown to EMIT the key when the owner really does choose. Case 3
// is that positive control — delete the fix and case 2 reddens; delete the fix
// and case 3 still passes, which is what makes case 2 mean something.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";

const patchMember = vi.fn(async (_id: string, patch: object) => ({
  ...mkMember(),
  ...(patch as Partial<Member>),
}));

vi.mock("../api", () => ({
  api: {
    listMachines: () =>
      Promise.resolve([{ machineId: "mach-1", displayName: "Mac", online: true }]),
    patchMember: (id: string, patch: object) => patchMember(id, patch),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
    listWebhooks: () => Promise.resolve([]),
    listScheduledMessages: () => Promise.resolve([]),
    createWebhook: () =>
      Promise.resolve({ endpointId: "", purpose: "", status: "enabled", createdTs: 0, token: "" }),
    updateWebhook: () =>
      Promise.resolve({ endpointId: "", purpose: "", status: "enabled", createdTs: 0, token: "" }),
    deleteWebhook: () => Promise.resolve(),
    subscribeEvents: () => () => {},
  },
}));

/** An UNSET-runtime member: `runtime` is absent, exactly as the adapter reports
 * a member the server stores with "". This is what every member hired after
 * T-ae8b's server change looks like until its first placement resolves. */
function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    name: "Mira",
    role: "assistant",
    status: "offline",
    lifecycle: "offline",
    model: "opus",
    effort: "medium",
    kind: "staff",
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

function renderPanel(over: Partial<Member> = {}) {
  return render(
    <I18nProvider>
      <MemberDetailPanel
        member={mkMember(over)}
        onBack={() => {}}
        onRename={vi.fn()}
      />
    </I18nProvider>,
  );
}

async function openSettings(utils: { getByTestId: (id: string) => HTMLElement }) {
  await waitFor(() =>
    expect(
      (utils.getByTestId("member-action-spawn") as HTMLButtonElement).disabled,
    ).toBe(false),
  );
  fireEvent.click(utils.getByTestId("member-action-spawn"));
}

async function confirmSettings() {
  const confirm = document.querySelector<HTMLButtonElement>(
    ".machine-picker__actions .btn--accent",
  )!;
  await waitFor(() => expect(confirm.disabled).toBe(false));
  fireEvent.click(confirm);
}

beforeEach(() => {
  patchMember.mockClear();
});

describe("MemberDetailPanel · an unchosen runtime is never submitted (T-ae8b)", () => {
  it("the control SHOWS claude for an unset member — the fill this test is about", async () => {
    const utils = renderPanel();
    await openSettings(utils);
    // Stated so the omission below reads as "shown but not sent", not as
    // "the dialog was empty so of course nothing went".
    expect((utils.getByTestId("me-runtime-select") as HTMLSelectElement).value).toBe(
      "claude",
    );
    // ...and accepting it untouched sends NOTHING. The narrow entry, pinned:
    // this is the case a reader is most likely to assume is broken.
    await confirmSettings();
    await waitFor(() => expect(patchMember).not.toHaveBeenCalled());
  });

  it("editing ONLY the model sends a body with NO runtime key — the unset value survives", async () => {
    const utils = renderPanel();
    await openSettings(utils);
    fireEvent.change(utils.getByTestId("me-model-input"), {
      target: { value: "sonnet" },
    });
    await confirmSettings();
    await waitFor(() => expect(patchMember).toHaveBeenCalledTimes(1));

    const [, body] = patchMember.mock.calls[0] as [string, Record<string, unknown>];
    // Two assertions, because they fail for different reasons and a reader
    // needs to know which. The first says the owner's real edit went; the
    // second says the FILL did not ride along with it. An unsupplied field is
    // what leaves the stored runtime untouched — "" cannot be sent instead,
    // ValidRuntime 422s it — so the absence of the key IS the fix.
    expect(body).toEqual({ model: "sonnet", effort: "medium" });
    expect(Object.keys(body)).not.toContain("runtime");
  });

  it("editing ONLY the effort sends a body with NO runtime key either", async () => {
    const utils = renderPanel();
    await openSettings(utils);
    fireEvent.change(utils.getByTestId("me-effort-select"), {
      target: { value: "high" },
    });
    await confirmSettings();
    await waitFor(() => expect(patchMember).toHaveBeenCalledTimes(1));

    const [, body] = patchMember.mock.calls[0] as [string, Record<string, unknown>];
    expect(Object.keys(body)).not.toContain("runtime");
    expect(body).toEqual({ model: "opus", effort: "high" });
  });

  it("POSITIVE CONTROL — actually moving the control DOES send runtime", async () => {
    // Without this the three cases above are satisfied by a harness that can
    // never emit `runtime` at all, which would prove nothing.
    const utils = renderPanel();
    await openSettings(utils);
    fireEvent.change(utils.getByTestId("me-runtime-select"), {
      target: { value: "codex" },
    });
    await confirmSettings();
    await waitFor(() => expect(patchMember).toHaveBeenCalledTimes(1));

    const [, body] = patchMember.mock.calls[0] as [string, Record<string, unknown>];
    expect(body.runtime).toBe("codex");
  });
});
