import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import "./office.css";

const itemsOf = (menu: HTMLElement | null) =>
  Array.from(menu?.querySelectorAll<HTMLButtonElement>("[role='menuitem']") ?? []);

export interface PressMenuItem {
  key: string;
  label: string;
  icon: ReactNode;
  onSelect: () => void;
  danger?: boolean;
}

/** The long-press menu of one message: a sheet rising from the bottom of the
 * screen over a dimmed backdrop, in the same place whichever message was
 * pressed. Focus starts on the first item; the arrow keys, Home and End move
 * it. A tap on the backdrop, a scroll of the page, Esc, or choosing an item
 * closes it. */
export function ChatMessagePressMenu({
  items,
  onClose,
}: {
  items: PressMenuItem[];
  onClose: () => void;
}) {
  const { t } = useI18n();
  const ref = useRef<HTMLDivElement>(null);
  const close = useRef(onClose);
  close.current = onClose;
  // The sheet opens under a finger that is still down; when it lifts, the
  // browser's follow-up click lands on whatever is now under it — the
  // backdrop, or an item when the pressed message sat low on the screen. Only
  // a tap that STARTED on the backdrop closes it, and only a press that
  // started in the sheet (or a key) chooses an item.
  const downOnBackdrop = useRef(false);
  const downInSheet = useRef(false);
  const [keyed, setKeyed] = useState(false);
  useEscapeLayer(() => close.current(), ref);

  useEffect(() => {
    itemsOf(ref.current)[0]?.focus({ preventScroll: true });
    const onScroll = () => close.current();
    window.addEventListener("scroll", onScroll, true);
    return () => window.removeEventListener("scroll", onScroll, true);
  }, []);

  return createPortal(
    <div
      className="chat__msg-sheet-backdrop"
      data-testid="chat-msg-sheet-backdrop"
      onPointerDown={(e) => {
        downOnBackdrop.current = e.target === e.currentTarget;
      }}
      // That same follow-up click comes with a mousedown, which would take
      // focus off the first item.
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) e.preventDefault();
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget && downOnBackdrop.current) close.current();
        downOnBackdrop.current = false;
      }}
    >
      <div
        ref={ref}
        className={"chat__msg-sheet" + (keyed ? " chat__msg-sheet--keyed" : "")}
        role="menu"
        aria-label={t.chat.messageActions}
        data-testid="chat-msg-menu"
        onPointerDown={() => {
          downInSheet.current = true;
        }}
        onKeyDown={(e) => {
          setKeyed(true);
          const items = itemsOf(ref.current);
          const at = items.indexOf(document.activeElement as HTMLButtonElement);
          const next =
            e.key === "ArrowDown"
              ? (at + 1) % items.length
              : e.key === "ArrowUp"
                ? (Math.max(at, 0) - 1 + items.length) % items.length
                : e.key === "Home"
                  ? 0
                  : e.key === "End"
                    ? items.length - 1
                    : null;
          if (next === null) return;
          e.preventDefault();
          items[next]?.focus();
        }}
      >
        {items.map((item) => (
          <button
            key={item.key}
            type="button"
            role="menuitem"
            className={
              "chat__msg-sheet__item" +
              (item.danger ? " chat__msg-sheet__item--danger" : "")
            }
            data-testid={`chat-msg-menu-${item.key}`}
            onClick={(e) => {
              if (e.detail !== 0 && !downInSheet.current) return;
              close.current();
              item.onSelect();
            }}
          >
            {item.icon}
            <span>{item.label}</span>
          </button>
        ))}
      </div>
    </div>,
    document.body,
  );
}
