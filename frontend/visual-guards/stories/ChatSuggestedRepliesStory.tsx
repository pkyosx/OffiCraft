import { I18nProvider } from "../../src/i18n";
import { ChatArea } from "../../src/components/ChatArea";
import "../../src/components/office.css";
import type { Member } from "../../src/types";

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
}: {
  replies: string[];
  allHeaderButtons?: boolean;
}) {
  return (
    <I18nProvider>
      <div style={{ height: 700, display: "flex", flexDirection: "column" }} data-testid="host">
        <ChatArea
          member={MEMBER}
          suggestedReplies={replies}
          onSaveSuggestedReplies={noop}
          onOpenTasks={allHeaderButtons ? noop : undefined}
          onOpenRoleSettings={allHeaderButtons ? noop : undefined}
        />
      </div>
    </I18nProvider>
  );
}
