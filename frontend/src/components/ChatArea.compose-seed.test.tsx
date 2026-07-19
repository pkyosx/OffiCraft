// ChatArea compose seed (T-e987) — the 任務卡 負責人/建立者 label routes to
// #office/chat/<peer>/compose/<taskNo>, which OfficePage turns into a
// `draftSeed` of "[<taskNo>] ". Locked here:
//   • the seed prefills an EMPTY composer once;
//   • it never clobbers a draft the owner is already typing.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";

vi.mock("../hooks/useChat", () => ({
  useChat: () => ({ messages: [], messagesPeer: "m1", send: vi.fn() }),
}));

const member: Member = {
  id: "m1",
  memberId: "m1",
  name: "Mira",
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
  tmuxSession: "member-m1",
  refocusSince: null,
  lastOp: "",
  lastOpOk: null,
  lastOpLog: "",
  lastOpAt: null,
  unreadCount: 0,
};

function renderChat(props: { draftSeed?: string } = {}) {
  const utils = render(
    <I18nProvider>
      <ChatArea member={member} draftSeed={props.draftSeed} />
    </I18nProvider>
  );
  const input = utils.container.querySelector(
    ".chat__input"
  ) as HTMLTextAreaElement;
  return { input };
}

describe("ChatArea compose seed", () => {
  it("prefills an empty composer with the task prefix", () => {
    const { input } = renderChat({ draftSeed: "[T-7d40] " });
    expect(input.value).toBe("[T-7d40] ");
  });

  it("does not clobber a draft the owner has already typed", () => {
    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea member={member} />
      </I18nProvider>
    );
    const box = container.querySelector(".chat__input") as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: "已經在打字" } });
    rerender(
      <I18nProvider>
        <ChatArea member={member} draftSeed="[T-7d40] " />
      </I18nProvider>
    );
    expect(box.value).toBe("已經在打字");
  });
});
