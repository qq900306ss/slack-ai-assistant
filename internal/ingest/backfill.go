package ingest

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/slack-go/slack"

	"github.com/qq900306ss/slack-ai-assistant/internal/config"
	"github.com/qq900306ss/slack-ai-assistant/internal/db"
	slackclient "github.com/qq900306ss/slack-ai-assistant/internal/slack"
)

// Backfiller fetches historical messages for all channels.
type Backfiller struct {
	client  *slackclient.Client
	queries *db.Queries
	pool    *pgxpool.Pool
	cfg     *config.Config
	logger  *slog.Logger
}

// NewBackfiller creates a new backfill worker.
func NewBackfiller(client *slackclient.Client, pool *pgxpool.Pool, cfg *config.Config, logger *slog.Logger) *Backfiller {
	return &Backfiller{
		client:  client,
		queries: db.New(pool),
		pool:    pool,
		cfg:     cfg,
		logger:  logger,
	}
}

// Run starts the backfill process.
func (b *Backfiller) Run(ctx context.Context) error {
	if err := b.syncMetadata(ctx); err != nil {
		return err
	}

	channels, err := b.queries.ListChannelsNeedingBackfill(ctx)
	if err != nil {
		return err
	}

	b.logger.Info("starting backfill", "channels", len(channels))

	const maxConcurrent = 3
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for _, ch := range channels {
		if b.cfg.IsChannelExcluded(ch.ID) {
			continue
		}

		wg.Add(1)
		go func(ch db.Channel) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()

			if err := b.backfillChannel(ctx, ch); err != nil {
				b.logger.Error("backfill failed", "channel", ch.ID, "name", db.TextValue(ch.Name), "error", err)
			}
		}(ch)
	}

	wg.Wait()
	b.logger.Info("backfill completed")
	return nil
}

func (b *Backfiller) syncMetadata(ctx context.Context) error {
	b.logger.Info("syncing channel and user metadata")

	channels, err := b.client.GetConversations(ctx, "public_channel,private_channel,mpim,im")
	if err != nil {
		return err
	}

	for _, ch := range channels {
		if err := b.queries.UpsertChannel(ctx, db.UpsertChannelParams{
			ID:         ch.ID,
			Name:       db.TextFromString(ch.Name),
			IsPrivate:  ch.IsPrivate || ch.IsIM || ch.IsMpIM,
			IsArchived: ch.IsArchived,
		}); err != nil {
			return err
		}
	}
	b.logger.Info("synced channels", "count", len(channels))

	users, err := b.client.GetUsers(ctx)
	if err != nil {
		return err
	}

	for _, u := range users {
		if err := b.queries.UpsertUser(ctx, db.UpsertUserParams{
			ID:          u.ID,
			Name:        db.TextFromString(u.Name),
			DisplayName: db.TextFromString(u.Profile.DisplayName),
			RealName:    db.TextFromString(u.RealName),
			IsBot:       u.IsBot,
		}); err != nil {
			return err
		}
	}
	b.logger.Info("synced users", "count", len(users))

	return nil
}

func (b *Backfiller) backfillChannel(ctx context.Context, ch db.Channel) error {
	logger := b.logger.With("channel", ch.ID, "name", db.TextValue(ch.Name))
	logger.Info("starting channel backfill")

	oldest := time.Now().AddDate(0, 0, -b.cfg.BackfillDays).Unix()
	oldestTS := slack.JSONTime(oldest)

	state, err := b.queries.GetIngestState(ctx, ch.ID)
	if err != nil {
		state = db.IngestState{ChannelID: ch.ID}
	}

	cursor := ""
	messageCount := 0
	batchSize := 200

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params := &slack.GetConversationHistoryParameters{
			ChannelID: ch.ID,
			Cursor:    cursor,
			Limit:     batchSize,
			Oldest:    string(oldestTS),
		}

		if db.TextValid(state.OldestTsFetched) {
			params.Latest = db.TextValue(state.OldestTsFetched)
		}

		resp, err := b.client.GetConversationHistory(ctx, params)
		if err != nil {
			return err
		}

		for _, msg := range resp.Messages {
			if err := b.ingestMessage(ctx, ch.ID, &msg); err != nil {
				logger.Error("failed to ingest message", "ts", msg.Timestamp, "error", err)
				continue
			}
			messageCount++

			for _, reaction := range msg.Reactions {
				for _, userID := range reaction.Users {
					if err := b.ingestReaction(ctx, ch.ID, msg.Timestamp, reaction.Name, userID); err != nil {
						logger.Debug("failed to ingest reaction", "error", err)
					}
				}
			}
		}

		if len(resp.Messages) > 0 {
			oldestInBatch := resp.Messages[len(resp.Messages)-1].Timestamp
			if err := b.queries.UpsertIngestState(ctx, db.UpsertIngestStateParams{
				ChannelID:       ch.ID,
				OldestTsFetched: db.TextFromString(oldestInBatch),
				NewestTsFetched: state.NewestTsFetched,
				BackfillDone:    false,
			}); err != nil {
				return err
			}
			state.OldestTsFetched = db.TextFromString(oldestInBatch)
		}

		logger.Debug("backfill progress", "fetched", messageCount, "has_more", resp.HasMore)

		if !resp.HasMore {
			break
		}
		cursor = resp.ResponseMetaData.NextCursor
	}

	if err := b.queries.UpsertIngestState(ctx, db.UpsertIngestStateParams{
		ChannelID:       ch.ID,
		OldestTsFetched: state.OldestTsFetched,
		NewestTsFetched: state.NewestTsFetched,
		BackfillDone:    true,
	}); err != nil {
		return err
	}

	logger.Info("channel backfill completed", "messages", messageCount)
	return nil
}

func (b *Backfiller) ingestMessage(ctx context.Context, channelID string, msg *slack.Message) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	if msg.User != "" {
		if err := b.queries.UpsertUser(ctx, db.UpsertUserParams{ID: msg.User}); err != nil {
			return err
		}
	}

	threadTS := ""
	if msg.ThreadTimestamp != "" && msg.ThreadTimestamp != msg.Timestamp {
		threadTS = msg.ThreadTimestamp
	}

	_, err = b.queries.InsertMessage(ctx, db.InsertMessageParams{
		ChannelID: channelID,
		SlackTs:   msg.Timestamp,
		ThreadTs:  db.TextFromString(threadTS),
		UserID:    db.TextFromString(msg.User),
		Text:      db.TextFromString(msg.Text),
		RawJson:   raw,
	})
	return err
}

func (b *Backfiller) ingestReaction(ctx context.Context, channelID, msgTS, emoji, userID string) error {
	msg, err := b.queries.GetMessageBySlackTS(ctx, db.GetMessageBySlackTSParams{
		ChannelID: channelID,
		SlackTs:   msgTS,
	})
	if err != nil {
		return err
	}

	if err := b.queries.UpsertUser(ctx, db.UpsertUserParams{ID: userID}); err != nil {
		return err
	}

	return b.queries.UpsertReaction(ctx, db.UpsertReactionParams{
		MessageID: msg.ID,
		Emoji:     emoji,
		UserID:    userID,
	})
}
