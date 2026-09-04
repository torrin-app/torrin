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
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS season INT NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS episode INT NOT NULL DEFAULT 0;

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

CREATE TABLE IF NOT EXISTS cold_pulls (
    user_id    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cold_pulls_user_time ON cold_pulls(user_id, created_at);

-- A job is one user's account record for one content hash. Older application
-- versions created a row per playback/episode, so archive and consolidate those
-- rows before enforcing the invariant. The archive makes this migration
-- recoverable without keeping duplicate rows visible or runnable.
CREATE TABLE IF NOT EXISTS job_dedup_archive (
    id            TEXT PRIMARY KEY,
    kept_job_id   TEXT NOT NULL,
    row_data      JSONB NOT NULL,
    archived_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One-time dedup + unique index. Guarded on the index so only the first boot
-- after deploy does the heavy work; once it exists, every later boot skips the
-- ranking scan and the write-blocking lock entirely.
DO $mig$
DECLARE
    mapping RECORD;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_jobs_one_live_user_hash') THEN
        RETURN;
    END IF;

-- Close the cleanup/index race with older API replicas. Reads continue, while
-- job writes pause for this transaction and resume once uniqueness is active.
LOCK TABLE jobs IN SHARE ROW EXCLUSIVE MODE;

DROP TABLE IF EXISTS job_dedup_map;
CREATE TEMP TABLE job_dedup_map AS
WITH ranked AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, lower(info_hash)
               ORDER BY
                   CASE status
                       WHEN 'complete' THEN 0
                       WHEN 'seeding' THEN 1
                       WHEN 'publishing' THEN 2
                       WHEN 'processing' THEN 3
                       WHEN 'downloading' THEN 4
                       WHEN 'pending' THEN 5
                       WHEN 'queued' THEN 6
                       ELSE 7
                   END,
                   CASE WHEN jsonb_typeof(files)='array' AND files<>'[]'::jsonb THEN 0 ELSE 1 END,
                   updated_at DESC,
                   created_at DESC,
                   id DESC
           ) AS keeper_id,
           row_number() OVER (
               PARTITION BY user_id, lower(info_hash)
               ORDER BY
                   CASE status
                       WHEN 'complete' THEN 0
                       WHEN 'seeding' THEN 1
                       WHEN 'publishing' THEN 2
                       WHEN 'processing' THEN 3
                       WHEN 'downloading' THEN 4
                       WHEN 'pending' THEN 5
                       WHEN 'queued' THEN 6
                       ELSE 7
                   END,
                   CASE WHEN jsonb_typeof(files)='array' AND files<>'[]'::jsonb THEN 0 ELSE 1 END,
                   updated_at DESC,
                   created_at DESC,
                   id DESC
           ) AS row_num
    FROM jobs
    WHERE user_id NOT IN ('', 'system', 'prewarm')
      AND info_hash <> ''
      AND seed = false
      AND status NOT IN ('failed', 'evicted')
)
SELECT id AS loser_id, keeper_id
FROM ranked
WHERE row_num > 1;

INSERT INTO job_dedup_archive (id, kept_job_id, row_data)
SELECT j.id, m.keeper_id, to_jsonb(j)
FROM jobs j
JOIN job_dedup_map m ON m.loser_id = j.id
ON CONFLICT (id) DO NOTHING;

-- Preserve useful metadata on the retained row when only a duplicate had it.
UPDATE jobs keeper
SET imdb_id = CASE WHEN keeper.imdb_id='' THEN loser.imdb_id ELSE keeper.imdb_id END,
    name = CASE WHEN keeper.name='' THEN loser.name ELSE keeper.name END,
    files = CASE WHEN keeper.files='[]'::jsonb THEN loser.files ELSE keeper.files END,
    file_size = CASE WHEN keeper.file_size=0 THEN loser.file_size ELSE keeper.file_size END,
    node = CASE WHEN keeper.node='' THEN loser.node ELSE keeper.node END,
    updated_at = GREATEST(keeper.updated_at, loser.updated_at)
FROM job_dedup_map m
JOIN jobs loser ON loser.id = m.loser_id
WHERE keeper.id = m.keeper_id;

-- These tables intentionally have no foreign keys. Move a duplicate's
-- auxiliary state to the retained job, discarding it only when the retained job
-- already has equivalent state.
FOR mapping IN SELECT loser_id, keeper_id FROM job_dedup_map LOOP
        IF EXISTS (SELECT 1 FROM byos_objects WHERE job_id=mapping.keeper_id) THEN
            DELETE FROM byos_objects WHERE job_id=mapping.loser_id;
        ELSE
            UPDATE byos_objects SET job_id=mapping.keeper_id WHERE job_id=mapping.loser_id;
        END IF;

        IF EXISTS (SELECT 1 FROM byos_queue WHERE job_id=mapping.keeper_id) THEN
            DELETE FROM byos_queue WHERE job_id=mapping.loser_id;
        ELSE
            UPDATE byos_queue SET job_id=mapping.keeper_id WHERE job_id=mapping.loser_id;
        END IF;

        IF EXISTS (SELECT 1 FROM prewarm_fallbacks WHERE job_id=mapping.keeper_id) THEN
            DELETE FROM prewarm_fallbacks WHERE job_id=mapping.loser_id;
        ELSE
            UPDATE prewarm_fallbacks SET job_id=mapping.keeper_id WHERE job_id=mapping.loser_id;
        END IF;
    END LOOP;

DELETE FROM jobs j
USING job_dedup_map m
WHERE j.id = m.loser_id;

DROP TABLE job_dedup_map;

-- Failed and evicted history can coexist with a current row. Seed jobs and
-- internal system/prewarm rows have separate operational lifecycles.
CREATE UNIQUE INDEX idx_jobs_one_live_user_hash
ON jobs(user_id, lower(info_hash))
WHERE user_id NOT IN ('', 'system', 'prewarm')
  AND info_hash <> ''
  AND seed = false
  AND status NOT IN ('failed', 'evicted');

END $mig$;
