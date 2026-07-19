// ModelEffortEditor — the SHARED model/effort quick-pick editor (owner M2).
//
// Presentation conventions inherited from the retired 新增角色 form (the
// inline-row redesign removed it from Settings; the pattern lives on here as a
// reusable piece):
//   • model  = fable/opus/sonnet/haiku quick-pick CHIPS + a free custom string
//     input (spawn --model is a FREE string — the chips are safe defaults, the
//     input stays authoritative; BLANK ⇒ the server/CLI default).
//   • effort = a low/medium/high dropdown (closed vocabulary, server-422
//     outside it).
//
// Controlled + stateless: the caller owns the draft values (it decides when to
// PATCH); this component only renders the two fields and reports changes.

import { useI18n } from "../i18n";
import type { Effort } from "../types";
import "./model-effort-editor.css";

/** Quick-pick model chips (safe defaults — the input stays free-form). */
export const MODEL_QUICK_PICKS = ["fable", "opus", "sonnet", "haiku"] as const;
/** The closed effort vocabulary (server 422s anything else). */
export const EFFORTS: readonly Effort[] = ["low", "medium", "high"] as const;

export function ModelEffortEditor({
  model,
  effort,
  onModelChange,
  onEffortChange,
}: {
  model: string;
  effort: string;
  onModelChange: (model: string) => void;
  onEffortChange: (effort: string) => void;
}) {
  const { t } = useI18n();
  return (
    <>
      <div className="me-editor__label">{t.mp.model}</div>
      <div className="me-editor__chips">
        {MODEL_QUICK_PICKS.map((m) => (
          <button
            type="button"
            key={m}
            className={`doc-btn${model === m ? " doc-btn--accent" : ""}`}
            data-testid={`me-model-chip-${m}`}
            onClick={() => onModelChange(m)}
          >
            {m}
          </button>
        ))}
      </div>
      <input
        className="me-editor__input"
        value={model}
        placeholder={t.mp.modelPlaceholder}
        aria-label={t.mp.model}
        onChange={(e) => onModelChange(e.target.value)}
        data-testid="me-model-input"
      />
      <div className="me-editor__label me-editor__label--stacked">
        {t.mp.effort}
      </div>
      <select
        className="me-editor__select"
        value={effort}
        aria-label={t.mp.effort}
        onChange={(e) => onEffortChange(e.target.value)}
        data-testid="me-effort-select"
      >
        {EFFORTS.map((e) => (
          <option key={e} value={e}>
            {t.mp.effortLevel(e)} ({e})
          </option>
        ))}
      </select>
    </>
  );
}
