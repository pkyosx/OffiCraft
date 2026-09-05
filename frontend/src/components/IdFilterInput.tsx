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

import type { CSSProperties } from "react";
import "./idFilter.css";

interface IdFilterInputProps {
  value: string;
  onChange: (next: string) => void;
  /** aria-label AND placeholder — the field carries no separate visible label. */
  label: string;
  testId: string;
  /** How many CHARACTERS this field has to hold — the id it filters on, not a
   * look. owner 2026-09-06: the field read as too wide because 200px was picked
   * with no reference to what goes in it, while 請示卡 ids are a FIXED length
   * (`api_replycards.go:283` mints "rc-" + 12 hex ⇒ always 15 characters). So
   * the width is DERIVED: pass the id length and the field sizes to it.
   *
   * 任務 ids are not fixed (`T-93` here, `t-<12 hex>` in the canonical form), so
   * owner set that one by hand — ten characters, his call, not a measurement. */
  widthCh: number;
}

export function IdFilterInput({
  value,
  onChange,
  label,
  testId,
  widthCh,
}: IdFilterInputProps) {
  return (
    <input
      type="text"
      className="id-filter"
      // A custom property, not a width: idFilter.css owns the box model (it has
      // to — the field runs `content-box` against the app's global `border-box`
      // so this count is the TEXT area, not the text area minus the padding).
      // `ch` is the advance of "0" in the field's own font, so the box tracks
      // the text it holds through a font or size change instead of freezing a
      // pixel count that was only ever right for one of them.
      style={{ "--id-filter-ch": widthCh } as CSSProperties}
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
