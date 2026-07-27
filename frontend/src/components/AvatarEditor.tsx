import { useRef, useState } from "react";
import { useI18n } from "../i18n";
import type { AvatarKind } from "../lib/themeBundle";
import { Avatar } from "./Avatar";

const MAX_AVATAR_BYTES = 64 * 1024;
const AVATAR_MIMES = new Set(["image/png", "image/jpeg", "image/webp"]);

interface AvatarEditorProps {
  size?: number;
  kind: AvatarKind;
  src?: string;
  onUpload: (file: File) => Promise<void>;
  onRemove: () => Promise<void>;
}

/** Owner-only editor for one stable member id's personal image. */
export function AvatarEditor({
  size = 52,
  kind,
  src,
  onUpload,
  onRemove,
}: AvatarEditorProps) {
  const { t } = useI18n();
  const inputRef = useRef<HTMLInputElement>(null);
  const uploadButtonRef = useRef<HTMLButtonElement>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function choose(file?: File) {
    if (!file) return;
    setError("");
    if (!AVATAR_MIMES.has(file.type)) {
      setError(t.mp.avatarTypeError);
      return;
    }
    if (file.size > MAX_AVATAR_BYTES) {
      setError(t.mp.avatarTooLarge);
      return;
    }
    setBusy(true);
    try {
      await onUpload(file);
    } catch {
      setError(t.mp.avatarSaveError);
    } finally {
      setBusy(false);
      if (inputRef.current) inputRef.current.value = "";
    }
  }

  async function remove() {
    setError("");
    setBusy(true);
    try {
      await onRemove();
    } catch {
      setError(t.mp.avatarSaveError);
    } finally {
      setBusy(false);
      // Removing src unmounts the focused remove button. Return keyboard focus
      // to the stable visible action instead of dropping it to <body>. Defer
      // until React has committed disabled=false; browsers ignore focus() on a
      // disabled button.
      window.setTimeout(() => uploadButtonRef.current?.focus(), 0);
    }
  }

  return (
    <div className="avatar-editor">
      <Avatar size={size} kind={kind} src={src} />
      <div className="avatar-editor__actions">
        <input
          ref={inputRef}
          className="avatar-editor__input"
          type="file"
          accept="image/png,image/jpeg,image/webp"
          aria-label={t.mp.avatarUpload}
          tabIndex={-1}
          disabled={busy}
          onChange={(event) => void choose(event.target.files?.[0])}
        />
        <button
          ref={uploadButtonRef}
          type="button"
          className="avatar-editor__button"
          disabled={busy}
          onClick={() => inputRef.current?.click()}
        >
          {busy ? t.mp.avatarBusy : t.mp.avatarUpload}
        </button>
        {src ? (
          <button
            type="button"
            className="avatar-editor__button avatar-editor__button--remove"
            disabled={busy}
            onClick={() => void remove()}
          >
            {t.mp.avatarRemove}
          </button>
        ) : null}
        {error ? (
          <span className="avatar-editor__error" role="alert">
            {error}
          </span>
        ) : null}
      </div>
    </div>
  );
}
