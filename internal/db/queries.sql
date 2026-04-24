-- name: UpsertChannel :exec
INSERT INTO channels (id, name, is_private, is_archived)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    is_private = EXCLUDED.is_private,
    is_archived = EXCLUDED.is_archived,
    updated_at = NOW();

-- name: GetChannel :one
SELECT * FROM channels WHERE id = $1;

-- name: ListChannels :many
SELECT * FROM channels WHERE is_archived = FALSE ORDER BY name;

-- name: UpdateChannelLastIngestedTS :exec
UPDATE channels SET last_ingested_ts = $2, updated_at = NOW() WHERE id = $1;

-- name: UpsertUser :exec
INSERT INTO users (id, name, display_name, real_name, is_bot)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    display_name = EXCLUDED.display_name,
    real_name = EXCLUDED.real_name,
    is_bot = EXCLUDED.is_bot,
    updated_at = NOW();

-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: InsertMessage :one
INSERT INTO messages (channel_id, slack_ts, thread_ts, user_id, text, raw_json)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (channel_id, slack_ts) DO NOTHING
RETURNING id;

-- name: UpdateMessage :exec
UPDATE messages SET
    text = $3,
    raw_json = $4,
    updated_at = NOW()
WHERE channel_id = $1 AND slack_ts = $2;

-- name: SoftDeleteMessage :exec
UPDATE messages SET deleted_at = NOW(), updated_at = NOW()
WHERE channel_id = $1 AND slack_ts = $2;

-- name: GetMessageBySlackTS :one
SELECT * FROM messages WHERE channel_id = $1 AND slack_ts = $2;

-- name: GetMessageByID :one
SELECT * FROM messages WHERE id = $1;

-- name: UpsertReaction :exec
INSERT INTO message_reactions (message_id, emoji, user_id)
VALUES ($1, $2, $3)
ON CONFLICT (message_id, emoji, user_id) DO NOTHING;

-- name: DeleteReaction :exec
DELETE FROM message_reactions
WHERE message_id = $1 AND emoji = $2 AND user_id = $3;

-- name: DeleteEmbedding :exec
DELETE FROM message_embeddings WHERE message_id = $1;

-- name: GetIngestState :one
SELECT * FROM ingest_state WHERE channel_id = $1;

-- name: UpsertIngestState :exec
INSERT INTO ingest_state (channel_id, oldest_ts_fetched, newest_ts_fetched, backfill_done)
VALUES ($1, $2, $3, $4)
ON CONFLICT (channel_id) DO UPDATE SET
    oldest_ts_fetched = COALESCE(EXCLUDED.oldest_ts_fetched, ingest_state.oldest_ts_fetched),
    newest_ts_fetched = COALESCE(EXCLUDED.newest_ts_fetched, ingest_state.newest_ts_fetched),
    backfill_done = EXCLUDED.backfill_done,
    updated_at = NOW();

-- name: ListChannelsNeedingBackfill :many
SELECT c.* FROM channels c
LEFT JOIN ingest_state s ON c.id = s.channel_id
WHERE c.is_archived = FALSE AND (s.backfill_done IS NULL OR s.backfill_done = FALSE);

-- name: ListMessagesNeedingEmbedding :many
SELECT m.id, m.channel_id, m.slack_ts, m.text, m.thread_ts
FROM messages m
LEFT JOIN message_embeddings e ON m.id = e.message_id
WHERE m.deleted_at IS NULL
  AND m.text IS NOT NULL
  AND m.text != ''
  AND e.message_id IS NULL
ORDER BY m.created_at ASC
LIMIT $1;

-- name: InsertEmbedding :exec
INSERT INTO message_embeddings (message_id, embedding, model)
VALUES ($1, $2, $3)
ON CONFLICT (message_id) DO UPDATE SET
    embedding = EXCLUDED.embedding,
    model = EXCLUDED.model,
    created_at = NOW();

-- name: CountMessagesNeedingEmbedding :one
SELECT COUNT(*) FROM messages m
LEFT JOIN message_embeddings e ON m.id = e.message_id
WHERE m.deleted_at IS NULL
  AND m.text IS NOT NULL
  AND m.text != ''
  AND e.message_id IS NULL;
