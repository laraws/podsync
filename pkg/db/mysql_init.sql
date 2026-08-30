CREATE TABLE IF NOT EXISTS feeds (
    id                VARCHAR(255) NOT NULL PRIMARY KEY,
    item_id           VARCHAR(255) NOT NULL DEFAULT '',
    provider          VARCHAR(50) NOT NULL DEFAULT '',
    link_type         VARCHAR(50) NOT NULL DEFAULT '',
    item_url          VARCHAR(2048) NOT NULL DEFAULT '',
    title             VARCHAR(512) NOT NULL DEFAULT '',
    description       TEXT NOT NULL,
    author            VARCHAR(255) NOT NULL DEFAULT '',
    cover_art         VARCHAR(2048) NOT NULL DEFAULT '',
    pub_date          DATETIME NULL,
    last_access       DATETIME NULL,
    expiration_time   DATETIME NULL,
    format            VARCHAR(50) NOT NULL DEFAULT '',
    quality           VARCHAR(50) NOT NULL DEFAULT '',
    cover_art_quality VARCHAR(50) NOT NULL DEFAULT '',
    page_size         INT NOT NULL DEFAULT 0,
    playlist_sort     VARCHAR(50) NOT NULL DEFAULT '',
    private_feed      TINYINT(1) NOT NULL DEFAULT 0,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS episodes (
    feed_id       VARCHAR(255) NOT NULL,
    id            VARCHAR(255) NOT NULL,
    title         VARCHAR(512) NOT NULL DEFAULT '',
    description   TEXT NOT NULL,
    thumbnail     VARCHAR(2048) NOT NULL DEFAULT '',
    video_url     VARCHAR(2048) NOT NULL DEFAULT '',
    pub_date      DATETIME NULL,
    duration      BIGINT NOT NULL DEFAULT 0,
    episode_order VARCHAR(50) NOT NULL DEFAULT '',
    status        VARCHAR(50) NOT NULL DEFAULT '',
    size          BIGINT NOT NULL DEFAULT 0,
    object_key    VARCHAR(1024) NOT NULL DEFAULT '',
    PRIMARY KEY (feed_id, id),
    INDEX idx_episodes_feed_id (feed_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
