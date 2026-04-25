package retrieval

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qq900306ss/slack-ai-assistant/internal/config"
	"github.com/qq900306ss/slack-ai-assistant/internal/embedding"
)

// Client provides a high-level interface for searching messages.
type Client struct {
	searcher  *Searcher
	embedder  *embedding.OpenAIClient
	logger    *slog.Logger
}

// NewClient creates a new retrieval client.
func NewClient(pool *pgxpool.Pool, cfg *config.Config, logger *slog.Logger) *Client {
	var embedder *embedding.OpenAIClient
	if cfg.OpenAIAPIKey != "" {
		embedder = embedding.NewOpenAIClient(cfg.OpenAIAPIKey, cfg.EmbeddingModel)
	}

	return &Client{
		searcher: NewSearcher(pool),
		embedder: embedder,
		logger:   logger,
	}
}

// Search performs a hybrid search (vector + full-text) for the given query.
func (c *Client) Search(ctx context.Context, query string, filter SearchFilter) ([]SearchResult, error) {
	// If we have an embedder, do hybrid search
	if c.embedder != nil {
		embeddings, err := c.embedder.Embed(ctx, []string{query})
		if err != nil {
			c.logger.Warn("embedding failed, falling back to full-text", "error", err)
			return c.searcher.FullTextSearch(ctx, query, filter)
		}

		if len(embeddings) > 0 && embeddings[0] != nil {
			return c.searcher.HybridSearch(ctx, query, embeddings[0], filter)
		}
	}

	// Fallback to full-text only
	return c.searcher.FullTextSearch(ctx, query, filter)
}

// VectorSearch performs semantic search only.
func (c *Client) VectorSearch(ctx context.Context, query string, filter SearchFilter) ([]SearchResult, error) {
	if c.embedder == nil {
		return nil, nil
	}

	embeddings, err := c.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}

	if len(embeddings) == 0 || embeddings[0] == nil {
		return nil, nil
	}

	return c.searcher.VectorSearch(ctx, embeddings[0], filter)
}

// FullTextSearch performs keyword search only.
func (c *Client) FullTextSearch(ctx context.Context, query string, filter SearchFilter) ([]SearchResult, error) {
	return c.searcher.FullTextSearch(ctx, query, filter)
}

// GetRecentMessages retrieves the most recent messages from a channel.
func (c *Client) GetRecentMessages(ctx context.Context, channelID string, limit int) ([]SearchResult, error) {
	query := `
		SELECT m.id, m.channel_id, COALESCE(c.name, '') as channel_name,
		       m.slack_ts, COALESCE(m.thread_ts, '') as thread_ts,
		       COALESCE(m.user_id, '') as user_id, COALESCE(u.name, u.display_name, '') as user_name,
		       COALESCE(m.text, '') as text, m.created_at,
		       0.0 as score
		FROM messages m
		LEFT JOIN channels c ON m.channel_id = c.id
		LEFT JOIN users u ON m.user_id = u.id
		WHERE m.channel_id = $1
		  AND m.deleted_at IS NULL
		  AND m.text IS NOT NULL AND m.text != ''
		ORDER BY m.created_at DESC
		LIMIT $2
	`

	return c.searcher.executeSearch(ctx, query, []any{channelID, limit})
}

// GetThread retrieves all messages in a thread.
func (c *Client) GetThread(ctx context.Context, channelID, threadTS string) ([]SearchResult, error) {
	filter := SearchFilter{
		ChannelIDs: []string{channelID},
		ThreadTS:   threadTS,
		Limit:      100,
	}

	query := `
		SELECT m.id, m.channel_id, COALESCE(c.name, '') as channel_name,
		       m.slack_ts, COALESCE(m.thread_ts, '') as thread_ts,
		       COALESCE(m.user_id, '') as user_id, COALESCE(u.name, '') as user_name,
		       COALESCE(m.text, '') as text, m.created_at,
		       0.0 as score
		FROM messages m
		LEFT JOIN channels c ON m.channel_id = c.id
		LEFT JOIN users u ON m.user_id = u.id
		WHERE m.channel_id = $1
		  AND (m.thread_ts = $2 OR m.slack_ts = $2)
		  AND m.deleted_at IS NULL
		ORDER BY m.slack_ts ASC
		LIMIT $3
	`

	return c.searcher.executeSearch(ctx, query, []any{channelID, threadTS, filter.Limit})
}
