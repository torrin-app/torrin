CREATE TABLE IF NOT EXISTS jobs (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL DEFAULT '',
    info_hash     TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    magnet        TEXT NOT NULL DEFAULT '',
    source        TEXT NOT NULL,
    status        TEXT NOT NULL,
    error         TEXT NOT NULL DEFAULT '',
    files         JSONB NOT NULL DEFAULT '[]',
    selected_idxs JSONB NOT NULL DEFAULT '[]',
    imdb_id       TEXT NOT NULL DEFAULT '',
    title_norm    TEXT NOT NULL DEFAULT '',
    file_size     BIGINT NOT NULL DEFAULT 0,
    max_bytes     BIGINT NOT NULL DEFAULT 0,
    priority         INT NOT NULL DEFAULT 0,
    progress         DOUBLE PRECISION NOT NULL DEFAULT 0,
    dl_speed         BIGINT NOT NULL DEFAULT 0,
    access_count     INTEGER NOT NULL DEFAULT 0,
    last_accessed_at TIMESTAMPTZ,
    node             TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL
);

-- migrate already-created DBs (CREATE TABLE IF NOT EXISTS won't add new columns)
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS dl_speed BIGINT NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS node TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_jobs_info_hash ON jobs(info_hash);
CREATE INDEX IF NOT EXISTS idx_jobs_user_id ON jobs(user_id);
CREATE INDEX IF NOT EXISTS idx_jobs_user_created ON jobs(user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_status_priority ON jobs(status, priority DESC, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_jobs_imdb_id ON jobs(imdb_id);
CREATE INDEX IF NOT EXISTS idx_jobs_title_norm ON jobs(title_norm);

-- prewarm_fallbacks holds the still-untried candidate hashes for a pre-warm job,
-- so a sweep can advance to the next release when the active one fails.
CREATE TABLE IF NOT EXISTS prewarm_fallbacks (
    job_id     TEXT PRIMARY KEY,
    imdb_id    TEXT NOT NULL DEFAULT '',
    name       TEXT NOT NULL DEFAULT '',
    remaining  JSONB NOT NULL DEFAULT '[]',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS views (
    info_hash TEXT NOT NULL,
    user_id   TEXT NOT NULL,
    viewed_on DATE NOT NULL DEFAULT CURRENT_DATE,
    PRIMARY KEY (info_hash, user_id, viewed_on)
);

CREATE TABLE IF NOT EXISTS metrics_snapshots (
    date         DATE PRIMARY KEY,
    cached_count INTEGER NOT NULL DEFAULT 0,
    cached_size  BIGINT  NOT NULL DEFAULT 0,
    total_views  BIGINT  NOT NULL DEFAULT 0,
    total_users  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS byos_objects (
    job_id       TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL,
    bucket       TEXT NOT NULL,
    info_hash    TEXT NOT NULL,
    name         TEXT NOT NULL DEFAULT '',
    streams_json JSONB NOT NULL DEFAULT '[]',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_byos_objects_userhash ON byos_objects(user_id, info_hash);

CREATE TABLE IF NOT EXISTS byos_queue (
    job_id     TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DO $$
BEGIN
    IF EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'hdencode_links')
       AND NOT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'release_links') THEN
        ALTER TABLE hdencode_links RENAME TO release_links;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS release_links (
    info_hash  TEXT PRIMARY KEY,
    post_url   TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    size       BIGINT NOT NULL DEFAULT 0,
    source     TEXT NOT NULL DEFAULT 'hdencode',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE release_links ADD COLUMN IF NOT EXISTS size BIGINT NOT NULL DEFAULT 0;
ALTER TABLE release_links ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'hdencode';

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS seed BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS blobs (
    content_key TEXT PRIMARY KEY,
    size        BIGINT NOT NULL DEFAULT 0,
    encrypted   BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS blob_refs (
    info_hash   TEXT NOT NULL,
    file_index  INT NOT NULL,
    content_key TEXT NOT NULL,
    PRIMARY KEY (info_hash, file_index)
);
CREATE INDEX IF NOT EXISTS idx_blob_refs_content_key ON blob_refs(content_key);

CREATE TABLE IF NOT EXISTS nzb_url_hashes (
    url_hash     TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_nzb_url_hashes_content ON nzb_url_hashes(content_hash);
