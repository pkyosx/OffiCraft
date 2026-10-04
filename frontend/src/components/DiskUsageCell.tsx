import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import type { Dict } from "../i18n/locales/zh";
import { formatBytes } from "../lib/bytes";
import { formatDuration } from "../lib/duration";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import type { MachineDiskUsageView } from "../types";
import "./disk-usage.css";

const TOP_MEMBERS = 5;
const GAP = 4;
const EDGE = 8;

export interface DiskUsageCellProps {
  /** The machine's `diskUsage`; null or undefined reads as never measured. */
  usage: MachineDiskUsageView | null | undefined;
}

interface Row {
  key: string;
  label: ReactNode;
  value: string;
  sub?: boolean;
  section?: boolean;
}

/** A machine's OffiCraft disk total. Clicking it (not hovering, so it works on
 * a phone) opens the breakdown; a second click, Esc, scrolling or a click
 * elsewhere closes it. The panel is portalled to <body> with fixed positioning
 * because the machine table's `overflow: auto` wrapper would clip it. */
export function DiskUsageCell({ usage }: DiskUsageCellProps) {
  const { t, msg } = useI18n();
  const d = t.monitor.diskUsage;
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<CSSProperties | null>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);

  useEscapeLayer(
    () => {
      setOpen(false);
      triggerRef.current?.focus();
    },
    panelRef,
    open,
  );

  useLayoutEffect(() => {
    if (!open) {
      setPos(null);
      return;
    }
    const trigger = triggerRef.current?.getBoundingClientRect();
    const box = panelRef.current?.getBoundingClientRect();
    if (!trigger || !box) return;
    const below = trigger.bottom + GAP;
    const above = trigger.top - GAP - box.height;
    const fitsBelow = below + box.height <= window.innerHeight - EDGE;
    // Neither side fits on a short window: pin it inside the window instead,
    // where its own scroll (max-height) shows the rest.
    const top = fitsBelow
      ? below
      : above >= EDGE
        ? above
        : Math.max(EDGE, window.innerHeight - EDGE - box.height);
    const maxLeft = Math.max(EDGE, window.innerWidth - EDGE - box.width);
    const left = Math.min(Math.max(EDGE, trigger.left), maxLeft);
    setPos({ top, left });
  }, [open]);

  useEffect(() => {
    if (!open) return;
    function onDown(e: MouseEvent) {
      const target = e.target as Node;
      if (triggerRef.current?.contains(target) || panelRef.current?.contains(target)) return;
      setOpen(false);
    }
    // A fixed panel does not follow its trigger when something else scrolls;
    // scrolling the panel's own rows is not that.
    function onScroll(e: Event) {
      if (e.target instanceof Node && panelRef.current?.contains(e.target)) return;
      setOpen(false);
    }
    const dismiss = () => setOpen(false);
    document.addEventListener("mousedown", onDown);
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", dismiss);
    return () => {
      document.removeEventListener("mousedown", onDown);
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", dismiss);
    };
  }, [open]);

  if (!usage) {
    return (
      <span className="disk-usage__unmeasured" data-testid="disk-usage-unmeasured">
        {d.notMeasured}
      </span>
    );
  }

  const totalText = usage.totalBytes === null ? t.monitor.dash : formatBytes(usage.totalBytes);
  const rows = breakdownRows(usage, d, t.monitor.machineCol, msg.monitorDiskOtherMembers);
  const measuredAt = usage.measuredAt ?? usage.databaseMeasuredAt;
  const measuredText =
    measuredAt === null
      ? null
      : msg.monitorMeasuredAgo(formatDuration(Math.max(0, Date.now() / 1000 - measuredAt)));

  return (
    <span className="disk-usage">
      <button
        ref={triggerRef}
        type="button"
        className="disk-usage__trigger"
        aria-label={`${d.open} ${totalText}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        data-testid="disk-usage-trigger"
        // The cell sits in table rows that may act on a click; this click is
        // only about the panel.
        onClick={(e) => {
          e.stopPropagation();
          setOpen((o) => !o);
        }}
      >
        {totalText}
      </button>
      {open &&
        createPortal(
          <div
            ref={panelRef}
            className="disk-usage__panel"
            role="dialog"
            aria-label={d.open}
            data-testid="disk-usage-panel"
            style={pos ?? { top: 0, left: 0, visibility: "hidden" }}
            // React bubbles portal events through the component tree, so a
            // click in here would otherwise reach the host row.
            onClick={(e) => e.stopPropagation()}
          >
            <ul className="disk-usage__rows">
              {rows.map((r) => (
                <li
                  key={r.key}
                  className={
                    "disk-usage__row" +
                    (r.sub ? " disk-usage__row--sub" : "") +
                    (r.section ? " disk-usage__row--section" : "")
                  }
                  data-testid="disk-usage-row"
                >
                  <span className="disk-usage__label">{r.label}</span>
                  <span className="disk-usage__value">{r.value}</span>
                </li>
              ))}
            </ul>
            {measuredText && (
              <div className="disk-usage__measured" data-testid="disk-usage-measured">
                {measuredText}
              </div>
            )}
          </div>,
          document.body,
        )}
    </span>
  );
}

function breakdownRows(
  u: MachineDiskUsageView,
  d: Dict["monitor"]["diskUsage"],
  col: Dict["monitor"]["machineCol"],
  otherMembers: (count: number) => string,
): Row[] {
  const rows: Row[] = [];
  const push = (key: string, label: ReactNode, bytes: number | null, extra?: Partial<Row>) => {
    if (bytes !== null) rows.push({ key, label, value: formatBytes(bytes), ...extra });
  };
  push("database", d.database, u.databaseBytes);
  push("backups", d.backups, u.backupsBytes);
  push("workspaces", d.workspaces, u.workspaceBytes);

  const members = [...u.members].sort((a, b) => b.totalBytes - a.totalBytes);
  members.slice(0, TOP_MEMBERS).forEach((m, i) => {
    const label = (
      <>
        {m.name ?? m.memberId}
        {m.rosterStatus !== "active" && (
          <span className="disk-usage__left" data-testid="disk-usage-left">
            {d.memberLeft}
          </span>
        )}
      </>
    );
    push(`top:${m.memberId}`, label, m.totalBytes, { section: i === 0 });
  });
  const rest = members.slice(TOP_MEMBERS);
  if (rest.length > 0) {
    push("members-rest", otherMembers(rest.length), rest.reduce((sum, m) => sum + m.totalBytes, 0));
  }

  const conversationAt = rows.length;
  push("conversations", d.conversations, u.conversationBytes);
  push("claude", col.claude, u.claudeConversationBytes, { sub: true });
  push("codex", col.codex, u.codexConversationBytes, { sub: true });
  if (rows[conversationAt]) rows[conversationAt].section = true;

  push("other", d.other, u.otherBytes);
  if (u.diskFreeBytes !== null && u.diskTotalBytes !== null) {
    rows.push({
      key: "disk",
      label: d.disk,
      value: `${formatBytes(u.diskFreeBytes)} / ${formatBytes(u.diskTotalBytes)}`,
      section: true,
    });
  }
  return rows;
}
