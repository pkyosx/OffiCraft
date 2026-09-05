interface IconProps {
  size?: number;
  className?: string;
  /** Opt-in only. Icons in this app are NOT hidden from the accessibility tree
   * by default — that is a pre-existing, app-wide question and this is not the
   * place to answer it for everyone. The one caller that passes it (T-4e95's
   * quote row) sits inside an element that already carries the whole meaning as
   * an aria-label, so its glyph would land in the tree as an unnamed `img` node
   * saying nothing. Every other icon keeps the behaviour it has today. */
  "aria-hidden"?: boolean | "true";
}

const base = (size: number) => ({
  width: size,
  height: size,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 2,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
});

export function LogoMark({ size = 20 }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none">
      <circle cx="12" cy="5" r="2.4" fill="currentColor" />
      <circle cx="6" cy="16" r="2.4" fill="currentColor" />
      <circle cx="18" cy="16" r="2.4" fill="currentColor" />
      <path
        d="M12 7v3M12 10 7 14M12 10l5 4"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
      />
    </svg>
  );
}

export function PencilIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M12 20h9" />
      <path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z" />
    </svg>
  );
}

export function RefreshIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M21 12a9 9 0 1 1-2.64-6.36" />
      <path d="M21 3v6h-6" />
    </svg>
  );
}

export function BellIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9" />
      <path d="M10 21h4" />
    </svg>
  );
}

export function BellOffIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="m3 3 18 18" />
      <path d="M6.26 6.26A6 6 0 0 0 6 8c0 7-3 7-3 9h13" />
      <path d="M18 8a6 6 0 0 0-8.43-5.49" />
      <path d="M10 21h4" />
    </svg>
  );
}

export function GearIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z" />
    </svg>
  );
}

export function ChevronDownIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="m6 9 6 6 6-6" />
    </svg>
  );
}

/** T-48: the jump-to-latest arrow. A full arrow (shaft + head), not the bare
 * chevron above — inside a 32px circle a lone chevron reads as "expand", which
 * is what every other chevron in this cockpit means. */
export function ArrowDownIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M12 5v14" />
      <path d="m19 12-7 7-7-7" />
    </svg>
  );
}

export function ChevronRightIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="m9 18 6-6-6-6" />
    </svg>
  );
}

export function ChevronLeftIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="m15 18-6-6 6-6" />
    </svg>
  );
}

export function CopyIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <rect x="9" y="9" width="13" height="13" rx="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </svg>
  );
}

export function CheckIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M20 6 9 17l-5-5" />
    </svg>
  );
}

export function CloseIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M18 6 6 18" />
      <path d="m6 6 12 12" />
    </svg>
  );
}

export function FileTextIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z" />
      <path d="M14 2v6h6" />
      <path d="M8 13h8M8 17h6" />
    </svg>
  );
}

/** Photo/gallery glyph (lucide "image"): the chat header's file-and-image
 * gallery toggle (M2-3). */
export function ImageIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <rect x="3" y="3" width="18" height="18" rx="2" ry="2" />
      <circle cx="9" cy="9" r="2" />
      <path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21" />
    </svg>
  );
}

export function LayersIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="m12 2 9 5-9 5-9-5 9-5Z" />
      <path d="m3 12 9 5 9-5" />
      <path d="m3 17 9 5 9-5" />
    </svg>
  );
}

export function DownloadIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
      <path d="M7 10l5 5 5-5" />
      <path d="M12 15V3" />
    </svg>
  );
}

export function GlobeIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18" />
      <path d="M12 3a14 14 0 0 1 0 18 14 14 0 0 1 0-18Z" />
    </svg>
  );
}

export function UsersIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
      <circle cx="9" cy="7" r="4" />
      <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
      <path d="M16 3.13a4 4 0 0 1 0 7.75" />
    </svg>
  );
}

export function UserIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
      <circle cx="12" cy="7" r="4" />
    </svg>
  );
}

/** Person + gear — 角色設定 (T-dfae, owner 2026-07-17: 「可能是一個人加一個齒
 * 輪的圖案？」). A UserIcon whose torso is cut back to the left half so a small
 * gear can sit in the bottom-right corner without the two glyphs colliding.
 * The gear is a 6-tooth star-of-lines + hub rather than GearIcon's single
 * outline path: at this size GearIcon's fine detail muddies into a blob, and
 * this control has to read as "role settings" at a glance — the whole reason
 * owner rejected the plain ⚙. */
