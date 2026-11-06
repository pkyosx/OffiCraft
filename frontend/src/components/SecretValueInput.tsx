import { forwardRef, useEffect, useState, type TextareaHTMLAttributes } from "react";
import { useI18n } from "../i18n";
import { EyeIcon, EyeOffIcon } from "./icons";
import "./secret-value-input.css";

type Props = TextareaHTMLAttributes<HTMLTextAreaElement> & { value: string };

/** The masked secret field. A textarea masked by CSS rather than a password input:
 * an input strips newlines, which silently corrupts PEM and SSH keys. Starts hidden,
 * and hides again once its value is cleared so the next value does not open shown. */
export const SecretValueInput = forwardRef<HTMLTextAreaElement, Props>(function SecretValueInput(
  { className, value, ...rest },
  ref,
) {
  const { t } = useI18n();
  const [revealed, setRevealed] = useState(false);

  useEffect(() => {
    if (value === "") setRevealed(false);
  }, [value]);

  const label = revealed ? t.secrets.hideValue : t.secrets.showValue;
  return (
    <div className="secret-value-input">
      <textarea
        ref={ref}
        className={`secret-value-input__field${revealed ? "" : " secret-value-input__field--masked"}${
          className ? ` ${className}` : ""
        }`}
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        spellCheck={false}
        value={value}
        {...rest}
      />
      <button
        type="button"
        className="secret-value-input__toggle"
        data-testid="secret-reveal-toggle"
        aria-label={label}
        aria-pressed={revealed}
        title={label}
        // Keeps the caret in the field while the owner checks what they pasted.
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => setRevealed((r) => !r)}
      >
        {revealed ? <EyeOffIcon size={16} /> : <EyeIcon size={16} />}
      </button>
    </div>
  );
});
