import type { ReactNode } from "react";
import "./page-toolbar.css";

/** The row above a page's card list: the count on the left, the page's add
 * button (`children`, styled `page-toolbar__btn`) on the right. */
export function PageToolbar({
  count,
  countTestId,
  children,
}: {
  /** Empty while the list is loading or failed to load. */
  count: string;
  countTestId: string;
  children: ReactNode;
}) {
  return (
    <div className="page-toolbar">
      <span className="page-toolbar__count" data-testid={countTestId}>
        {count}
      </span>
      {children}
    </div>
  );
}
