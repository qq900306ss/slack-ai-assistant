package embedding

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"github.com/qq900306ss/slack-ai-assistant/internal/config"
	"github.com/qq900306ss/slack-ai-assistant/internal/db"
)

// EmbeddingClient is the interface for embedding providers.
type EmbeddingClient interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
}

// Worker processes messages and generates embeddings.
type Worker struct {
	client    EmbeddingClient
	queries   *db.Queries
	pool      *pgxpool.Pool
	cfg       *config.Config
	logger    *slog.Logger
	batchSize int
}

// NewWorker creates an embedding worker.
func NewWorker(pool *pgxpool.Pool, cfg *config.Config, logger *slog.Logger) *Worker {
	client := NewOpenAIClient(cfg.OpenAIAPIKey, cfg.EmbeddingModel)
	return &Worker{
		client:    client,
		queries:   db.New(pool),
		pool:      pool,
		cfg:       cfg,
		logger:    logger,
		batchSize: cfg.EmbeddingBatchSize,
	}
}

// Run continuously processes messages needing embeddings.
func (w *Worker) Run(ctx context.Context) error {
	w.logger.Info("embedding worker started", "model", w.cfg.EmbeddingModel, "batch_size", w.batchSize)

	count, err := w.queries.CountMessagesNeedingEmbedding(ctx)
	if err != nil {
		w.logger.Error("failed to count messages", "error", err)
	} else {
		w.logger.Info("messages needing embedding", "count", count)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("embedding worker stopped")
			return ctx.Err()
		case <-ticker.C:
			processed, err := w.processBatch(ctx)
			if err != nil {
				w.logger.Error("failed to process batch", "error", err)
				time.Sleep(30 * time.Second)
				continue
			}
			if processed > 0 {
				w.logger.Info("embedded messages", "count", processed)
				if processed >= w.batchSize {
					ticker.Reset(100 * time.Millisecond)
				}
			} else {
				ticker.Reset(10 * time.Second)
			}
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) (int, error) {
	messages, err := w.queries.ListMessagesNeedingEmbedding(ctx, int32(w.batchSize))
	if err != nil {
		return 0, err
	}

	if len(messages) == 0 {
		return 0, nil
	}

	texts := make([]string, len(messages))
	for i, msg := range messages {
		texts[i] = prepareText(msg)
	}

	embeddings, err := w.client.Embed(ctx, texts)
	if err != nil {
		return 0, err
	}

	for i, msg := range messages {
		if i >= len(embeddings) || embeddings[i] == nil {
			w.logger.Warn("missing embedding for message", "id", msg.ID)
			continue
		}

		vec := pgvector.NewVector(embeddings[i])
		err := w.queries.InsertEmbedding(ctx, db.InsertEmbeddingParams{
			MessageID: msg.ID,
			Embedding: vec,
			Model:     w.client.Model(),
		})
		if err != nil {
			w.logger.Error("failed to insert embedding", "message_id", msg.ID, "error", err)
			continue
		}
	}

	return len(messages), nil
}

func prepareText(msg db.ListMessagesNeedingEmbeddingRow) string {
	return db.TextValue(msg.Text)
}
