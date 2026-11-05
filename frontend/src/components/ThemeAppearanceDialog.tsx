import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import {
  PICKER_BUILTIN_THEMES,
  isBundledBuiltinTheme,
  isValidBackgroundValue,
  type ThemeBundle,
} from "../lib/themeBundle";
import {
  DEFAULT_THEME_SCHEDULE,
  MAX_SCHEDULE_ENTRIES,
  MIN_SCHEDULE_ENTRIES,
  themeScheduleProblem,
  type ThemeScheduleEntry,
} from "../lib/themeSchedule";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { CloseIcon, TrashIcon } from "./icons";
import { ConfirmModal } from "./ConfirmModal";
import officeTheme from "../../../themes/office.theme.json" with { type: "json" };
import officeLightTheme from "../../../themes/office-light.theme.json" with { type: "json" };
import "./confirm-modal.css";
import "./settings.css";
import "./theme-settings.css";

function useThemeScheduleDraft() {
  const { themeSchedule } = useI18n();
  // null = the switch is off. A draft with rows is on but maybe unsaved; the
  // server only accepts it once there are 2..6 rows, so turning on saves nothing.
  const [draft, setDraft] = useState<ThemeScheduleEntry[] | null>(() =>
    themeSchedule.length ? themeSchedule : null
  );
  const savedKey = JSON.stringify(themeSchedule);
  useEffect(() => {
    setDraft(themeSchedule.length ? themeSchedule : null);
    // Keyed on the content: a reconcile re-delivering the same schedule must
    // not throw away rows being edited.
  }, [savedKey]);
  return { draft, setDraft };
}

export interface ThemeSwatch {
  bg?: string;
  card?: string;
  text?: string;
  accent?: string;
  image?: string;
}

function swatchOfColors(colors: Record<string, string>): ThemeSwatch {
  return {
    bg: colors["--color-bg"],
    card: colors["--color-card"],
    text: colors["--color-text"],
    accent: colors["--color-accent"],
  };
}

const BUILTIN_SWATCHES: Record<string, ThemeSwatch> = {
  office: swatchOfColors(officeTheme.colors),
  "office-light": swatchOfColors(officeLightTheme.colors),
};

// A custom theme paints over the office base, so whatever it leaves out is office's.
function swatchOfBundle(bundle: ThemeBundle): ThemeSwatch {
  const own = swatchOfColors(bundle.colors);
  const canvas = bundle.backgrounds?.canvas;
  return {
    bg: own.bg ?? BUILTIN_SWATCHES.office.bg,
    card: own.card ?? BUILTIN_SWATCHES.office.card,
    text: own.text ?? BUILTIN_SWATCHES.office.text,
    accent: own.accent ?? BUILTIN_SWATCHES.office.accent,
    image: canvas && isValidBackgroundValue(canvas) ? canvas : undefined,
  };
}

/** Thumbnail colours per theme id. theme.css built-ins are known up front; a
 * custom or bundled built-in theme is loaded once per mount when one of `wanted`
 * names it, and a failed load stays a blank placeholder rather than being
 * retried. */
export function useThemeSwatches(wanted: string[]) {
  const { themeList, loadTheme, activeThemeBundle } = useI18n();
  const [loaded, setLoaded] = useState<Record<string, ThemeSwatch | null>>({});
  const inFlight = useRef(new Set<string>());
  // A save during a fetch already holds the newer bundle; the late reply must not win.
  const remembered = useRef(new Set<string>());
  const loadRef = useRef(loadTheme);
  loadRef.current = loadTheme;
  const activeRef = useRef(activeThemeBundle);
  activeRef.current = activeThemeBundle;

  const customIds = new Set(themeList.map((b) => b.id));
  const missing = [...new Set(wanted)].filter(
    (id) =>
      (customIds.has(id) || isBundledBuiltinTheme(id)) && !(id in loaded) && !inFlight.current.has(id)
  );
  const missingKey = missing.join("\n");

  useEffect(() => {
    for (const id of missing) {
      const active = activeRef.current;
      if (active && active.id === id) {
        setLoaded((m) => ({ ...m, [id]: swatchOfBundle(active) }));
        continue;
      }
      inFlight.current.add(id);
      loadRef
        .current(id)
        .then(
          (b) => {
            if (remembered.current.has(id)) return;
            setLoaded((m) => ({ ...m, [id]: swatchOfBundle(b) }));
          },
          (e) => {
            console.warn("theme thumbnail: load failed", id, e);
            setLoaded((m) => ({ ...m, [id]: null }));
          }
        )
        .finally(() => inFlight.current.delete(id));
    }
  }, [missingKey]);

  return {
    swatchOf: (id: string): ThemeSwatch | null =>
      BUILTIN_SWATCHES[id] ?? loaded[id] ?? null,
    remember: (bundle: ThemeBundle) => {
      remembered.current.add(bundle.id);
      setLoaded((m) => ({ ...m, [bundle.id]: swatchOfBundle(bundle) }));
    },
  };
}

