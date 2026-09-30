import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type HTMLAttributes,
} from "react";
import { createPortal } from "react-dom";
import "./instant-hint.css";

const GAP = 6;
const EDGE = 8;

/** A span whose explanation shows the moment it is hovered or focused, in place
 * of a native `title` (which the browser holds back for about a second). Each
 * `\n` in `hint` starts a new line. The hint is portalled to <body> with fixed
 * positioning because its hosts sit inside ellipsis / overflow-hidden rows that
 * would clip anything rendered in place. */
export function InstantHint({
  hint,
  children,
  onKeyDown,
  ...rest
}: { hint: string } & Omit<HTMLAttributes<HTMLSpanElement>, "title">) {
  const id = useId();
  const triggerRef = useRef<HTMLSpanElement>(null);
  const hintRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<CSSProperties | null>(null);

  useLayoutEffect(() => {
    if (!open) {
      setPos(null);
      return;
    }
    const trigger = triggerRef.current?.getBoundingClientRect();
    const box = hintRef.current?.getBoundingClientRect();
    if (!trigger || !box) return;
    const below = trigger.bottom + GAP;
    const above = trigger.top - GAP - box.height;
    const fitsBelow = below + box.height <= window.innerHeight - EDGE;
    const top = fitsBelow || above < EDGE ? below : above;
    const maxLeft = Math.max(EDGE, window.innerWidth - EDGE - box.width);
    const left = Math.min(Math.max(EDGE, trigger.left), maxLeft);
    setPos({ top, left });
  }, [open, hint]);

  useEffect(() => {
    if (!open) return;
    // A fixed hint does not follow its trigger when something scrolls.
    const close = () => setOpen(false);
    window.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);
    return () => {
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
    };
  }, [open]);

  return (
    <>
      <span
        {...rest}
        ref={triggerRef}
        tabIndex={0}
        aria-describedby={open ? id : undefined}
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        // The focusable trigger sits inside row-buttons that activate on
        // Enter/Space; letting those keys bubble would open the row. Pointer
        // clicks still reach the row on purpose.
        onKeyDown={(e) => {
          onKeyDown?.(e);
          if (e.key === "Enter" || e.key === " ") e.stopPropagation();
        }}
      >
        {children}
      </span>
      {open &&
        createPortal(
          <div
            ref={hintRef}
            id={id}
            role="tooltip"
            className="instant-hint"
            style={pos ?? { top: 0, left: 0, visibility: "hidden" }}
          >
            {hint.split("\n").map((line, i) => (
              <div key={i}>{line}</div>
            ))}
          </div>,
          document.body,
        )}
    </>
  );
}
