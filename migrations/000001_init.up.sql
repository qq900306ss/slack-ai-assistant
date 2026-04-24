-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Channels
CREATE TABLE channels (
    id TEXT PRIMARY KEY,
    name TEXT,
    is_private BOOLEAN NOT NULL DEFAULT FALSE,
    is_archived BOOLEAN NOT NULL DEFAULT FALSE,
    last_ingested_ts TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Users
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    name TEXT,
    display_name TEXT,
    real_name TEXT,
    is_bot BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Messages
CREATE TABLE messages (
    id BIGSERIAL PRIMARY KEY,
    channel_id TEXT NOT NULL REFERENCES channels(id),
    slack_ts TEXT NOT NULL,
    thread_ts TEXT,
    user_id TEXT REFERENCES users(id),
    text TEXT,
    raw_json JSONB NOT NULL,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (channel_id, slack_ts)
);

CREATE INDEX idx_messages_channel_ts ON messages (channel_id, slack_ts DESC);
CREATE INDEX idx_messages_thread ON messages (thread_ts) WHERE thread_ts IS NOT NULL;
CREATE INDEX idx_messages_user ON messages (user_id);

-- Message embeddings (M2, schema placeholder)
CREATE TABLE message_embeddings (
    message_id BIGINT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    embedding VECTOR(1024),
    model TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Reactions
CREATE TABLE message_reactions (
    message_id BIGINT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    emoji TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (message_id, emoji, user_id)
);

CREATE INDEX idx_reactions_emoji ON message_reactions (emoji);

-- Ingest state for backfill resumption
CREATE TABLE ingest_state (
    channel_id TEXT PRIMARY KEY REFERENCES channels(id),
    oldest_ts_fetched TEXT,
    newest_ts_fetched TEXT,
    backfill_done BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