type SwatchOf = ReturnType<typeof useThemeSwatches>["swatchOf"];

export function ThemeThumb({ swatch }: { swatch: ThemeSwatch | null }) {
  if (!swatch) {
    return <span className="ts-thumb ts-thumb--blank" aria-hidden="true" data-testid="ts-thumb" />;
  }
  return (
    <span
      className="ts-thumb"
      aria-hidden="true"
      data-testid="ts-thumb"
      style={{
        backgroundColor: swatch.bg,
        backgroundImage: swatch.image ? `url("${swatch.image}")` : undefined,
      }}
    >
      <span className="ts-thumb__card" style={{ backgroundColor: swatch.card }}>
        <span className="ts-thumb__line" style={{ backgroundColor: swatch.text }} />
        <span className="ts-thumb__line ts-thumb__line--short" style={{ backgroundColor: swatch.accent }} />
      </span>
    </span>
  );
}

function useThemeNames() {
  const { t, themeList } = useI18n();
  const builtinOf = (id: string) => PICKER_BUILTIN_THEMES.find((b) => b.id === id);
  return {
    full: (id: string) => {
      const b = builtinOf(id);
      return b ? t.themeIdentity[b.nameKey] : (themeList.find((x) => x.id === id)?.name ?? id);
    },
    short: (id: string) => {
      const b = builtinOf(id);
      return b ? t.themeIdentity[b.shortKey] : (themeList.find((x) => x.id === id)?.name ?? id);
    },
  };
}

/** What is on screen, in words: the seasonal theme while it shows, else the
 * schedule's row (saying it is scheduled), else the selected theme. */
export function useThemeStatusText(): string {
  const { msg, theme, scheduledTheme, seasonalThemeName } = useI18n();
  const names = useThemeNames();
  return seasonalThemeName
    ? msg.themeStatusSeasonal(seasonalThemeName)
    : scheduledTheme
      ? msg.themeStatusScheduled(names.full(scheduledTheme))
      : names.full(theme);
}

/** The thumbnail of the theme on screen, from the bundle already painted. */
export function useShownThemeSwatch(): ThemeSwatch | null {
  const { shownTheme, shownThemeBundle } = useI18n();
  return shownThemeBundle ? swatchOfBundle(shownThemeBundle) : (BUILTIN_SWATCHES[shownTheme] ?? null);
}

function ThemeOptions() {
  const { t, themeList } = useI18n();
  return (
    <>
      {PICKER_BUILTIN_THEMES.map((b) => (
        <option key={b.id} value={b.id}>
          {t.themeIdentity[b.nameKey]}
        </option>
      ))}
      {themeList.map((b) => (
        <option key={b.id} value={b.id}>
          {b.name}
        </option>
      ))}
    </>
  );
}

const toneCache = new Map<string, string | null>();

/** The average colour of a background image, or null until known (and for
 * good where the image cannot be read — jsdom, a broken file). Two themes can
 * share --color-bg and differ only in their picture, so the timeline colours a
 * segment by the picture when there is one. */
