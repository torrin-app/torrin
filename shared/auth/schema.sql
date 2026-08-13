CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY,
    email           TEXT NOT NULL UNIQUE,
    api_key         TEXT NOT NULL UNIQUE,
    plan_id         TEXT NOT NULL DEFAULT 'free',
    subscription_id TEXT NOT NULL DEFAULT '',
    license_key     TEXT NOT NULL DEFAULT '',
    recurrence      TEXT NOT NULL DEFAULT '',
    expires_at      TIMESTAMPTZ NOT NULL DEFAULT '2099-12-31',
    paused_at       TIMESTAMPTZ,
    remaining_days  INTEGER NOT NULL DEFAULT 0,
    pause_count     INTEGER NOT NULL DEFAULT 0,
    last_paused_at  TIMESTAMPTZ,
    banned          BOOLEAN NOT NULL DEFAULT FALSE,
    ban_reason      TEXT NOT NULL DEFAULT '',
    referral_code   TEXT NOT NULL DEFAULT '',
    referred_by     TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_users_apikey ON users(api_key);
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_subid ON users(subscription_id);
CREATE INDEX IF NOT EXISTS idx_users_referral ON users(referral_code);

CREATE TABLE IF NOT EXISTS deleted_emails (
    email TEXT PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS referrals (
    id          TEXT PRIMARY KEY,
    referrer_id TEXT NOT NULL,
    referee_id  TEXT NOT NULL,
    paid        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS processed_sales (
    sale_id    TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE processed_sales ADD COLUMN IF NOT EXISTS user_id      TEXT NOT NULL DEFAULT '';
ALTER TABLE processed_sales ADD COLUMN IF NOT EXISTS amount_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE processed_sales ADD COLUMN IF NOT EXISTS currency     TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_processed_sales_user ON processed_sales(user_id);

CREATE TABLE IF NOT EXISTS referral_clicks (
    id         BIGSERIAL PRIMARY KEY,
    code       TEXT NOT NULL,
    ip_hash    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_referral_clicks_code ON referral_clicks(code);

CREATE TABLE IF NOT EXISTS partner_tokens (
    token      TEXT PRIMARY KEY,
    code       TEXT NOT NULL,
    revoked    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS audit_log (
    id         BIGSERIAL PRIMARY KEY,
    user_id    TEXT NOT NULL,
    action     TEXT NOT NULL,
    detail     TEXT NOT NULL DEFAULT '',
    ip         TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_user ON audit_log(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_ip_action ON audit_log(ip, action, created_at);

CREATE TABLE IF NOT EXISTS blocklist_terms (
    term TEXT PRIMARY KEY,
    tier TEXT NOT NULL DEFAULT 'hard'
);

CREATE TABLE IF NOT EXISTS rd_credentials (
    user_id    TEXT PRIMARY KEY,
    api_key    TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS ad_credentials (
    user_id    TEXT PRIMARY KEY,
    api_key    TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS pm_credentials (
    user_id    TEXT PRIMARY KEY,
    api_key    TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS tb_credentials (
    user_id    TEXT PRIMARY KEY,
    api_key    TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS storage_credentials (
    user_id     TEXT PRIMARY KEY,
    provider    TEXT NOT NULL DEFAULT '',
    endpoint    TEXT NOT NULL DEFAULT '',
    region      TEXT NOT NULL DEFAULT '',
    bucket      TEXT NOT NULL DEFAULT '',
    access_key  TEXT NOT NULL DEFAULT '',
    secret_key  TEXT NOT NULL DEFAULT '',
    prefix      TEXT NOT NULL DEFAULT '',
    enabled     BOOLEAN NOT NULL DEFAULT FALSE,
    backend     TEXT NOT NULL DEFAULT '',
    config_json TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE storage_credentials ADD COLUMN IF NOT EXISTS encrypted      BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE storage_credentials ADD COLUMN IF NOT EXISTS crypt_password TEXT NOT NULL DEFAULT '';
ALTER TABLE storage_credentials ADD COLUMN IF NOT EXISTS providers        TEXT NOT NULL DEFAULT '';
ALTER TABLE storage_credentials ADD COLUMN IF NOT EXISTS union_policy     TEXT NOT NULL DEFAULT '';
ALTER TABLE storage_credentials ADD COLUMN IF NOT EXISTS evict_after_days INTEGER NOT NULL DEFAULT 0;
ALTER TABLE storage_credentials ADD COLUMN IF NOT EXISTS evict_max_bytes  BIGINT NOT NULL DEFAULT 0;
ALTER TABLE storage_credentials ADD COLUMN IF NOT EXISTS last_error       TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS usenet_credentials (
    user_id    TEXT PRIMARY KEY,
    host       TEXT NOT NULL,
    port       INTEGER NOT NULL DEFAULT 563,
    username   TEXT NOT NULL DEFAULT '',
    password   TEXT NOT NULL DEFAULT '',
    ssl        BOOLEAN NOT NULL DEFAULT TRUE,
    max_conns  INTEGER NOT NULL DEFAULT 20,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS usenet_indexers (
    user_id    TEXT PRIMARY KEY,
    url        TEXT NOT NULL,
    api_key    TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS usenet_indexer_list (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    label      TEXT NOT NULL DEFAULT '',
    url        TEXT NOT NULL,
    api_key    TEXT NOT NULL DEFAULT '',
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_usenet_indexer_list_user ON usenet_indexer_list(user_id);
INSERT INTO usenet_indexer_list (id, user_id, label, url, api_key, enabled, created_at, updated_at)
SELECT md5(user_id), user_id, '', url, api_key, TRUE, updated_at, updated_at
FROM usenet_indexers
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS rd_library (
    info_hash TEXT NOT NULL,
    user_id   TEXT NOT NULL,
    filename  TEXT NOT NULL DEFAULT '',
    filesize  BIGINT NOT NULL DEFAULT 0,
    synced_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (info_hash, user_id)
);

CREATE TABLE IF NOT EXISTS tb_library (
    info_hash TEXT NOT NULL,
    user_id   TEXT NOT NULL,
    filename  TEXT NOT NULL DEFAULT '',
    filesize  BIGINT NOT NULL DEFAULT 0,
    synced_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (info_hash, user_id)
);

CREATE TABLE IF NOT EXISTS reseller_codes (
    code        TEXT PRIMARY KEY,
    plan_id     TEXT NOT NULL,
    period      TEXT NOT NULL,
    days        INTEGER NOT NULL DEFAULT 0,
    reseller    TEXT NOT NULL DEFAULT '',
    redeemed_by TEXT NOT NULL DEFAULT '',
    redeemed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE reseller_codes ADD COLUMN IF NOT EXISTS settled_at     TIMESTAMPTZ;
ALTER TABLE reseller_codes ADD COLUMN IF NOT EXISTS settlement_ref TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS ad_library (
    info_hash TEXT PRIMARY KEY,
    filename  TEXT NOT NULL DEFAULT '',
    filesize  BIGINT NOT NULL DEFAULT 0,
    synced_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS rss_feeds (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    url        TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    filter     TEXT NOT NULL DEFAULT '',
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    last_check TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_rss_feeds_user ON rss_feeds(user_id);
ALTER TABLE rss_feeds ADD COLUMN IF NOT EXISTS criteria JSONB NOT NULL DEFAULT '{}';

CREATE TABLE IF NOT EXISTS rss_seen (
    feed_id TEXT NOT NULL,
    guid    TEXT NOT NULL,
    PRIMARY KEY (feed_id, guid)
);

CREATE TABLE IF NOT EXISTS telegram_links (
    tg_user_id BIGINT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS telegram_link_codes (
    code       TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS seed_slot_packs INT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS seed_slot_sub TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS system_indexer_enabled BOOLEAN NOT NULL DEFAULT TRUE;

CREATE TABLE IF NOT EXISTS cairn_archives (
    info_hash  TEXT PRIMARY KEY,
    nzb_key    TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    size       BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_cairns (
    user_id    TEXT NOT NULL,
    info_hash  TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, info_hash)
);

CREATE TABLE IF NOT EXISTS debrid_usage (
    user_id  TEXT NOT NULL,
    provider TEXT NOT NULL,
    month    TEXT NOT NULL,
    bytes    BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, provider, month)
);

ALTER TABLE cairn_archives ADD COLUMN IF NOT EXISTS nzb BYTEA;

CREATE TABLE IF NOT EXISTS job_nzbs (
    info_hash  TEXT PRIMARY KEY,
    nzb        BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS usenet_tombstones (
    user_id   TEXT NOT NULL,
    info_hash TEXT NOT NULL,
    until     TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, info_hash)
);
