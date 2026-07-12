CREATE TABLE IF NOT EXISTS schema_migrations (
    version BIGINT NOT NULL PRIMARY KEY,
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Feed identity & source information (core entity)
CREATE TABLE IF NOT EXISTS feeds (
    id         VARCHAR(255) NOT NULL PRIMARY KEY,
    item_id    VARCHAR(255) NOT NULL DEFAULT '',
    provider   VARCHAR(50)  NOT NULL DEFAULT '',
    link_type  VARCHAR(50)  NOT NULL DEFAULT '',
    item_url   VARCHAR(2048) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Feed content metadata fetched from the platform API (1:1 with feeds)
CREATE TABLE IF NOT EXISTS feed_metadata (
    feed_id         VARCHAR(255) NOT NULL PRIMARY KEY,
    title           VARCHAR(512) NOT NULL DEFAULT '',
    description     TEXT,
    author          VARCHAR(255) NOT NULL DEFAULT '',
    cover_art       VARCHAR(2048) NOT NULL DEFAULT '',
    pub_date        DATETIME NULL,
    last_access     DATETIME NULL,
    expiration_time DATETIME NULL,
    CONSTRAINT fk_feed_metadata_feed FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Feed download/configuration settings (1:1 with feeds)
CREATE TABLE IF NOT EXISTS feed_settings (
    feed_id           VARCHAR(255) NOT NULL PRIMARY KEY,
    format            VARCHAR(50)  NOT NULL DEFAULT '',
    quality           VARCHAR(50)  NOT NULL DEFAULT '',
    cover_art_quality VARCHAR(50)  NOT NULL DEFAULT '',
    page_size         INT NOT NULL DEFAULT 0,
    playlist_sort     VARCHAR(50)  NOT NULL DEFAULT '',
    private_feed      TINYINT(1) NOT NULL DEFAULT 0,
    CONSTRAINT fk_feed_settings_feed FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Episode content metadata (from platform API)
CREATE TABLE IF NOT EXISTS episodes (
    feed_id       VARCHAR(255) NOT NULL,
    id            VARCHAR(255) NOT NULL,
    title         VARCHAR(512) NOT NULL DEFAULT '',
    description   TEXT,
    thumbnail     VARCHAR(2048) NOT NULL DEFAULT '',
    video_url     VARCHAR(2048) NOT NULL DEFAULT '',
    pub_date      DATETIME NULL,
    duration      BIGINT NOT NULL DEFAULT 0,
    episode_order VARCHAR(50)  NOT NULL DEFAULT '',
    PRIMARY KEY (feed_id, id),
    CONSTRAINT fk_episodes_feed FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Episode download state (1:1 with episodes, frequently updated)
CREATE TABLE IF NOT EXISTS episode_states (
    feed_id    VARCHAR(255) NOT NULL,
    episode_id VARCHAR(255) NOT NULL,
    status     VARCHAR(50)  NOT NULL DEFAULT '',
    size       BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (feed_id, episode_id),
    CONSTRAINT fk_episode_states_episode FOREIGN KEY (feed_id, episode_id) REFERENCES episodes(feed_id, id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO schema_migrations (version) VALUES (2);
