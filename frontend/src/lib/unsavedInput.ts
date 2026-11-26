type TextField = HTMLInputElement | HTMLTextAreaElement | HTMLElement;

const NON_TEXT_INPUT_TYPES = new Set([
  "button",
  "checkbox",
  "color",
  "file",
  "hidden",
  "image",
  "radio",
  "range",
  "reset",
  "submit",
]);

function asTextField(target: EventTarget | null): TextField | null {
  if (target instanceof HTMLTextAreaElement) return target;
  if (target instanceof HTMLInputElement) return NON_TEXT_INPUT_TYPES.has(target.type) ? null : target;
  if (target instanceof HTMLElement && target.isContentEditable) return target;
  return null;
}

function currentText(field: TextField): string {
  return field instanceof HTMLInputElement || field instanceof HTMLTextAreaElement
    ? field.value
    : field.textContent ?? "";
}

/** Remembers what each text field held when it was focused, so that a field
 * typed into and not yet put back reads as unsaved work. */
export function trackUnsavedTextFields(doc: Document): {
  hasEditedField: () => boolean;
  stop: () => void;
} {
  const textAtFocus = new Map<TextField, string>();
  const onFocusIn = (e: Event) => {
    const field = asTextField(e.target);
    if (field && !textAtFocus.has(field)) textAtFocus.set(field, currentText(field));
  };
  doc.addEventListener("focusin", onFocusIn, true);
  return {
    hasEditedField: () => {
      for (const [field, before] of textAtFocus) {
        if (!field.isConnected) textAtFocus.delete(field);
        else if (currentText(field) !== before) return true;
      }
      return false;
    },
    stop: () => doc.removeEventListener("focusin", onFocusIn, true),
  };
}

const unsavedWork = new Set<() => boolean>();

/** Work held outside text fields (e.g. 傳承's staged 編輯多筆 changes). While a
 * registered check says true, the stale-build watch does not reload the page
 * on its own. Returns the release. */
export function registerUnsavedWork(check: () => boolean): () => void {
  unsavedWork.add(check);
  return () => {
    unsavedWork.delete(check);
  };
}

export function hasRegisteredUnsavedWork(): boolean {
  for (const check of unsavedWork) if (check()) return true;
  return false;
}
