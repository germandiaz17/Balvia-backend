-- ACCOUNTS
CREATE TABLE accounts (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name                VARCHAR(100) NOT NULL,
    account_type        VARCHAR(30) NOT NULL,
    currency            CHAR(3) NOT NULL DEFAULT 'COP',
    initial_balance     NUMERIC(15,2) NOT NULL DEFAULT 0,
    current_balance     NUMERIC(15,2) NOT NULL DEFAULT 0,
    icon                VARCHAR(50),
    color               CHAR(7),
    is_archived         BOOLEAN NOT NULL DEFAULT FALSE,
    display_order       INTEGER NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ,

    CONSTRAINT chk_account_type CHECK (account_type IN ('cash','checking','savings','credit_card','investment','other'))
);

CREATE INDEX idx_accounts_user_id ON accounts(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_accounts_user_active ON accounts(user_id) WHERE deleted_at IS NULL AND is_archived = FALSE;

-- CATEGORIES
CREATE TABLE categories (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID REFERENCES users(id) ON DELETE CASCADE,
    parent_id       UUID REFERENCES categories(id) ON DELETE SET NULL,
    name            VARCHAR(100) NOT NULL,
    category_type   VARCHAR(20) NOT NULL,
    icon            VARCHAR(50),
    color           CHAR(7),
    is_system       BOOLEAN NOT NULL DEFAULT FALSE,
    display_order   INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,

    CONSTRAINT chk_category_type CHECK (category_type IN ('income','expense','transfer')),
    CONSTRAINT chk_system_no_user CHECK (
        (is_system = TRUE AND user_id IS NULL) OR
        (is_system = FALSE AND user_id IS NOT NULL)
    )
);

CREATE INDEX idx_categories_user_id ON categories(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_categories_parent_id ON categories(parent_id);
CREATE INDEX idx_categories_system ON categories(is_system) WHERE is_system = TRUE;
