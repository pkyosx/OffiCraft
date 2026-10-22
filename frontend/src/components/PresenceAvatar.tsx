import type { ReactNode } from "react";
import type { MemberLifecycle } from "../types";
import { LifecycleDot, presenceVisual } from "./LifecycleDot";
import "./presence-avatar.css";

/** An avatar with the member's presence dot on its bottom-right corner. Every
 * identity surface (list rows, chat header, detail identity card; staff and
 * outsource) draws its dot through this one component. The dot is a sibling of
 * `children`, never inside it, because the avatar is often a button of its own. */
export function PresenceAvatar({
  presence,
  dotTestId,
  children,
}: {
  presence: MemberLifecycle | undefined;
  dotTestId?: string;
  children: ReactNode;
}) {
  return (
    <span className="presence-avatar">
      {children}
      <span className="presence-avatar__dot">
        <LifecycleDot status={presenceVisual(presence)} testId={dotTestId} />
      </span>
    </span>
  );
}