function useImageTone(src: string | undefined): string | null {
  const [tone, setTone] = useState<string | null>(() => (src ? (toneCache.get(src) ?? null) : null));
  useEffect(() => {
    if (!src) {
      setTone(null);
      return;
    }
    if (toneCache.has(src)) {
      setTone(toneCache.get(src) ?? null);
      return;
    }
    let alive = true;
    const img = new Image();
    img.onload = () => {
      let value: string | null = null;
      try {
        const canvas = document.createElement("canvas");
        canvas.width = canvas.height = 8;
        const ctx = canvas.getContext("2d");
        if (ctx) {
          ctx.drawImage(img, 0, 0, 8, 8);
          const px = ctx.getImageData(0, 0, 8, 8).data;
          const sum = [0, 0, 0];
          for (let i = 0; i < px.length; i += 4) {
            sum[0] += px[i];
            sum[1] += px[i + 1];
            sum[2] += px[i + 2];
          }
          const n = px.length / 4;
          value = `rgb(${sum.map((c) => Math.round(c / n)).join(", ")})`;
        }
      } catch {
        value = null;
      }
      toneCache.set(src, value);
      if (alive) setTone(value);
    };
    img.src = src;
    return () => {
      alive = false;
    };
  }, [src]);
  return tone;
}

const DAY_MINUTES = 24 * 60;

const minutesOf = (start: string) => {
  const m = /^(\d{2}):(\d{2})$/.exec(start);
  return m ? Number(m[1]) * 60 + Number(m[2]) : null;
};

function TimelineSegment({
  widthPercent,
  swatch,
  name,
  start,
}: {
  widthPercent: number;
  swatch: ThemeSwatch | null;
  name: string;
  start: string | null;
}) {
  const tone = useImageTone(swatch?.image);
  return (
    <span
      className="ts-timeline__seg"
      data-testid="ts-timeline-seg"
      style={{
        width: `${widthPercent}%`,
        backgroundColor: tone ?? swatch?.bg,
        color: swatch?.text,
      }}
    >
      <span className="ts-timeline__text">
        <span className="ts-timeline__name">{name}</span>
        {start && <span className="ts-timeline__time">{start}</span>}
      </span>
    </span>
  );
}

/** Today at a glance, 00:00 → 24:00. Decorative: the rows carry the same
 * information for assistive tech. */
function ScheduleTimeline({ rows, swatchOf }: { rows: ThemeScheduleEntry[]; swatchOf: SwatchOf }) {
  const names = useThemeNames();
  const marks = rows
    .map((r) => ({ at: minutesOf(r.start), start: r.start, theme: r.theme }))
    .filter((m): m is { at: number; start: string; theme: string } => m.at !== null)
    .sort((a, b) => a.at - b.at);
  if (!marks.length) return null;
  const last = marks[marks.length - 1];
  const segments = [
    // Before the first start it is still the previous evening's last row.
    ...(marks[0].at > 0 ? [{ from: 0, to: marks[0].at, theme: last.theme, start: null }] : []),
    ...marks.map((m, i) => ({
      from: m.at,
      to: marks[i + 1]?.at ?? DAY_MINUTES,
      theme: m.theme,
      start: m.start as string | null,
    })),
  ].filter((seg) => seg.to > seg.from);
  return (
    <div className="ts-timeline" aria-hidden="true" data-testid="ts-timeline">
      {segments.map((seg, i) => (
        <TimelineSegment
          key={i}
          widthPercent={((seg.to - seg.from) / DAY_MINUTES) * 100}
          swatch={swatchOf(seg.theme)}
          name={names.short(seg.theme)}
          start={seg.start}
        />
      ))}
    </div>
  );
}

