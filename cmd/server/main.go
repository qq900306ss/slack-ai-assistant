package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qq900306ss/slack-ai-assistant/internal/agent"
	"github.com/qq900306ss/slack-ai-assistant/internal/config"
	"github.com/qq900306ss/slack-ai-assistant/internal/embedding"
	"github.com/qq900306ss/slack-ai-assistant/internal/ingest"
	"github.com/qq900306ss/slack-ai-assistant/internal/slack"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	cfg := config.Load()

	if cfg.SlackAppToken == "" || cfg.SlackUserToken == "" {
		logger.Error("SLACK_APP_TOKEN and SLACK_USER_TOKEN are required")
		os.Exit(1)
	}

	if cfg.DatabaseURL == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Connect to database
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to database")

	// Create Slack client for API calls
	slackClient := slack.NewClient(cfg.SlackUserToken, logger)

	// Create event handler
	handler := ingest.NewHandler(pool, cfg, logger)

	// Create bot handler if OpenAI is configured (enables @mention responses)
	if cfg.OpenAIAPIKey != "" {
		botHandler, err := agent.NewBotHandler(pool, slackClient, cfg, logger)
		if err != nil {
			logger.Error("failed to create bot handler", "error", err)
		} else {
			handler.SetBotResponder(botHandler)
			logger.Info("bot handler enabled", "bot_id", botHandler.BotID())
		}
	} else {
		logger.Warn("OPENAI_API_KEY not set, bot mentions disabled")
	}

	// Start backfill in background
	backfiller := ingest.NewBackfiller(slackClient, pool, cfg, logger)
	go func() {
		if err := backfiller.Run(ctx); err != nil && err != context.Canceled {
			logger.Error("backfill error", "error", err)
		}
	}()

	// Start embedding worker if configured
	if cfg.OpenAIAPIKey != "" {
		embeddingWorker := embedding.NewWorker(pool, cfg, logger)
		go func() {
			if err := embeddingWorker.Run(ctx); err != nil && err != context.Canceled {
				logger.Error("embedding worker error", "error", err)
			}
		}()
	} else {
		logger.Warn("OPENAI_API_KEY not set, embedding worker disabled")
	}

	// Start Socket Mode client (blocks)
	smClient := slack.NewSocketModeClient(cfg.SlackAppToken, cfg.SlackUserToken, handler, logger)
	logger.Info("starting socket mode client")

	if err := smClient.Run(ctx); err != nil && err != context.Canceled {
		logger.Error("socket mode error", "error", err)
		os.Exit(1)
	}

	logger.Info("shutdown complete")
}
