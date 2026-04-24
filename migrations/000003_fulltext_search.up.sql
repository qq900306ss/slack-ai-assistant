-- Add full-text search column
ALTER TABLE messages ADD COLUMN IF NOT EXISTS text_search tsvector;

-- Generate tsvector for existing messages
UPDATE messages SET text_search = to_tsvector('simple', COALESCE(text, ''));

-- Create GIN index for fast full-text search
CREATE INDEX IF NOT EXISTS idx_messages_text_search ON messages USING GIN(text_search);

-- Create trigger to auto-update tsvector on insert/update
CREATE OR REPLACE FUNCTION messages_text_search_trigger() RETURNS trigger AS $$
BEGIN
  NEW.text_search := to_tsvector('simple', COALESCE(NEW.text, ''));
  RETURN NEW;
END
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS messages_text_search_update ON messages;
CREATE TRIGGER messages_text_search_update
  BEFORE INSERT OR UPDATE OF text ON messages
  FOR EACH ROW EXECUTE FUNCTION messages_text_search_trigger();

-- Create index for vector similarity search (if not exists)
CREATE INDEX IF NOT EXISTS idx_embeddings_vector ON message_embeddings USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);
