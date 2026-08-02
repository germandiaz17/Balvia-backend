-- name: UpsertUserAISettings :one
INSERT INTO user_ai_settings (user_id, provider, api_key_encrypted, base_url, model, enabled)
VALUES ($1, $2, $3, $4, $5, TRUE)
ON CONFLICT (user_id) DO UPDATE SET
    provider          = EXCLUDED.provider,
    api_key_encrypted = EXCLUDED.api_key_encrypted,
    base_url          = EXCLUDED.base_url,
    model             = EXCLUDED.model,
    enabled           = TRUE
RETURNING *;

-- name: GetUserAISettings :one
SELECT * FROM user_ai_settings
WHERE user_id = $1;

-- name: DeleteUserAISettings :exec
DELETE FROM user_ai_settings
WHERE user_id = $1;
