import { Avatar } from "./Avatar";
import { ChatBubbleIcon } from "./icons";
import type { AvatarKind } from "../lib/themeBundle";
import "./card-meta.css";

type ChipAvatar = { kind: AvatarKind; src?: string } | null;

/** A person on a card's label/value row (負責人、建立者、前任、開卡人、撰寫人):
 * avatar and name. The CT guard (MemberChatChipStory) mounts this component. */
export function MemberChip({
  name,
  avatar,
  testId,
  nameTestId,
}: {
  name: string;
  avatar: ChipAvatar;
  testId?: string;
  nameTestId?: string;
}) {
  return (
    <span className="card-meta__chip" data-testid={testId}>
      {avatar ? <Avatar size={18} kind={avatar.kind} src={avatar.src} /> : null}
      <span className="card-meta__chip-name" data-testid={nameTestId} title={name}>
        {name}
      </span>
    </span>
  );
}

/** The same chip for someone who has a chat room: a click opens it, and the
 * chat bubble after the name says so. Every member chip that opens a chat is
 * this one, on every card (owner: if it goes to the chat room, they all show it). */
export function MemberChatChip({
  name,
  avatar,
  label,
  onOpen,
  testId,
  nameTestId,
}: {
  name: string;
  avatar: ChipAvatar;
  /** The button's name and tooltip, e.g. 傳訊息給負責人. */
  label: string;
  onOpen: () => void;
  testId?: string;
  nameTestId?: string;
}) {
  return (
    <button
      type="button"
      className="card-meta__chip card-meta__chip--link"
      title={label}
      aria-label={label}
      data-testid={testId}
      onClick={onOpen}
    >
      {avatar ? <Avatar size={18} kind={avatar.kind} src={avatar.src} /> : null}
      <span className="card-meta__chip-name" data-testid={nameTestId} title={name}>
        {name}
      </span>
      <ChatBubbleIcon size={13} />
    </button>
  );
}
