import { useEffect, useState } from "react";

interface AvatarIndexEditorProps {
  value: number | undefined;
  onSave: (avatarIndex: number) => Promise<void>;
  label: string;
  saveLabel: string;
  savingLabel: string;
  errorLabel: string;
}

export function AvatarIndexEditor({
  value,
  onSave,
  label,
  saveLabel,
  savingLabel,
  errorLabel,
}: AvatarIndexEditorProps) {
  const confirmed = value ?? 0;
  const [draft, setDraft] = useState(String(confirmed));
  const [status, setStatus] = useState<"idle" | "saving" | "error">("idle");

  useEffect(() => {
    setDraft(String(confirmed));
    setStatus("idle");
  }, [confirmed]);

  const parsed = Number(draft);
  const valid = Number.isInteger(parsed) && parsed >= 0;
  const dirty = valid && parsed !== confirmed;

  async function save() {
    if (!dirty || status === "saving") return;
    setStatus("saving");
    try {
      await onSave(parsed);
      setStatus("idle");
    } catch {
      setDraft(String(confirmed));
      setStatus("error");
    }
  }

  return (
    <div className="avatar-index-editor">
      <input
        className="inline-edit__input avatar-index-editor__input"
        type="number"
        min={0}
        step={1}
        value={draft}
        aria-label={label}
        disabled={status === "saving"}
        onChange={(event) => {
          setDraft(event.target.value);
          if (status === "error") setStatus("idle");
        }}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault();
            void save();
          }
          if (event.key === "Escape") {
            setDraft(String(confirmed));
            setStatus("idle");
          }
        }}
      />
      <button
        type="button"
        className="doc-btn"
        disabled={!dirty || status === "saving"}
        onClick={() => void save()}
      >
        {status === "saving" ? savingLabel : saveLabel}
      </button>
      {status === "error" && (
        <span className="avatar-index-editor__error" role="alert">
          {errorLabel}
        </span>
      )}
    </div>
  );
}
