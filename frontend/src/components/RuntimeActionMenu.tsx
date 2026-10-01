import { useEffect, useRef, useState } from "react";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import "./runtime-login.css";

export interface RuntimeActionItem {
  key: string;
  label: string;
  onSelect: () => void;
}

/** The "⋯" action menu beside one runtime's version on a machine row. */
export function RuntimeActionMenu({
  label,
  items,
  testIdPrefix,
}: {
  label: string;
  items: RuntimeActionItem[];
  testIdPrefix: string;
}) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLSpanElement>(null);
  useEscapeLayer(() => setOpen(false), rootRef, open);

  useEffect(() => {
    if (!open) return;
    function onDown(e: MouseEvent) {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  if (items.length === 0) return null;
  return (
    <span className="runtime-menu" ref={rootRef}>
      <button
        type="button"
        className="runtime-menu__trigger"
        aria-label={label}
        title={label}
        aria-haspopup="menu"
        aria-expanded={open}
        data-testid={`${testIdPrefix}-menu`}
        onClick={() => setOpen((o) => !o)}
      >
        <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true" fill="currentColor">
          <circle cx="3" cy="8" r="1.5" />
          <circle cx="8" cy="8" r="1.5" />
          <circle cx="13" cy="8" r="1.5" />
        </svg>
      </button>
      {open && (
        <div className="runtime-menu__pop" role="menu" aria-label={label}>
          {items.map((item) => (
            <button
              key={item.key}
              type="button"
              role="menuitem"
              className="runtime-menu__item"
              data-testid={`${testIdPrefix}-menu-${item.key}`}
              onClick={() => {
                setOpen(false);
                item.onSelect();
              }}
            >
              {item.label}
            </button>
          ))}
        </div>
      )}
    </span>
  );
}
