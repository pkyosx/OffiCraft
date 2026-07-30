import { useEffect, useState } from "react";
import { useActiveAvatarPools } from "../i18n";
import type { PoolAvatarKind } from "../lib/themeBundle";

interface AvatarIndexEditorProps {
  value: number | undefined;
  kind: PoolAvatarKind;
  onSave: (avatarIndex: number) => Promise<void>;
  label: string;
  savingLabel: string;
  errorLabel: string;
}

/**
 * The wire stores a stable numeric slot, but that number is an implementation
 * detail. Owners choose the identity they can actually see; clicking a preview
 * persists its slot and a rejected write restores the last confirmed image.
 */
export function AvatarIndexEditor({
  value,
  kind,
  onSave,
  label,
  savingLabel,
  errorLabel,
}: AvatarIndexEditorProps) {
  const pools = useActiveAvatarPools();
  const pool = pools?.[kind] ?? [];
  const propConfirmed = value ?? 0;
  const [confirmed, setConfirmed] = useState(propConfirmed);
  const [pending, setPending] = useState<number | null>(null);
  const [status, setStatus] = useState<"idle" | "saving" | "error">("idle");

  useEffect(() => {
    setConfirmed(propConfirmed);
    setPending(null);
    setStatus("idle");
  }, [propConfirmed]);

  if (pool.length === 0) return null;

  const selected = ((confirmed % pool.length) + pool.length) % pool.length;

  async function select(index: number) {
    if (index === selected || status === "saving") return;
    setPending(index);
    setStatus("saving");
    try {
      await onSave(index);
      setConfirmed(index);
      setPending(null);
      setStatus("idle");
    } catch {
      setPending(null);
      setStatus("error");
    }
  }

  return (
    <div className="avatar-index-editor">
      <div className="avatar-index-editor__label">{label}</div>
      <div
        className="avatar-index-editor__choices"
        role="radiogroup"
        aria-label={label}
        aria-busy={status === "saving"}
      >
        {pool.map((src, index) => {
          const checked = (pending ?? selected) === index;
          return (
            <button
              key={`${kind}-${index}`}
              type="button"
              role="radio"
              aria-checked={checked}
              aria-label={`${label} ${index + 1}`}
              className={`avatar-index-editor__choice${
                checked ? " avatar-index-editor__choice--selected" : ""
              }`}
              disabled={status === "saving"}
              onClick={() => void select(index)}
            >
              <img src={src} alt="" aria-hidden="true" draggable={false} />
            </button>
          );
        })}
      </div>
      {status === "saving" && (
        <span className="avatar-index-editor__status" role="status">
          {savingLabel}
        </span>
      )}
      {status === "error" && (
        <span className="avatar-index-editor__error" role="alert">
          {errorLabel}
        </span>
      )}
    </div>
  );
}
