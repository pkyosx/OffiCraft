// ClampedText — a block of text shown at most `lines` lines tall, fading out at
// the bottom, with a 「展開全文」/「收起」 toggle under it. Text that fits shows
// neither the fade nor the toggle. Whether it is open lives in this component
// only, so it starts closed on every mount.
//
// `children` in place of `text` clamps rendered blocks (Markdown) by height:
// `lines` times the line height the caller sets in --clamped-text-line-height.
//
// `onLineCount` reports the full text's height in lines at the current width,
// whether clamped or open; it is not called until the browser has laid it out.

import { useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { useI18n } from "../i18n";
import { ChevronDownIcon, ChevronUpIcon } from "./icons";
import "./clamped-text.css";

type ClampedTextProps = {
  lines?: number;
  className?: string;
  testId?: string;
  onLineCount?: (lines: number) => void;
} & ({ text: string; children?: never } | { text?: never; children: ReactNode });

export function ClampedText({
  text,
  children,
  lines = 3,
  className = "",
  testId = "clamped-text",
  onLineCount,
}: ClampedTextProps) {
  const { t } = useI18n();
  const bodyRef = useRef<HTMLDivElement>(null);
  const rich = text === undefined;
  const [expanded, setExpanded] = useState(false);
  const [overflows, setOverflows] = useState(false);

  // Open, the box is as tall as its text, so overflow can only be judged
  // against the clamp height; text that has shrunk under it drops 收起.
  useLayoutEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    const measure = () => {
      const full = el.scrollHeight;
      const lineHeight = lineHeightPx(el);
      if (onLineCount && full > 0 && lineHeight > 0) {
        onLineCount(Math.max(1, Math.round(full / lineHeight)));
      }
      // 1px of slack: a fractional line height rounds clientHeight down.
      if (!expanded) {
        setOverflows(full > el.clientHeight + 1);
      } else if (lineHeight > 0 && full <= lines * lineHeight + 1) {
        setOverflows(false);
        setExpanded(false);
      }
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [text, children, lines, expanded, onLineCount]);

  const label = expanded ? t.common.textShowLess : t.common.textShowAll;
  return (
    <div className={`clamped-text ${className}`.trim()} data-testid={testId}>
      <div
        ref={bodyRef}
        className={[
          "clamped-text__body",
          rich && "clamped-text__body--rich",
          !expanded && "clamped-text__body--clamped",
          !expanded && overflows && "clamped-text__body--faded",
        ]
          .filter(Boolean)
          .join(" ")}
        style={{ "--clamped-text-lines": lines } as CSSProperties}
        data-testid={`${testId}-body`}
      >
        {rich ? children : text}
      </div>
      {overflows && (
        <button
          type="button"
          className="clamped-text__toggle"
          aria-expanded={expanded}
          data-testid={`${testId}-toggle`}
          onClick={() => setExpanded((v) => !v)}
        >
          {label}
          {expanded ? <ChevronUpIcon size={13} /> : <ChevronDownIcon size={13} />}
        </button>
      )}
    </div>
  );
}

function lineHeightPx(el: HTMLElement): number {
  const style = getComputedStyle(el);
  const custom = style.getPropertyValue("--clamped-text-line-height").trim();
  if (custom.endsWith("em")) return parseFloat(custom) * parseFloat(style.fontSize);
  if (custom.endsWith("px")) return parseFloat(custom);
  return parseFloat(style.lineHeight);
}
