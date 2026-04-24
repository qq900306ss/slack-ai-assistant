ALTER TABLE message_embeddings DROP COLUMN IF EXISTS embedding;
ALTER TABLE message_embeddings ADD COLUMN embedding VECTOR(1024);
