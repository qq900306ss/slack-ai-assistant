DROP TRIGGER IF EXISTS messages_text_search_update ON messages;
DROP FUNCTION IF EXISTS messages_text_search_trigger();
DROP INDEX IF EXISTS idx_messages_text_search;
DROP INDEX IF EXISTS idx_embeddings_vector;
ALTER TABLE messages DROP COLUMN IF EXISTS text_search;
