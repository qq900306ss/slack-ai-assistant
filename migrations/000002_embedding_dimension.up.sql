-- Change embedding dimension from 1024 to 512 for voyage-3-lite
-- Must drop and recreate since ALTER COLUMN doesn't work with vector dimensions
ALTER TABLE message_embeddings DROP COLUMN IF EXISTS embedding;
ALTER TABLE message_embeddings ADD COLUMN embedding VECTOR(512);
