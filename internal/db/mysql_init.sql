CREATE TABLE IF NOT EXISTS feeds (
    id                VARCHAR(128) COLLATE utf8mb4_bin NOT NULL PRIMARY KEY,
    item_id           VARCHAR(255) NOT NULL DEFAULT '',
    provider          VARCHAR(50) NOT NULL DEFAULT '',
    link_type         VARCHAR(50) NOT NULL DEFAULT '',
    item_url          VARCHAR(2048) NOT NULL DEFAULT '',
    title             VARCHAR(512) NOT NULL DEFAULT '',
    description       TEXT NOT NULL,
    author            VARCHAR(255) NOT NULL DEFAULT '',
    cover_art         VARCHAR(2048) NOT NULL DEFAULT '',
    pub_date          DATETIME(6) NULL,
    created_at        DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at        DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS episodes (
    feed_id       VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
    id            VARCHAR(255) COLLATE utf8mb4_bin NOT NULL,
    title         VARCHAR(512) NOT NULL DEFAULT '',
    description   TEXT NOT NULL,
    thumbnail     VARCHAR(2048) NOT NULL DEFAULT '',
    video_url     VARCHAR(2048) NOT NULL DEFAULT '',
    pub_date      DATETIME(6) NULL,
    source_published_at DATETIME(6) NULL,
    duration      BIGINT NOT NULL DEFAULT 0,
    episode_order BIGINT NOT NULL DEFAULT 0,
    status        VARCHAR(16) NOT NULL DEFAULT 'new' CHECK (status IN ('new','downloaded','error','cleaned')),
    size          BIGINT NOT NULL DEFAULT 0,
    object_key    VARCHAR(1024) NOT NULL DEFAULT '',
    last_attempt_at DATETIME(6) NULL,
    downloaded_at DATETIME(6) NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error LONGTEXT NOT NULL,
    PRIMARY KEY (feed_id, id),
    INDEX idx_episodes_feed_date (feed_id, pub_date, id),
    INDEX idx_episodes_failure_time (status, last_attempt_at),
    CONSTRAINT fk_episodes_feed FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
