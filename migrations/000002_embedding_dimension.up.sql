-- Change embedding dimension to 1536 for OpenAI text-embedding-3-small
ALTER TABLE message_embeddings DROP COLUMN IF EXISTS embedding;
ALTER TABLE message_embeddings ADD COLUMN embedding VECTOR(1536);
