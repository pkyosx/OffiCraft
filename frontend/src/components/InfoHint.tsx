import { useI18n } from "../i18n";
import { InfoIcon } from "./icons";
import { InstantHint } from "./InstantHint";
import "./info-hint.css";

/** The ⓘ after a title that holds its explanation: hover or focus shows it, a
 * click or tap pins it (the only way to read it on a phone), and the click
 * never reaches the row or label around it. Warnings, consequences and state
 * stay visible on the page; only explanation goes in here. `about` is the title
 * it explains, for the accessible name.
 *
 * Inside a button, pass `descriptionId` and point the button's
 * `aria-describedby` at it: a button may not hold a focusable element, so there
 * the ⓘ leaves the tab order and the accessibility tree, and the hint text is
 * kept as a hidden element the button is described by. */
export function InfoHint({
  about,
  hint,
  className,
  descriptionId,
}: {
  about: string;
  hint: string;
  className?: string;
  descriptionId?: string;
}) {
  const { t } = useI18n();
  const cls = className ? `info-hint ${className}` : "info-hint";
  if (descriptionId) {
    return (
      <>
        <InstantHint hint={hint} className={cls} data-testid="info-hint" focusable={false} aria-hidden="true">
          <InfoIcon size={14} />
        </InstantHint>
        <span id={descriptionId} hidden>
          {hint}
        </span>
      </>
    );
  }
  const name = `${t.common.infoAboutLead}${about}${t.common.infoAboutTail}`;
  return (
    <InstantHint
      hint={hint}
      className={cls}
      data-testid="info-hint"
      role="img"
      aria-label={name}
    >
      <InfoIcon size={14} />
    </InstantHint>
  );
}

/** A title followed by its ⓘ, with the last word (the last character of CJK)
 * and the icon kept on one line, so a wrapping title never leaves the icon
 * alone on the next line. An inline icon always allows a break before it; no
 * word joiner or end padding stops Chromium from taking it. */
export function TitleWithInfo({ title, hint }: { title: string; hint: string }) {
  const cut = lastWordStart(title);
  return (
    <span className="info-hint__title">
      {title.slice(0, cut)}
      <span className="info-hint__glue">
        {title.slice(cut)}
        <InfoHint about={title} hint={hint} />
      </span>
    </span>
  );
}

function lastWordStart(title: string): number {
  const space = title.lastIndexOf(" ");
  if (space >= 0) return space + 1;
  const last = [...title].pop() ?? "";
  return title.length - last.length;
}
