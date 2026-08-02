-- name: CreateUserSettings :one
INSERT INTO user_settings (
    user_id,
    tracking_start_day,
    tracking_duration_days,
    default_currency,
    country_code,
    locale,
    theme,
    default_period_view,
    subscription_tier
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetUserSettingsByUserID :one
SELECT * FROM user_settings
WHERE user_id = $1;

-- name: UpdateUserSettings :one
-- Partial update: every field is nullable, and a NULL argument leaves the
-- column untouched. updated_at is handled by the set_updated_at_user_settings
-- trigger. country_code and subscription_tier are deliberately not editable
-- here (the tier is server-controlled).
--
-- Changing tracking_duration_days does NOT touch the active tracking period:
-- ClosePeriodTx re-reads these settings at close time, so the new duration
-- applies to the NEXT period. See UserSettingsService.Update.
UPDATE user_settings
SET tracking_start_day     = COALESCE(sqlc.narg(tracking_start_day),     tracking_start_day),
    tracking_duration_days = COALESCE(sqlc.narg(tracking_duration_days), tracking_duration_days),
    default_currency       = COALESCE(sqlc.narg(default_currency),       default_currency),
    locale                 = COALESCE(sqlc.narg(locale),                 locale),
    theme                  = COALESCE(sqlc.narg(theme),                  theme),
    default_period_view    = COALESCE(sqlc.narg(default_period_view),    default_period_view)
WHERE user_id = sqlc.arg(user_id)
RETURNING *;
