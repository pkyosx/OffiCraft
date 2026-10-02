import {
  type ReactNode,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
} from "react";
import { createPortal } from "react-dom";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { ChevronDownIcon } from "./icons";
import "./runtime-login.css";

/* The menu overlaps the trigger's border by one pixel so the two read as one
 * attached control with a single hairline between them. */
const OVERLAP = 1;
const EDGE = 8;

export interface RuntimeActionItem {
  key: string;
  label: string;
  icon?: ReactNode;
  onSelect: () => void;
}

/** One runtime's version on a machine row, made into the trigger of that
 * runtime's action menu: `children` (the version and its chips) sit inside a
 * quiet pill with a chevron. `label` is the trigger's accessible name and must
 * name the runtime, since the visible text alone does not say which column it
 * is in. The popup is portalled to <body> with fixed positioning (the
 * InstantHint pattern): the machine table sits in an `overflow: auto` wrapper
 * that clips anything rendered in place, and a one-row table leaves no room
 * below. */
export function RuntimeActionMenu({
  label,
  items,
  testIdPrefix,
  children,
}: {
  label: string;
  items: RuntimeActionItem[];
  testIdPrefix: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<CSSProperties | null>(null);
  const [placement, setPlacement] = useState<"below" | "above">("below");
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popRef = useRef<HTMLDivElement>(null);
  const close = (refocus: boolean) => {
    setOpen(false);
    if (refocus) triggerRef.current?.focus();
  };
  useEscapeLayer(() => close(true), popRef, open);

  useLayoutEffect(() => {
    if (!open) {
      setPos(null);
      return;
    }
    const trigger = triggerRef.current?.getBoundingClientRect();
    const box = popRef.current?.getBoundingClientRect();
    if (!trigger || !box) return;
    const width = Math.max(box.width, trigger.width);
    const below = trigger.bottom - OVERLAP;
    const above = trigger.top + OVERLAP - box.height;
    const fitsBelow = below + box.height <= window.innerHeight - EDGE;
    const goBelow = fitsBelow || above < EDGE;
    const maxLeft = Math.max(EDGE, window.innerWidth - EDGE - width);
    const left = Math.min(Math.max(EDGE, trigger.left), maxLeft);
    setPlacement(goBelow ? "below" : "above");
    setPos({ top: goBelow ? below : above, left, minWidth: trigger.width });
  }, [open]);

  useEffect(() => {
    if (!open) return;
    function onDown(e: MouseEvent) {
      const target = e.target as Node;
      if (triggerRef.current?.contains(target) || popRef.current?.contains(target)) return;
      setOpen(false);
    }
    // A fixed popup does not follow its trigger when something scrolls.
    const dismiss = () => setOpen(false);
    document.addEventListener("mousedown", onDown);
    window.addEventListener("scroll", dismiss, true);
    window.addEventListener("resize", dismiss);
    return () => {
      document.removeEventListener("mousedown", onDown);
      window.removeEventListener("scroll", dismiss, true);
      window.removeEventListener("resize", dismiss);
    };
  }, [open]);

  useEffect(() => {
    if (open && pos) popRef.current?.querySelector<HTMLElement>("[role='menuitem']")?.focus();
  }, [open, pos]);

  if (items.length === 0) return <>{children}</>;
  return (
    <span className="runtime-menu">
      <button
        ref={triggerRef}
        type="button"
        className="runtime-menu__trigger"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        data-placement={open && pos ? placement : undefined}
        data-testid={`${testIdPrefix}-menu`}
        onClick={() => setOpen((o) => !o)}
        onKeyDown={(e) => {
          if (e.key !== "ArrowDown" || open) return;
          e.preventDefault();
          setOpen(true);
        }}
      >
        {children}
        <span className="runtime-menu__chevron" aria-hidden="true">
          <ChevronDownIcon size={12} />
        </span>
      </button>
      {open &&
        createPortal(
          <div
            ref={popRef}
            className="runtime-menu__pop"
            role="menu"
            aria-label={label}
            data-placement={placement}
            data-testid={`${testIdPrefix}-menu-pop`}
            style={pos ?? { top: 0, left: 0, visibility: "hidden" }}
            onKeyDown={(e) => {
              if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
              e.preventDefault();
              const all = Array.from(
                popRef.current?.querySelectorAll<HTMLElement>("[role='menuitem']") ?? []
              );
              const at = all.indexOf(document.activeElement as HTMLElement);
              const step = e.key === "ArrowDown" ? 1 : -1;
              all[(at + step + all.length) % all.length]?.focus();
            }}
          >
            {items.map((item) => (
              <button
                key={item.key}
                type="button"
                role="menuitem"
                className="runtime-menu__item"
                data-testid={`${testIdPrefix}-menu-${item.key}`}
                onClick={() => {
                  close(false);
                  item.onSelect();
                }}
              >
                {item.icon && (
                  <span className="runtime-menu__icon" aria-hidden="true">
                    {item.icon}
                  </span>
                )}
                {item.label}
              </button>
            ))}
          </div>,
          document.body
        )}
    </span>
  );
}
