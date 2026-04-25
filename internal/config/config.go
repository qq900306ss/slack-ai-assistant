package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// Slack
	SlackAppToken      string
	SlackUserToken     string
	ExcludedChannelIDs []string
	BackfillDays       int

	// Database
	DatabaseURL string

	// Embedding + Agent (both use OpenAI)
	OpenAIAPIKey       string
	EmbeddingModel     string
	EmbeddingBatchSize int

	// Reindex
	ForceReindex        bool
	PurgeExcludedOnSync bool
}

func Load() *Config {
	backfillDays := 30
	if v := os.Getenv("BACKFILL_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			backfillDays = n
		}
	}

	var excluded []string
	if v := os.Getenv("SLACK_EXCLUDE_CHANNELS"); v != "" {
		for _, id := range strings.Split(v, ",") {
			if id = strings.TrimSpace(id); id != "" {
				excluded = append(excluded, id)
			}
		}
	}

	embeddingModel := os.Getenv("EMBEDDING_MODEL")
	if embeddingModel == "" {
		embeddingModel = "text-embedding-3-small"
	}

	embeddingBatchSize := 32
	if v := os.Getenv("EMBEDDING_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2048 {
			embeddingBatchSize = n
		}
	}

	forceReindex := os.Getenv("FORCE_REINDEX") == "true"
	purgeExcluded := os.Getenv("PURGE_EXCLUDED_ON_SYNC") == "true"

	return &Config{
		SlackAppToken:       os.Getenv("SLACK_APP_TOKEN"),
		SlackUserToken:      os.Getenv("SLACK_USER_TOKEN"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		BackfillDays:        backfillDays,
		ExcludedChannelIDs:  excluded,
		OpenAIAPIKey:        os.Getenv("OPENAI_API_KEY"),
		EmbeddingModel:      embeddingModel,
		EmbeddingBatchSize:  embeddingBatchSize,
		ForceReindex:        forceReindex,
		PurgeExcludedOnSync: purgeExcluded,
	}
}

func (c *Config) IsChannelExcluded(channelID string) bool {
	for _, id := range c.ExcludedChannelIDs {
		if id == channelID {
			return true
		}
	}
	return false
}
