// CT story for the identity card's id badge after T-5dab swapped the derived
// `MB-XXX###` label for the REAL member id.
//
// Why this needs a real browser: the new string is both LONGER (an outsource id
// is `ow-` + 12 hex = 15 chars vs the old 9) and, unlike the old one, it
// contains a HYPHEN in the middle — a legal line-break opportunity. The badge
// has a FIXED height (21px), so a break does not make it taller, it makes the
// second line spill out of the badge. jsdom resolves neither line boxes nor
// `@media`, so nothing in the vitest suite can see either failure.
//
// The ancestor chain is the panel's real one — `.mp-card.mp-identity` (flex row,
// 20px side padding) holding the avatar, the flex-1/min-width-0 body, and the
// action cluster at the end. Bare-mounting the badge would give it the whole
// viewport and neither failure could reproduce.
import { I18nProvider } from "../../src/i18n";
import { InlineEdit } from "../../src/components/InlineEdit";
import { MemberActionButtons } from "../../src/components/MemberActionButtons";
import "../../src/styles/theme.css";
import "../../src/components/office.css";
import "../../src/components/member-detail.css";

// The fixture values live in the spec (identity-real-id.ct.spec.tsx): a CT
// story module may export the component only — a second export makes Playwright
// register the module twice and the bundle fails with a duplicate identifier.
export function IdentityRealIdStory({
  name = "OffiCraft 自動化測試員",
  id = "ow-7eed74b85026",
}: {
  name?: string;
  id?: string;
}) {
  return (
    <I18nProvider>
      <div style={{ width: "100%", padding: 22 }}>
        <div className="mp-card mp-identity" data-testid="story-identity">
          <div
            style={{ width: 52, height: 52, flex: "none" }}
            data-testid="story-avatar"
          />
          <div className="mp-identity__body">
            <div className="mp-identity__line" data-testid="story-line">
              {/* The REAL InlineEdit, as the panel renders it: the name sits
                  inside an inline-flex wrapper next to a pencil button, so a
                  bare <span> here would understate how much room the name half
                  actually takes. */}
              <InlineEdit
                value={name}
                onCommit={() => {}}
                ariaLabel="改名"
                className="story-name"
                displayClassName="mp-identity__name"
              />
              <span
                className="badge mp-identity__id"
                data-testid="story-id-badge"
              >
                {id}
              </span>
            </div>
            <div className="mp-identity__status">
              <span>助理</span>
            </div>
          </div>
          <div className="mp-identity__actions">
            <div className="mp-identity__buttons">
              <button type="button" className="btn btn--accent-ghost">
                更改
              </button>
              <MemberActionButtons status="online-awake" onStop={() => {}} />
            </div>
          </div>
        </div>
      </div>
    </I18nProvider>
  );
}