function ThemeAppearance({
  draft,
  setDraft,
  swatchOf,
}: ReturnType<typeof useThemeScheduleDraft> & { swatchOf: SwatchOf }) {
  const { t, theme, setTheme, themeSchedule, setThemeSchedule, seasonalTheme, setSeasonalTheme } =
    useI18n();
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<"" | "saved" | "start" | "duplicate" | "failed">("");

  const save = async (next: ThemeScheduleEntry[]) => {
    const problem = themeScheduleProblem(next);
    if (problem === "start" || problem === "duplicate") {
      setStatus(problem);
      return;
    }
    setBusy(true);
    setStatus("");
    try {
      await setThemeSchedule(next);
      setStatus(next.length ? "saved" : "");
    } catch (e) {
      console.warn("theme schedule: save failed", e);
      setStatus("failed");
    } finally {
      setBusy(false);
    }
  };

  // A saved schedule switches off when the server has cleared it (the draft
  // hook's effect); a failed clear leaves the switch on.
  const toggleSchedule = () => {
    setStatus("");
    if (draft === null) {
      setDraft([...DEFAULT_THEME_SCHEDULE]);
    } else if (themeSchedule.length) {
      void save([]);
    } else {
      setDraft(null);
    }
  };

  const editRow = (i: number, patch: Partial<ThemeScheduleEntry>) => {
    setStatus("");
    setDraft((rows) => rows && rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  };

  return (
    <div className="param-card ts-appearance">
      <div className="param-row">
        <div className="param-row__body">
          <span className="param-row__name">{t.settings.themeSchedule}</span>
          <div className="param-row__sub">{t.settings.themeScheduleHint}</div>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={draft !== null}
          aria-label={t.settings.themeSchedule}
          className={`set-toggle${draft !== null ? " set-toggle--on" : ""}`}
          disabled={busy}
          onClick={toggleSchedule}
        >
          <span className="set-toggle__knob" />
        </button>
      </div>

      {draft ? (
        <div className="param-row param-row--stacked ts-schedule" data-testid="theme-schedule">
          <ScheduleTimeline rows={draft} swatchOf={swatchOf} />
          <div className="ts-list">
            {draft.map((row, i) => (
              <div key={i} className="ts-row ts-schedule-row">
                <input
                  type="time"
                  className="ts-input ts-schedule-start"
                  aria-label={`${t.settings.themeScheduleStart} ${i + 1}`}
                  value={row.start}
                  onChange={(e) => editRow(i, { start: e.target.value })}
                />
                <ThemeThumb swatch={swatchOf(row.theme)} />
                <select
                  className="ts-input"
                  aria-label={`${t.settings.themeScheduleTheme} ${i + 1}`}
                  value={row.theme}
                  onChange={(e) => editRow(i, { theme: e.target.value })}
                >
                  <ThemeOptions />
                </select>
                <button
                  type="button"
                  className="ts-icon-btn ts-icon-btn--danger"
                  aria-label={`${t.settings.themeScheduleRemove} ${i + 1}`}
                  title={t.settings.themeScheduleRemove}
                  disabled={draft.length <= MIN_SCHEDULE_ENTRIES}
                  onClick={() => {
                    setStatus("");
                    setDraft(draft.filter((_, j) => j !== i));
                  }}
                >
                  <TrashIcon size={15} />
                </button>
              </div>
            ))}
          </div>
          <div className="ts-toolbar ts-schedule-actions">
            <button
              type="button"
              className="doc-btn"
              disabled={draft.length >= MAX_SCHEDULE_ENTRIES}
              onClick={() => {
                setStatus("");
                setDraft([...draft, { start: "", theme: "office" }]);
              }}
            >
              {t.settings.themeScheduleAdd}
            </button>
            <button
              type="button"
              className="doc-btn doc-btn--accent"
              disabled={busy}
              onClick={() => void save(draft)}
            >
              {t.settings.themeScheduleSave}
            </button>
          </div>
          {status === "saved" && <div className="ts-link-note">{t.settings.themeScheduleSaved}</div>}
          {status === "start" && <div className="set-error">{t.settings.themeScheduleErrorStart}</div>}
          {status === "duplicate" && (
            <div className="set-error">{t.settings.themeScheduleErrorDuplicate}</div>
          )}
          {status === "failed" && <div className="set-error">{t.settings.themeScheduleSaveFailed}</div>}
        </div>
      ) : (
        <div className="param-row ts-pick-row">
          <div className="param-row__body">
            <span className="param-row__name">{t.settings.themePick}</span>
            <div className="param-row__sub">{t.settings.themePickHint}</div>
          </div>
          <ThemeThumb swatch={swatchOf(theme)} />
          {/* Built-ins are written out first and each option is the theme's own
           * name only, so no pack name can pass itself off as a built-in or
           * move ahead of one. */}
          <select
            className="ts-input ts-theme-select"
            aria-label={t.settings.themePick}
            value={theme}
            onChange={(e) => setTheme(e.target.value)}
          >
            <ThemeOptions />
          </select>
        </div>
      )}

      <div className="param-row">
        <div className="param-row__body">
          <span className="param-row__name">{t.profile.seasonalTheme}</span>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={seasonalTheme}
          aria-label={t.profile.seasonalTheme}
          className={`set-toggle${seasonalTheme ? " set-toggle--on" : ""}`}
          onClick={() => setSeasonalTheme(!seasonalTheme)}
        >
          <span className="set-toggle__knob" />
        </button>
      </div>
    </div>
  );
}

/** 主題設定: the one place the theme on screen is chosen — a single theme or a
 * time-of-day schedule, and the seasonal switch. Opened from 偏好設定 and from
 * 設定 › 主題 › 外觀. Portalled to <body>. */
export function ThemeAppearanceDialog({
  onClose,
  returnFocusTo,
}: {
  onClose: () => void;
  /** Where focus goes on close when the opener itself is gone. */
  returnFocusTo?: HTMLElement | null;
}) {
  const { t, theme, restoreDefaultAppearance } = useI18n();
  const [confirmRestore, setConfirmRestore] = useState(false);
  const [restoring, setRestoring] = useState(false);
  const [restoreError, setRestoreError] = useState("");
  const schedule = useThemeScheduleDraft();
  const swatches = useThemeSwatches(schedule.draft?.map((r) => r.theme) ?? [theme]);
  const rootRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  useEscapeLayer(onClose, rootRef);

  const restore = async () => {
    setRestoring(true);
    setRestoreError("");
    try {
      await restoreDefaultAppearance();
      setConfirmRestore(false);
    } catch (e) {
      console.warn("theme appearance: restore default failed", e);
      setRestoreError(t.settings.themeRestoreFailed);
    } finally {
      setRestoring(false);
    }
  };

  // Focus goes in and comes back (the MarkdownPreviewOverlay contract); not a trap.
  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    rootRef.current?.focus();
    return () => {
      const target =
        opener && opener !== document.body && opener.isConnected ? opener : returnFocusTo;
      if (target && target.isConnected) target.focus();
    };
  }, []);

  return createPortal(
    <div
      ref={rootRef}
      className="confirm-modal ts-modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      tabIndex={-1}
      data-testid="theme-appearance-dialog"
    >
      <div className="confirm-modal__box ts-modal__box">
        <div className="ts-modal__head">
          <h2 id={titleId} className="ts-modal__title">
            {t.settings.themeDialogTitle}
          </h2>
          <button
            type="button"
            className="ts-icon-btn"
            aria-label={t.settings.themeDialogClose}
            title={t.settings.themeDialogClose}
            onClick={onClose}
          >
            <CloseIcon size={15} />
          </button>
        </div>
        <ThemeAppearance {...schedule} swatchOf={swatches.swatchOf} />
        <div className="confirm-modal__actions ts-modal__actions">
          <button
            type="button"
            className="confirm-modal__btn"
            onClick={() => {
              setRestoreError("");
              setConfirmRestore(true);
            }}
          >
            {t.settings.themeRestoreDefault}
          </button>
          <button type="button" className="confirm-modal__btn" onClick={onClose}>
            {t.settings.themeDialogClose}
          </button>
        </div>
      </div>
      {confirmRestore && (
        <ConfirmModal
          testId="theme-restore-confirm"
          confirmTestId="theme-restore-confirm-btn"
          body={t.settings.themeRestoreConfirm}
          error={restoreError}
          busy={restoring}
          cancelLabel={t.settings.cancel}
          confirmLabel={t.settings.themeRestoreDefault}
          onCancel={() => setConfirmRestore(false)}
          onConfirm={() => void restore()}
        />
      )}
    </div>,
    document.body
  );
}
