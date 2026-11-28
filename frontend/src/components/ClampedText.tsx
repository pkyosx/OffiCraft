// ClampedText — a block of text shown at most `lines` lines tall, fading out at
// the bottom, with a 「展開全文」/「收起」 toggle under it. Text that fits shows
// neither the fade nor the toggle. Whether it is open lives in this component
// only, so it starts closed on every mount.

import { useLayoutEffect, useRef, useState, type CSSProperties } from "react";
import { useI18n } from "../i18n";
import { ChevronDownIcon, ChevronUpIcon } from "./icons";
import "./clamped-text.css";

export function ClampedText({
  text,
  lines = 3,
  className = "",
  testId = "clamped-text",
}: {
  text: string;
  lines?: number;
  className?: string;
  testId?: string;
}) {
  const { t } = useI18n();
  const bodyRef = useRef<HTMLSpanElement>(null);
  const [expanded, setExpanded] = useState(false);
  const [overflows, setOverflows] = useState(false);

  // Measured only while clamped: open, the box is as tall as its text and has
  // nothing to compare against. Closing re-runs this, so a width change made
  // while open is caught then.
  useLayoutEffect(() => {
    const el = bodyRef.current;
    if (!el || expanded) return;
    // 1px of slack: a fractional line height rounds clientHeight down.
    const measure = () => setOverflows(el.scrollHeight > el.clientHeight + 1);
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [text, lines, expanded]);

  const label = expanded ? t.common.textShowLess : t.common.textShowAll;
  return (
    <span className={`clamped-text ${className}`.trim()} data-testid={testId}>
      <span
        ref={bodyRef}
        className={[
          "clamped-text__body",
          !expanded && "clamped-text__body--clamped",
          !expanded && overflows && "clamped-text__body--faded",
        ]
          .filter(Boolean)
          .join(" ")}
        style={{ "--clamped-text-lines": lines } as CSSProperties}
        data-testid={`${testId}-body`}
      >
        {text}
      </span>
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
    </span>
  );
}
