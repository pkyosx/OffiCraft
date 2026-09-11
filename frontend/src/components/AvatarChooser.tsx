import { useEffect, useRef, useState } from "react";
import { useActiveAvatarPools } from "../i18n";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import type { PoolAvatarKind } from "../lib/themeBundle";
// This component renders the .avatar-chooser* block, so it imports that block's
// stylesheet ITSELF (T-7526). Free-riding on a parent's transitive import
// breaks the day the last indirect importer goes away, and neither tsc nor
// jsdom can see the link between a class string and a stylesheet.
import "./member-detail.css";

interface AvatarChooserProps {
  /** The icon id this actor chose in the ACTIVE theme; null when it never did. */
  value: string | null | undefined;
  kind: PoolAvatarKind;
  /** Persist an explicit choice. The id is stable, never a pool position. */
  onSave: (iconId: string) => Promise<void>;
  label: string;
  changeLabel: string;
  dialogTitle: string;
  closeLabel: string;
  emptyLabel: string;
  brokenLabel: string;
  savingLabel: string;
  errorLabel: string;
}

/**
 * Compact by default, expanded only on request.
 *
 * 🔴 The row shows ONE image — the current choice — plus a control that opens
 * the full pool. It deliberately does NOT lay every option out inline: the
 * owner ran that shape in a trial and a pool of any size stretched each roster
 * row until the member list was unreadable (owner 2026-08-12). Anything that
 * puts the whole pool back into the row reintroduces exactly that.
 *
 * The pool is addressed by STABLE ID, not by position, so removing an image
 * cannot silently rebind this member to a different one.
 */
export function AvatarChooser({
  value,
  kind,
  onSave,
  label,
  changeLabel,
  dialogTitle,
  closeLabel,
  emptyLabel,
  brokenLabel,
  savingLabel,
  errorLabel,
}: AvatarChooserProps) {
  const pools = useActiveAvatarPools();
  const pool = pools?.[kind] ?? [];
  const [confirmed, setConfirmed] = useState<string | null>(value ?? null);
  const [open, setOpen] = useState(false);
  const [status, setStatus] = useState<"idle" | "saving" | "error">("idle");
  const [broken, setBroken] = useState<Record<string, boolean>>({});
  // Focus returns to the opener AFTER the close has rendered. Calling focus()
  // inline does nothing: the opener is disabled while the save is in flight,
  // and a disabled element cannot take focus.
  const restoreFocus = useRef(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const openerRef = useRef<HTMLButtonElement>(null);
  const titleId = `avatar-chooser-${kind}-title`;

  useEffect(() => {
    setConfirmed(value ?? null);
    setStatus("idle");
  }, [value]);

  // Esc goes through the shared layer stack, never a listener of our own:
  // `lib/escapeLayers.ts` is the ONLY module allowed to bind window keydown
  // (see frontend/CLAUDE.md). The ref is the surface root, so nesting — not
  // registration order — decides whether Esc reaches us.
  useEscapeLayer(() => setOpen(false), rootRef);

  useEffect(() => {
    if (open) {
      dialogRef.current
        ?.querySelector<HTMLElement>('button:not([disabled])')
        ?.focus();
      return;
    }
    if (restoreFocus.current) {
      restoreFocus.current = false;
      openerRef.current?.focus();
    }
  }, [open]);

  // An empty pool renders an EXPLANATION, never nothing. A control that
  // vanishes reads as a broken cockpit; the owner asked for the reason to be
  // visible instead.
  if (pool.length === 0) {
    return (
      <div className="avatar-chooser" ref={rootRef}>
        <div className="avatar-chooser__label">{label}</div>
        <p className="avatar-chooser__empty">{emptyLabel}</p>
      </div>
    );
  }

  // Resolve the choice against the LIVE pool. A chosen id whose image was
  // removed falls back to the first image — the same thing a member who never
  // chose sees — and never to "whatever sits at that position now".
  const current = pool.find((item) => item.id === confirmed) ?? pool[0];
  const currentIndex = pool.indexOf(current);

  async function select(iconId: string) {
    if (status === "saving") return;
    setStatus("saving");
    try {
      await onSave(iconId);
      setConfirmed(iconId);
      setStatus("idle");
      restoreFocus.current = true;
      setOpen(false);
    } catch {
      setStatus("error");
    }
  }

  function preview(item: { id?: string; image: string }, index: number) {
    const key = item.id ?? String(index);
    if (broken[key]) {
      return (
        <span className="avatar-chooser__broken" role="img" aria-label={brokenLabel}>
          !
        </span>
      );
    }
    return (
      <img
        src={item.image}
        alt=""
        aria-hidden="true"
        draggable={false}
        onError={() => setBroken((prev) => ({ ...prev, [key]: true }))}
      />
    );
  }

  function onTabKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.key !== "Tab") return;
    const items = Array.from(
      dialogRef.current?.querySelectorAll<HTMLElement>('button:not([disabled])') ?? [],
    );
    if (items.length === 0) return;
    const first = items[0];
    const last = items[items.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  return (
    <div className="avatar-chooser" ref={rootRef}>
      <div className="avatar-chooser__label" id={`${titleId}-label`}>
        {label}
      </div>
      <div className="avatar-chooser__row">
        <span className="avatar-chooser__current">
          {preview(current, currentIndex)}
        </span>
        <button
          ref={openerRef}
          type="button"
          className="doc-btn avatar-chooser__open"
          aria-haspopup="dialog"
          aria-expanded={open}
          disabled={status === "saving"}
          onClick={() => setOpen((prev) => !prev)}
        >
          {changeLabel}
        </button>
      </div>
      {status === "saving" && (
        <span className="avatar-chooser__status" role="status">
          {savingLabel}
        </span>
      )}
      {status === "error" && (
        <span className="avatar-chooser__error" role="alert">
          {errorLabel}
        </span>
      )}
      {open && (
        <div
          className="avatar-chooser__dialog"
          role="dialog"
          aria-modal="false"
          aria-labelledby={titleId}
          onKeyDown={onTabKeyDown}
        >
          <div ref={dialogRef} className="avatar-chooser__dialog-box">
            <div className="avatar-chooser__dialog-header">
              <h3 id={titleId} className="avatar-chooser__dialog-title">
                {dialogTitle}
              </h3>
              <button
                type="button"
                className="doc-btn"
                aria-label={`${closeLabel} ${dialogTitle}`}
                onClick={() => {
                  restoreFocus.current = true;
                  setOpen(false);
                }}
              >
                ×
              </button>
            </div>
            <div
              className="avatar-chooser__choices"
              role="radiogroup"
              aria-label={dialogTitle}
              aria-busy={status === "saving"}
            >
              {pool.map((item, index) => {
                const iconId = item.id;
                const checked = item === current;
                return (
                  <button
                    key={iconId ?? `${kind}-${index}`}
                    type="button"
                    role="radio"
                    aria-checked={checked}
                    aria-label={`${label} ${index + 1}`}
                    className={`avatar-chooser__choice${
                      checked ? " avatar-chooser__choice--selected" : ""
                    }`}
                    // An item with no id has not been saved into the theme yet,
                    // so the server could not resolve it. Offering it would
                    // produce a 422 the owner cannot act on.
                    disabled={status === "saving" || !iconId}
                    onClick={() => iconId && void select(iconId)}
                  >
                    {preview(item, index)}
                  </button>
                );
              })}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