export function UserGearIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      {/* head + left-biased shoulders (the right shoulder stops early to clear
          the gear) */}
      <circle cx="9" cy="7" r="4" />
      <path d="M3 21v-2a4 4 0 0 1 4-4h5" />
      {/* gear: hub + six spokes, bottom-right */}
      <circle cx="17.5" cy="17.5" r="2.5" />
      <path d="M17.5 13v1.5M17.5 20.5V22M13 17.5h1.5M20.5 17.5H22" />
      <path d="m14.3 14.3 1.1 1.1M19.6 19.6l1.1 1.1M20.7 14.3l-1.1 1.1M15.4 19.6l-1.1 1.1" />
    </svg>
  );
}

export function OfficeIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M3 21h18" />
      <path d="M5 21V7l8-4v18" />
      <path d="M19 21V11l-6-4" />
      <path d="M9 9v.01M9 12v.01M9 15v.01M9 18v.01" />
    </svg>
  );
}

export function InboxIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <polyline points="22 12 16 12 14 15 10 15 8 12 2 12" />
      <path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z" />
    </svg>
  );
}

/** 任務 nav icon — a checked square (mirrors the mockup's ☑ tab glyph). */
export function TasksIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M21 11v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11" />
      <polyline points="9 11 12 14 22 4" />
    </svg>
  );
}

export function MonitorIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
    </svg>
  );
}

/** Open-book glyph (lucide "book-open") — the 傳承 nav tab (T-33). A book is
 * the one glyph in this set that reads as「寫下來留給下一個人」, which is what the
 * tab holds. */
export function BookIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M12 7v14" />
      <path d="M3 18a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h5a4 4 0 0 1 4 4 4 4 0 0 1 4-4h5a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1h-6a3 3 0 0 0-3 3 3 3 0 0 0-3-3Z" />
    </svg>
  );
}

/** Briefcase glyph — a decorative 外包 marker in the task-manual assignee /
 * outsource-cap UI. NOT an avatar: outsource identity now renders the active
 * theme's role-level Avatar (kind="outsource") at every avatar site. */
export function BriefcaseIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <rect x="2" y="7" width="20" height="14" rx="2" />
      <path d="M16 21V5a2 2 0 0 0-2-2h-4a2 2 0 0 0-2 2v16" />
    </svg>
  );
}

/** Person-plus glyph — the "add a role / hire" cue used by BOTH the 正職
 * group header (jump to 角色誌) and the 外包 panel header (open the cap
 * popover). Owner ordering (2026-07-14): the PLUS sits FIRST (left), the
 * person SECOND (right) — a mirrored lucide "user-plus". A single shared
 * component so the two call sites stay pixel-identical. */
export function PersonPlusIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M8 21v-2a4 4 0 0 1 4-4h6a4 4 0 0 1 4 4v2" />
      <circle cx="15" cy="7" r="4" />
      <line x1="5" y1="8" x2="5" y2="14" />
      <line x1="2" y1="11" x2="8" y2="11" />
    </svg>
  );
}

/** ⇄ two-way swap arrows (lucide "arrow-right-left") — the 任務卡 負責人 row's
 * 轉派 (reassign) cue. Two stacked horizontal arrows pointing opposite ways
 * read as "swap ownership", where PersonPlusIcon (add-a-person) read as
 * "invite a new person". T-160e icon recut, owner review 2026-07-18. */
export function SwapIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="m16 3 4 4-4 4" />
      <path d="M20 7H4" />
      <path d="m8 21-4-4 4-4" />
      <path d="M4 17h16" />
    </svg>
  );
}

/** ↗ external-link glyph — the task key badge's "value is a URL" cue. */
export function ExternalLinkIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
      <polyline points="15 3 21 3 21 9" />
      <line x1="10" y1="14" x2="21" y2="3" />
    </svg>
  );
}

/** Eye glyph — the 預覽 (in-cockpit preview) action on a .md attachment /
 * file artifact (T-a1c4 / T-3dc5), distinct from the download action. */
export function EyeIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  );
}

/** Corner-arrows glyph — the 放大閱讀 action on an incoming chat bubble: open
 * this message body in the full-view overlay. Distinct from EyeIcon (預覽 a
 * FILE): nothing is being previewed here, the same text is being re-laid-out
 * with room to read. */
