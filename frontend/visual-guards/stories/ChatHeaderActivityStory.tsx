// CT story (T-492): the chat header row with the member-activity status word,
// built from the real PresenceBadge and MemberActivityWord inside the real
// office grid, beside the same three header buttons ChatArea renders for a
// roster member. A twin header without activity sits under it so the spec can
// compare heights, and a free-standing word gives its natural width.
import { I18nProvider } from "../../src/i18n";
import { PresenceBadge } from "../../src/components/PresenceBadge";
import { MemberActivityWord } from "../../src/components/MemberActivity";
import type { Member, MemberActivity } from "../../src/types";
import "../../src/components/office.css";

function mkMember(roleName: string, activity: MemberActivity | null): Member {
  return {
    id: "m-act",
    name: "Eva Rhapsody Inbox",
    role: "r-custom",
    roleName,
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
    activity,
  };
}

function Header({ member, testId }: { member: Member; testId: string }) {
  return (
    <header className="chat__header chat__header--clickable" data-testid={testId}>
      <span className="avatar" style={{ width: 38, height: 38, flex: "none" }} />
      <div className="chat__header-text">
        <div className="chat__header-name">
          <span>{member.name}</span>
        </div>
        <div className="chat__header-sub">
          <PresenceBadge member={member} />
          <MemberActivityWord activity={member.activity} />
        </div>
      </div>
      <button type="button" className="chat__header-action" aria-label="tasks" />
      <button type="button" className="chat__header-action" aria-label="role" />
      <button type="button" className="chat__gallery-toggle" aria-label="gallery" />
    </header>
  );
}

export function ChatHeaderActivityStory({
  mobile,
  state,
  roleName,
}: {
  mobile: boolean;
  state: MemberActivity["state"];
  roleName: string;
}) {
  const now = Date.now() / 1000;
  const activity: MemberActivity = {
    state,
    busySince: state === "busy" ? now - 300 : null,
    subagents: state === "busy" ? [{ name: "Explore", description: "", startedTs: now - 60 }] : [],
    waits: state === "idle" ? [] : [{ kind: "schedule", description: "看 CI" }],
  };
  return (
    <I18nProvider>
      <div className={mobile ? "office office--mobile" : "office"} style={{ height: 600 }}>
        {!mobile && <aside className="office__members" />}
        <section className="office__chat">
          <div className="chat">
            <Header member={mkMember(roleName, activity)} testId="header-with" />
            <Header member={mkMember(roleName, null)} testId="header-without" />
          </div>
        </section>
      </div>
      <div className="chat__header-sub" style={{ position: "absolute", left: 0, top: 700 }}>
        <span data-testid="free-word">
          <MemberActivityWord activity={activity} />
        </span>
      </div>
    </I18nProvider>
  );
}
