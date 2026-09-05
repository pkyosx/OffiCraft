// IdFilterInput — the ID 篩選 field on a list page's 篩選列.
//
// It is a PLAIN FILTER, deliberately: owner 2026-09-05 (rc-2085e5ec60be)
// ruled that filtering by id must behave exactly like the filters next to it
// —「我預期功能都一樣 / 只是連過去幫忙帶篩選參數而已」—— so this control adds
// no locate mechanism, no "not found" notice and no fetch of its own. A URL
// carrying an id only PRE-FILLS it; the page's own clear-filters button
// empties it.
//
// Matching is case-insensitive SUBSTRING (see the callers): a pasted id is a
// full match, and a half-typed one still narrows instead of collapsing to
// nothing the moment the first character lands.

import "./idFilter.css";

interface IdFilterInputProps {
  value: string;
  onChange: (next: string) => void;
  /** aria-label AND placeholder — the field carries no separate visible label. */
  label: string;
  testId: string;
}

export function IdFilterInput({
  value,
  onChange,
  label,
  testId,
}: IdFilterInputProps) {
  return (
    <input
      type="text"
      className="id-filter"
      data-testid={testId}
      aria-label={label}
      placeholder={label}
      value={value}
      spellCheck={false}
      autoComplete="off"
      onChange={(e) => onChange(e.target.value)}
    />
  );
}
