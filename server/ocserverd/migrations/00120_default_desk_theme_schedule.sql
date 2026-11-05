-- +goose Up
-- The office desk themes ship as built-ins and become the default appearance:
-- switch by time of day (06:00 day, 16:00 dusk, 18:30 night) with the seasonal
-- theme on. A station still on the old built-in default (display.theme unset,
-- "", office or office-light) moves to it ONCE, here; choosing office again
-- afterwards sticks. On a fresh install this is what writes the default.
-- ⚠️ The schedule literal is the twin of DEFAULT_THEME_SCHEDULE in
-- frontend/src/lib/themeSchedule.ts. The schedule and seasonal rows are written
-- before display.theme, because both test display.theme's old value.
INSERT INTO setting (key, value, updated_at)
SELECT 'display.theme_schedule',
       '[{"start":"06:00","theme":"modern-office-desk-day"},{"start":"16:00","theme":"modern-office-desk-dusk"},{"start":"18:30","theme":"modern-office-desk-night"}]',
       0
WHERE COALESCE((SELECT value FROM setting WHERE key = 'display.theme'), '')
      IN ('', 'office', 'office-light')
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

INSERT INTO setting (key, value, updated_at)
SELECT 'display.seasonal_theme', 'true', 0
WHERE COALESCE((SELECT value FROM setting WHERE key = 'display.theme'), '')
      IN ('', 'office', 'office-light')
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

INSERT INTO setting (key, value, updated_at)
SELECT 'display.theme', 'modern-office-desk-day', 0
WHERE COALESCE((SELECT value FROM setting WHERE key = 'display.theme'), '')
      IN ('', 'office', 'office-light')
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

-- A custom copy stored under a desk id (imported before the id was reserved)
-- goes, so the built-in takes over; every reference already names that id.
DELETE FROM custom_theme
WHERE theme_id IN ('modern-office-desk-day', 'modern-office-desk-dusk', 'modern-office-desk-night');

-- +goose Down
-- The older binary does not know the desk ids. Anything pointing at them goes
-- back to office; the deleted custom copies are not restored.
UPDATE setting SET value = 'office'
WHERE key = 'display.theme'
  AND value IN ('modern-office-desk-day', 'modern-office-desk-dusk', 'modern-office-desk-night');

UPDATE setting SET value = '[]'
WHERE key = 'display.theme_schedule'
  AND (value LIKE '%"modern-office-desk-day"%'
    OR value LIKE '%"modern-office-desk-dusk"%'
    OR value LIKE '%"modern-office-desk-night"%');
