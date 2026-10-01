CREATE TABLE IF NOT EXISTS feeds (
    id                TEXT NOT NULL PRIMARY KEY,
    item_id           TEXT NOT NULL DEFAULT '',
    provider          TEXT NOT NULL DEFAULT '',
    link_type         TEXT NOT NULL DEFAULT '',
    item_url          TEXT NOT NULL DEFAULT '',
    title             TEXT NOT NULL DEFAULT '',
    description       TEXT NOT NULL DEFAULT '',
    author            TEXT NOT NULL DEFAULT '',
    cover_art         TEXT NOT NULL DEFAULT '',
    pub_date          DATETIME,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS episodes (
    feed_id       TEXT NOT NULL,
    id            TEXT NOT NULL,
    title         TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    thumbnail     TEXT NOT NULL DEFAULT '',
    video_url     TEXT NOT NULL DEFAULT '',
    pub_date      DATETIME,
    duration      INTEGER NOT NULL DEFAULT 0,
    episode_order INTEGER NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new','downloaded','error','cleaned')),
    size          INTEGER NOT NULL DEFAULT 0,
    object_key    TEXT NOT NULL DEFAULT '',
    last_attempt_at DATETIME NULL,
    downloaded_at DATETIME NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (feed_id, id),
    FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_episodes_feed_date ON episodes(feed_id, pub_date, id);
CREATE INDEX IF NOT EXISTS idx_episodes_failure_time ON episodes(status, last_attempt_at);
