// The id chip that copies its id. Any id a click jumps from is a TaskIdChip
// (TaskChips.tsx); an id nothing happens on carries no icon at all.

import { useEffect, useRef, useState } from "react";
import { copyText } from "../lib/clipboard";
import { CheckIcon, CopyIcon } from "./icons";
import "./id-chip.css";

/** How every id chip writes its id: with a leading 「#」 (owner). The copy and
 * the link carry the bare id. */
export function idChipText(id: string): string {
  return `#${id}`;
}

/** The copy icon leads the chip; a successful copy swaps it for a check and,
 * given `copiedLabel`, shows that word after the id for a moment. Nothing is
 * shown when the clipboard refuses the write. `value` is what is copied, which
 * may be longer than the `text` on the chip, which is written after a 「#」. */
export function CopyIdChip({
  value,
  text,
  label,
  copiedLabel,
  className,
  testId,
  copiedTestId,
}: {
  value: string;
  text: string;
  label: string;
  copiedLabel?: string;
  /** Extra classes for where the chip sits; its look is .id-chip's. */
  className?: string;
  testId: string;
  copiedTestId?: string;
}) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number | null>(null);
  useEffect(
    () => () => {
      if (timer.current != null) window.clearTimeout(timer.current);
    },
    [],
  );
  async function copy() {
    if (!(await copyText(value))) return;
    setCopied(true);
    if (timer.current != null) window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setCopied(false), 1600);
  }
  return (
    <button
      type="button"
      className={className ? `id-chip ${className}` : "id-chip"}
      data-testid={testId}
      aria-label={label}
      title={label}
      onClick={() => void copy()}
    >
      {copied ? <CheckIcon size={13} /> : <CopyIcon size={13} />}
      {idChipText(text)}
      {copied && copiedLabel && (
        <span className="id-chip__copied" role="status" data-testid={copiedTestId}>
          {copiedLabel}
        </span>
      )}
    </button>
  );
}
