CREATE TABLE IF NOT EXISTS orisun_last_published_event_position
(
    boundary       TEXT PRIMARY KEY,
    transaction_id BIGINT NOT NULL DEFAULT 0,
    global_id      BIGINT NOT NULL DEFAULT 0,
    date_created   TIMESTAMPTZ     DEFAULT NOW() NOT NULL,
    date_updated   TIMESTAMPTZ     DEFAULT NOW() NOT NULL
);

CREATE TABLE IF NOT EXISTS events_count
(
    id          VARCHAR(255) PRIMARY KEY,
    event_count VARCHAR(255) NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS projector_checkpoint
(
    id               VARCHAR(255) PRIMARY KEY,
    name             VARCHAR(255) UNIQUE NOT NULL,
    commit_position  BIGINT              NOT NULL,
    prepare_position BIGINT              NOT NULL
);