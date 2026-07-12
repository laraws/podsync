PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER NOT NULL PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Feed identity & source information (core entity)
CREATE TABLE IF NOT EXISTS feeds (
    id         TEXT NOT NULL PRIMARY KEY,
    item_id    TEXT NOT NULL DEFAULT '',
    provider   TEXT NOT NULL DEFAULT '',
    link_type  TEXT NOT NULL DEFAULT '',
    item_url   TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Feed content metadata fetched from the platform API (1:1 with feeds)
CREATE TABLE IF NOT EXISTS feed_metadata (
    feed_id         TEXT NOT NULL PRIMARY KEY,
    title           TEXT NOT NULL DEFAULT '',
    description     TEXT NOT NULL DEFAULT '',
    author          TEXT NOT NULL DEFAULT '',
    cover_art       TEXT NOT NULL DEFAULT '',
    pub_date        DATETIME,
    last_access     DATETIME,
    expiration_time DATETIME,
    FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE
);

-- Feed download/configuration settings (1:1 with feeds)
CREATE TABLE IF NOT EXISTS feed_settings (
    feed_id           TEXT NOT NULL PRIMARY KEY,
    format            TEXT NOT NULL DEFAULT '',
    quality           TEXT NOT NULL DEFAULT '',
    cover_art_quality TEXT NOT NULL DEFAULT '',
    page_size         INTEGER NOT NULL DEFAULT 0,
    playlist_sort     TEXT NOT NULL DEFAULT '',
    private_feed      INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE
);

-- Episode content metadata (from platform API)
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
    PRIMARY KEY (feed_id, id),
    FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE
);

-- Episode download state (1:1 with episodes, frequently updated)
CREATE TABLE IF NOT EXISTS episode_states (
    feed_id    TEXT NOT NULL,
    episode_id TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT '',
    size       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (feed_id, episode_id),
    FOREIGN KEY (feed_id, episode_id) REFERENCES episodes(feed_id, id) ON DELETE CASCADE
);

INSERT OR IGNORE INTO schema_migrations (version) VALUES (2);
