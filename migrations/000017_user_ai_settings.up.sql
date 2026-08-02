-- Per-user BYOK (bring your own key) AI provider settings. Each user supplies
-- their own API key for their chosen provider (Anthropic, or any OpenAI-compatible
-- endpoint). The key is stored ENCRYPTED (AES-GCM, nonce||ciphertext); the app
-- decrypts it per request and never returns it to the client.

CREATE TABLE user_ai_settings (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    provider          VARCHAR(30) NOT NULL,
    api_key_encrypted BYTEA NOT NULL,             -- AES-GCM ciphertext (nonce||ct)
    base_url          TEXT,                        -- openai_compatible only (e.g. https://api.openai.com/v1)
    model             TEXT,                        -- optional model override
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_ai_provider CHECK (provider IN ('anthropic', 'openai_compatible'))
);

CREATE TRIGGER set_updated_at_user_ai_settings
    BEFORE UPDATE ON user_ai_settings
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();
