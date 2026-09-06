// IdFilterInput — the ID field on a list page's filter panel.
//
// 🔴 IT IS A TEXT BOX AND NOTHING ELSE. It holds a value and reports changes;
// it does not fetch, does not match, and does not decide what an id MEANS.
// That belongs to the host page, and the two hosts no longer agree:
// RepliesPage (T-93 round 2, owner 2026-09-06 option ①) asks the server for
// the id on Apply and tells 找到／404／問不到伺服器 apart, while the 任務頁
// keeps its own semantics. Do not restore a sentence here claiming either
// behaviour as this component's own: an earlier version of this comment
// stated round 1's 「不分大小寫的子字串比對、不打任何 API、沒有找不到提示」 as
// if it were this component's contract, and it went stale the moment one host
// changed.

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
