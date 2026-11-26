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
  /** 🔴 WHEN THE TYPED VALUE BECOMES A FILTER. Enter, or blur — and nothing
   * else (owner 2026-09-06 c-c3d681fe05da:「任務編號那邊就是按enter或是點外面就
   * 視為apply了」). `onChange` moves the TEXT IN THE BOX; this moves the FILTER.
   *
   * They are two callbacks and not one because both hosts turn a committed id
   * into a SERVER request — `useTasks(…, appliedId)` on 任務頁,
   * `api.getReplyCard(idQuery)` on 請示卡頁 — so committing per keystroke is a
   * fetch per keystroke, which is the shape owner rejected twice before this
   * round (「每次都要全部都撈回來才濾不合理」). Wiring this to `onChange` would
   * look like a tidy-up and would be that rejection, restored. */
  onCommit: (next: string) => void;
  /** aria-label AND placeholder — the field carries no separate visible label. */
  label: string;
  testId: string;
  /** How many CHARACTERS the ID this field holds is — not a look. owner
   * 2026-09-06: the field read as too wide because 200px was picked with no
   * reference to what goes in it, while 請示卡 ids are a FIXED length
   * (`api_replycards.go:283` mints "rc-" + 12 hex ⇒ always 15 characters). So
   * the width is DERIVED: pass the id length and the field sizes to it.
   *
   * 🔴 THE ID IS ONLY HALF OF WHAT HAS TO FIT. `label` below is this field's
   * ONLY label — there is no caption beside it — so the box must also hold the
   * placeholder, and on 任務頁 the label is the WIDER of the two. owner
   * 2026-09-07 `rc-e2edbb0fff01` circled 「標籤維持『任務編號』，寬度取編號跟標籤
   * 的較大者」 rather than shortening the label.
   *
   * 🔴 THE LARGER OF THE TWO IS MEASURED, NOT WRITTEN DOWN. idFilter.css sizes
   * the field off a hidden copy of the label carrying `min-width: <this>ch`, so
   * the browser takes the max in the field's own font. Writing a number here for
   * the label would be one more picked constant — the exact thing owner has now
   * objected to twice on this field (2026-09-06 「200px」, 2026-09-07 「ID寬度
   * 要合理」) — and it would be wrong in the other locale, where the labels are
   * "Task ID" and "Reply-card id" rather than four and five CJK glyphs.
   *
   * 任務 ids are `T-` + an integer (`dal_task_id_seq.go`; ids minted before that
   * change are "t-" + 12 hex and were NOT migrated, so both shapes still exist
   * in the table). owner 2026-09-07 `rc-b2beb7b1fd3c`: 「任務可先假設到萬位數」
   * ⇒ 7. A pre-change id typed in here still QUERIES fine; it is only too long
   * to read at a glance, which is the cost that ruling accepted. */
  widthCh: number;
}

export function IdFilterInput({
  value,
  onChange,
  onCommit,
  label,
  testId,
  widthCh,
}: IdFilterInputProps) {
  return (
    // The box is sized by the SIZER below, not by the input: the width has to be
    // the larger of「the id」and「the label」, and only the browser knows how wide
    // the label renders. Both live in one grid cell; the cell takes the wider.
    <span
      className="id-filter-field"
      // A custom property, not a width: idFilter.css owns the box model (it has
      // to — the sizer runs `content-box` against the app's global `border-box`
      // so this count is the TEXT area, not the text area minus the padding).
      // `ch` is the advance of "0" in the field's own font, so the box tracks
      // the text it holds through a font or size change instead of freezing a
      // pixel count that was only ever right for one of them.
      style={{ "--id-filter-ch": widthCh } as CSSProperties}
    >
      <span className="id-filter-sizer" aria-hidden="true">
        {label}
      </span>
      <input
        type="text"
        className="id-filter"
        // 🔴 `size={1}` OR THE SIZER ABOVE IS IGNORED. An <input> with no `size`
        // carries a UA default of 20 characters, and that is an INTRINSIC width:
        // during the grid's intrinsic sizing pass a percentage width behaves as
        // `auto`, so the column comes out at max(sizer, 20 characters) and the
        // input's own default wins nearly always — the `widthCh` knob would go
        // dead and this field would stay the widest thing on the row, which is
        // the complaint owner opened (「ID的也太長」). 1, not 0: 0 is invalid and
        // browsers fall back to the default, which is the bug again.
        size={1}
        data-testid={testId}
        aria-label={label}
        placeholder={label}
        value={value}
        spellCheck={false}
        autoComplete="off"
        onChange={(e) => onChange(e.target.value)}
        // Enter commits. `key` rather than `keyCode`, and no `preventDefault`:
        // the field is not in a form, so there is no submit to suppress.
        onKeyDown={(e) => {
          // An IME's own Enter (confirming a candidate) must not commit the
          // filter. ids are ASCII today, so this costs nothing and guards the
          // day one is not — the standard shape for every committing field.
          if (e.nativeEvent.isComposing) return;
          if (e.key === "Enter") onCommit(e.currentTarget.value);
        }}
        // 「點外面」 — clicking away commits what is in the box. Enter then
        // blur therefore commits twice with the same value; both hosts treat
        // that as idempotent (they set state to the trimmed string), so it is a
        // no-op rather than a second fetch.
        onBlur={(e) => onCommit(e.currentTarget.value)}
      />
    </span>
  );
}
