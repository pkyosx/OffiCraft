// Story — the `.doc-md` DOCUMENT surfaces that render agent/owner free text
// (T-d451). Owner (2026-07-20, phone): 角色誌 / 學習經驗 carried unbreakable long
// tokens (long URL, 40-char sha, long English word) that widened the container
// and gave the whole PAGE a horizontal scrollbar.
//
// T-4974 fixed the three TASK-CARD surfaces via per-surface rules in tasks.css.
// Every OTHER `.doc-md` host (settings 角色誌, 學習經驗, 任務手冊 SOP, reply-card
// summary/body, chat bubble, agent boot prompt) still inherited the bare
// `.doc-md` base, which declares no `overflow-wrap` — so they all still overflow.
// This story renders the doc-shaped hosts against the REAL sheets so the guard
// measures production layout.
//
// The story ALSO renders a fenced code block and a GFM table: those are the two
// legitimate horizontal-scroll sub-regions (`.doc-md pre` / `.doc-md table`,
// both `overflow-x: auto`). The wrap fix must not flatten them — the guard
// asserts they still scroll inside their own box while the page does not.
import { Markdown } from "../../src/components/Markdown";
import "../../src/components/member-detail.css";
import "../../src/components/replies.css";

/** 88 chars, no break opportunity — the shape owner hit in a lessons doc. */
export const LONG_WORD =
  "supercalifragilisticexpialidociousantidisestablishmentarianismpneumonoultramicroscopicsi";
/** A real-shaped long URL with no spaces. */
export const LONG_URL =
  "https://github.com/hardcoretech/officraft/blob/1eb0c9bfeedfacecafebabe0123456789abcdef0/frontend/src/components/settings.css#L346-L412";
/** A full 40-hex sha plus a twin(...) blob — the T-4974 shape, unchanged. */
export const LONG_SHA =
  "897a00853ca287deb861dccba228cc033c1386a8/twin(desired_state/desired_machine_id/refocus_since/bank_balance)";

const DOC = `# 學習經驗

坑:部署後要驗 ${LONG_SHA} 這顆 sha 真的在服役面。

參考 ${LONG_URL}

一個沒有斷點的長字:${LONG_WORD}

\`\`\`
$ curl -s https://officraft.hardcoretech.link/api/version | jq -r '.git_sha + " " + .version'
\`\`\`

| surface | class | overflow-wrap |
| --- | --- | --- |
| 角色誌 | .doc-md | (none before T-d451) |
| 學習經驗 | .doc-md | (none before T-d451) |
`;

/**
 * Each host reproduces one real render site's wrapper chain, because the
 * overflow can be created by the WRAPPER (a flex/grid child that refuses to
 * shrink) as much as by `.doc-md` itself.
 */
export function DocMdLongTokenStory() {
  return (
    <div className="sw-page" style={{ padding: 0 }}>
      {/* 角色誌 (SettingsPage.tsx:1462) + 任務手冊 SOP (TaskManualsPage.tsx:762).
       * The wrapper chain is `.doc-card > .doc-card__body` (settings.css:324) —
       * NOT `.sw-card`, which is the software-update card (SettingsPage.tsx:792)
       * and the only user of that class. Getting this wrong matters: `.sw-card`
       * is `display: flex`, so its flex item's `min-width: auto` keeps `.doc-md`
       * from shrinking and the guard reports a residual overflow that no real
       * page has — a fixed product would still look broken. */}
      <div className="doc-card" data-surface="doc-detail">
        <div className="doc-card__body">
          <Markdown source={DOC} className="doc-md" />
        </div>
      </div>

      {/* 學習經驗 (LessonsCard.tsx:125 — .mp-lessons__body wrapper) */}
      <div className="mp-lessons" data-surface="lessons">
        <div className="mp-lessons__body">
          <Markdown source={DOC} className="doc-md" />
        </div>
      </div>

      {/* 回覆卡 summary + body (RepliesPage/ChatReplyCard/TaskReplyCard) */}
      <div className="reply-card" data-surface="reply-card">
        <Markdown source={LONG_SHA} className="reply-card__summary doc-md" />
        <Markdown source={DOC} className="reply-card__body doc-md" />

        {/* The reply card's NON-markdown fields. They render plain text, so
         * they cannot inherit the `.doc-md` base fix and carry their own rule
         * (replies.css). Measured before that rule at 375px: option text
         * +293px, answer text +233px, page +264px. */}
        <div className="reply-card__options">
          <button type="button" className="reply-option">
            <span className="reply-option__num">1</span>
            <span className="reply-option__text">{LONG_WORD}</span>
          </button>
        </div>
        <div className="reply-card__answer-text">{LONG_URL}</div>
      </div>
    </div>
  );
}
