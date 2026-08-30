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
    last_access       DATETIME,
    expiration_time   DATETIME,
    format            TEXT NOT NULL DEFAULT '',
    quality           TEXT NOT NULL DEFAULT '',
    cover_art_quality TEXT NOT NULL DEFAULT '',
    page_size         INTEGER NOT NULL DEFAULT 0,
    playlist_sort     TEXT NOT NULL DEFAULT '',
    private_feed      INTEGER NOT NULL DEFAULT 0,
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
    episode_order TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT '',
    size          INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (feed_id, id)
);

CREATE INDEX IF NOT EXISTS idx_episodes_feed_id ON episodes(feed_id);
