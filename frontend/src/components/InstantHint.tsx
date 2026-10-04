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
 * of a native `title` (which the browser holds back for about a second). A
 * click or tap pins it open until the next click on the trigger or anywhere
 * outside it, Escape, a scroll or a resize, so phones (no hover) can read it
 * too. Each
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
  // "pinned" is set only by a click, and mouseleave / blur never clear it, so
  // neither the pointer leaving nor the blur from tapping the hint itself drops
  // a hint the reader asked to keep; what closes it is listed in the effect below. A tap fires mouseenter right before click; keeping
  // the two apart is what stops that tap from toggling the hint straight shut.
  const [state, setState] = useState<"closed" | "shown" | "pinned">("closed");
  const open = state !== "closed";
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
    const close = () => setState("closed");
    const closeOutside = (e: MouseEvent) => {
      const target = e.target as Node;
      if (triggerRef.current?.contains(target) || hintRef.current?.contains(target)) return;
      close();
    };
    const closeOnEscape = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    // A fixed hint does not follow its trigger when something scrolls.
    window.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);
    // Capture phase: another hint's trigger stops its click from bubbling, so a
    // bubbling listener would leave this one open beside the next.
    document.addEventListener("click", closeOutside, true);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
      document.removeEventListener("click", closeOutside, true);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [open]);

  const show = () => setState((s) => (s === "pinned" ? s : "shown"));
  const hide = () => setState((s) => (s === "pinned" ? s : "closed"));

  return (
    <>
      <span
        {...rest}
        ref={triggerRef}
        tabIndex={0}
        aria-describedby={open ? id : undefined}
        onMouseEnter={show}
        onMouseLeave={hide}
        onFocus={show}
        onBlur={hide}
        // The trigger sits inside row-buttons that open on click and on
        // Enter/Space; neither may bubble, or using the hint opens the row.
        onClick={(e) => {
          e.stopPropagation();
          setState((s) => (s === "pinned" ? "closed" : "pinned"));
        }}
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
            className={state === "pinned" ? "instant-hint instant-hint--pinned" : "instant-hint"}
            // React bubbles portal events through the trigger's tree, so a tap
            // on the hint would otherwise open the row too.
            onClick={(e) => e.stopPropagation()}
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
