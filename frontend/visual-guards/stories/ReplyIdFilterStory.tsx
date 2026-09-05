// CT story: the 請示 page's 篩選列 (T-93) in the production DOM shape it ships
// in — `.replies__filters` holding the real `IdFilterInput` plus the real
// 清除篩選 button, inside `.replies` and the app's content column — so the
// loaded replies.css / idFilter.css / theme.css, not a mock sheet, govern the
// geometry and the colours the guard measures.
//
// The component under test is the SHIPPED `IdFilterInput`. What is stand-in is
// only the page around it: RepliesPage reaches its data through `api`, and the
// question here is "does this row lay out and stay findable", which lives in
// the CSS box tree rather than in the fetch.
//
// `theme` is applied the way the app applies it — `data-theme` on the root
// element — so the guard can ask the same question twice under the two theme
// families without the story re-implementing theming.
import { useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { IdFilterInput } from "../../src/components/IdFilterInput";
import "../../src/components/chrome.css"; // .app / .app__main — the real width cap
import "../../src/components/replies.css";

export function ReplyIdFilterStory({
  theme,
  initialValue = "",
}: {
  theme: "light" | "dark";
  initialValue?: string;
}) {
  const [value, setValue] = useState(initialValue);
  // The app stamps the choice on documentElement; do the same rather than
  // wrapping in a div, so :root-scoped tokens resolve exactly as they ship.
  document.documentElement.setAttribute("data-theme", theme);

  return (
    <I18nProvider>
      <div className="app" style={{ width: "100vw", maxWidth: "100vw" }}>
        <main className="app__main">
          <div className="replies">
            <div className="replies__filters">
              <IdFilterInput
                value={value}
                onChange={setValue}
                label="請示卡編號"
                testId="filter-reply-card-id"
              />
              {value !== "" && (
                <button
                  type="button"
                  className="replies__clear-filters"
                  data-testid="clear-filters"
                  onClick={() => setValue("")}
                >
                  清除篩選
                </button>
              )}
            </div>
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
