import { useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { ChatArea } from "../../src/components/ChatArea";
import "../../src/components/office.css";
import type { Member } from "../../src/types";
import type { ChatMessage } from "../../src/api/adapter";
import { __injectMockChat } from "../../src/api/mock";

const MEMBER = {
  id: "rd",
  name: "OffiCraft RD",
  role: "dev",
  status: "online",
  lifecycle: "online",
  model: "opus",
  effort: "medium",
  kind: "staff",
  desiredMachineId: "",
  machine: null,
  account: null,
  contextPct: null,
  estimatedCost: null,
  bankedCost: null,
  terminalAttachCommand: "",
  refocusSince: null,
  lastOp: "",
  lastOpOk: null,
  lastOpLog: "",
  lastOpAt: null,
  unreadCount: 0,
} as Member;

const noop = () => {};

export function ChatSuggestedRepliesStory({
  replies,
  allHeaderButtons = false,
  name = MEMBER.name,
  thread = [],
  waitingReplies,
  outsource = false,
}: {
  /** Leave out for a peer without the suggested-replies button. */
  replies?: string[];
  allHeaderButtons?: boolean;
  name?: string;
  /** Lands in the mock chat log before mounting, so the thread can be searched. */
  thread?: ChatMessage[];
  /** The 請示卡 jump with this many waiting cards; left out = no jump button. */
  waitingReplies?: number;
  /** An outsource peer: no tasks or role jump, whatever allHeaderButtons says. */
  outsource?: boolean;
}) {
  useState(() => thread.forEach(__injectMockChat));
  const [saved, setSaved] = useState(replies ?? []);
  return (
    <I18nProvider>
      <div style={{ height: 700, display: "flex", flexDirection: "column" }} data-testid="host">
        <ChatArea
          member={outsource ? { ...MEMBER, id: "ow-ct", kind: "outsource", role: "", name } : { ...MEMBER, name }}
          suggestedReplies={saved}
          onSaveSuggestedReplies={replies ? setSaved : undefined}
          onOpenTasks={allHeaderButtons && !outsource ? noop : undefined}
          onOpenRoleSettings={allHeaderButtons && !outsource ? noop : undefined}
          onOpenReplies={waitingReplies === undefined ? undefined : noop}
          waitingReplyCount={waitingReplies}
        />
      </div>
    </I18nProvider>
  );
}
