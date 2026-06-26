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
