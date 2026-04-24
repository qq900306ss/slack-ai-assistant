package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	SlackAppToken      string
	SlackUserToken     string
	DatabaseURL        string
	BackfillDays       int
	ExcludedChannelIDs []string
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

	return &Config{
		SlackAppToken:      os.Getenv("SLACK_APP_TOKEN"),
		SlackUserToken:     os.Getenv("SLACK_USER_TOKEN"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		BackfillDays:       backfillDays,
		ExcludedChannelIDs: excluded,
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