export function ExpandIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <polyline points="14 4 20 4 20 10" />
      <polyline points="10 20 4 20 4 14" />
      <line x1="20" y1="4" x2="13" y2="11" />
      <line x1="4" y1="20" x2="11" y2="13" />
    </svg>
  );
}

/** Reply arrow — the 「回覆這則」 entry on a chat row, and the marker on the
 * quote line above a message that replies to another (T-4e95). A left-turning
 * arrow rather than a speech bubble: the action is aiming at an EXISTING
 * message, not starting a new one, and ChatBubbleIcon already means the latter. */
export function ReplyIcon({
  size = 16,
  className,
  "aria-hidden": ariaHidden,
}: IconProps) {
  return (
    <svg {...base(size)} className={className} aria-hidden={ariaHidden}>
      <polyline points="9 17 4 12 9 7" />
      <path d="M20 18v-2a4 4 0 0 0-4-4H4" />
    </svg>
  );
}

/** Clock glyph — step 耗時 stamps + the 等待外部 reason row. */
export function ClockIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <circle cx="12" cy="12" r="9" />
      <polyline points="12 7 12 12 15 14" />
    </svg>
  );
}

export function BulbIcon({ size = 16, className }: IconProps) {
  // 學習經驗 (manual hub entry card) — a simple lightbulb.
  return (
    <svg {...base(size)} className={className}>
      <path d="M9 18h6" />
      <path d="M10 21h4" />
      <path d="M12 3a6 6 0 0 0-4 10.5c.8.8 1.3 1.5 1.5 2.5h5c.2-1 .7-1.7 1.5-2.5A6 6 0 0 0 12 3Z" />
    </svg>
  );
}

export function BoltIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="currentColor"
      className={className}
    >
      <path d="M13 2 3 14h7l-1 8 10-12h-7l1-8Z" />
    </svg>
  );
}

export function LogOutIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
      <path d="m16 17 5-5-5-5" />
      <path d="M21 12H9" />
    </svg>
  );
}

export function MoonIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79Z" />
    </svg>
  );
}

export function PaperclipIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M21.44 11.05 12.25 20.24a5 5 0 0 1-7.07-7.07l9.19-9.19a3 3 0 0 1 4.24 4.24l-9.2 9.19a1 1 0 0 1-1.41-1.41l8.49-8.49" />
    </svg>
  );
}

export function SmileIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <circle cx="12" cy="12" r="9" />
      <path d="M8 14s1.5 2 4 2 4-2 4-2" />
      <path d="M9 9h.01M15 9h.01" />
    </svg>
  );
}

/** Trash-can glyph (lucide "trash-2"): destructive row actions, e.g. the
 * Settings 角色定義 custom-role delete button. */
export function TrashIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M3 6h18" />
      <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" />
      <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
      <path d="M10 11v6" />
      <path d="M14 11v6" />
    </svg>
  );
}

/** Speech-bubble glyph (lucide "message-square") — the 任務卡 負責人/建立者
 * rows' "message this person" cue (SendIcon/InboxIcon read wrong here). */
export function ChatBubbleIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
    </svg>
  );
}

export function SendIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="m22 2-7 20-4-9-9-4Z" />
      <path d="M22 2 11 13" />
    </svg>
  );
}

/** Database-cylinder glyph (lucide "database") — the backup-health indicator's
 * HEALTHY / UNKNOWN face (T-da06). The colour, not the shape, carries the
 * verdict; the shape says "this control is about the database backup". */
export function DatabaseIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <ellipse cx="12" cy="5" rx="9" ry="3" />
      <path d="M3 5v14c0 1.66 4.03 3 9 3s9-1.34 9-3V5" />
      <path d="M3 12c0 1.66 4.03 3 9 3s9-1.34 9-3" />
    </svg>
  );
}

/** Triangle-exclamation glyph (lucide "alert-triangle") — the backup-health
 * indicator's UNHEALTHY face (T-da06). A DIFFERENT SHAPE, not merely a
 * different colour: colour alone is not a signal a colour-blind reader can
 * take, and this is the one indicator whose whole job is to be noticed. */
export function AlertTriangleIcon({ size = 16, className }: IconProps) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z" />
      <path d="M12 9v4" />
      <path d="M12 17h.01" />
    </svg>
  );
}
