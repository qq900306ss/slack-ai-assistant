package ingest

import (
	"context"
	"encoding/json"
	"fmt"
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
	// Handle FORCE_REINDEX
	if b.cfg.ForceReindex {
		b.logger.Info("FORCE_REINDEX enabled, resetting all ingest states")
		if err := b.queries.ResetAllIngestStates(ctx); err != nil {
			b.logger.Error("failed to reset ingest states", "error", err)
		}
	}

	if err := b.syncMetadata(ctx); err != nil {
		return err
	}

	// Purge excluded channels if enabled
	if b.cfg.PurgeExcludedOnSync {
		b.purgeExcludedChannels(ctx)
	}

	// First, fill gaps (fetch new messages since last sync)
	if err := b.fillGaps(ctx); err != nil {
		b.logger.Error("gap fill failed", "error", err)
	}

	// Extend backfill if BACKFILL_DAYS increased
	if err := b.extendBackfill(ctx); err != nil {
		b.logger.Error("extend backfill failed", "error", err)
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

// purgeExcludedChannels removes all data for channels in the exclude list.
func (b *Backfiller) purgeExcludedChannels(ctx context.Context) {
	for _, channelID := range b.cfg.ExcludedChannelIDs {
		b.logger.Info("purging excluded channel", "channel", channelID)

		// Delete in order: embeddings -> messages -> ingest_state
		if err := b.queries.PurgeChannelEmbeddings(ctx, channelID); err != nil {
			b.logger.Debug("failed to purge embeddings", "channel", channelID, "error", err)
		}
		if err := b.queries.PurgeChannelMessages(ctx, channelID); err != nil {
			b.logger.Debug("failed to purge messages", "channel", channelID, "error", err)
		}
		if err := b.queries.PurgeChannelIngestState(ctx, channelID); err != nil {
			b.logger.Debug("failed to purge ingest state", "channel", channelID, "error", err)
		}
	}
	b.logger.Info("purged excluded channels", "count", len(b.cfg.ExcludedChannelIDs))
}

// extendBackfill fetches older messages if BACKFILL_DAYS was increased.
func (b *Backfiller) extendBackfill(ctx context.Context) error {
	targetOldest := time.Now().AddDate(0, 0, -b.cfg.BackfillDays).Unix()
	targetOldestTS := fmt.Sprintf("%d.000000", targetOldest)

	channels, err := b.queries.ListChannelsNeedingExtend(ctx)
	if err != nil {
		return err
	}

	var needExtend []db.ListChannelsNeedingExtendRow
	for _, ch := range channels {
		if b.cfg.IsChannelExcluded(ch.ID) {
			continue
		}
		// Check if oldest_ts_fetched is newer than target (needs extension)
		if db.TextValid(ch.OldestTsFetched) {
			oldestFetched := db.TextValue(ch.OldestTsFetched)
			if oldestFetched > targetOldestTS {
				needExtend = append(needExtend, ch)
			}
		}
	}

	if len(needExtend) == 0 {
		return nil
	}

	b.logger.Info("extending backfill for channels", "count", len(needExtend), "target_days", b.cfg.BackfillDays)

	const maxConcurrent = 3
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for _, ch := range needExtend {
		wg.Add(1)
		go func(ch db.ListChannelsNeedingExtendRow) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()

			if err := b.extendChannel(ctx, ch, targetOldestTS); err != nil {
				b.logger.Error("extend failed", "channel", ch.ID, "error", err)
			}
		}(ch)
	}

	wg.Wait()
	b.logger.Info("extend backfill completed")
	return nil
}

func (b *Backfiller) extendChannel(ctx context.Context, ch db.ListChannelsNeedingExtendRow, targetOldestTS string) error {
	logger := b.logger.With("channel", ch.ID, "name", db.TextValue(ch.Name))
	logger.Info("extending channel backfill")

	cursor := ""
	messageCount := 0
	batchSize := 200
	currentOldest := db.TextValue(ch.OldestTsFetched)

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
			Oldest:    targetOldestTS,
			Latest:    currentOldest,
		}

		resp, err := b.client.GetConversationHistory(ctx, params)
		if err != nil {
			return err
		}

		for _, msg := range resp.Messages {
			if err := b.ingestMessage(ctx, ch.ID, &msg); err != nil {
				continue
			}
			messageCount++
		}

		if len(resp.Messages) > 0 {
			oldestInBatch := resp.Messages[len(resp.Messages)-1].Timestamp
			if err := b.queries.UpsertIngestState(ctx, db.UpsertIngestStateParams{
				ChannelID:       ch.ID,
				OldestTsFetched: db.TextFromString(oldestInBatch),
				BackfillDone:    true,
			}); err != nil {
				return err
			}
			currentOldest = oldestInBatch
		}

		if !resp.HasMore {
			break
		}
		cursor = resp.ResponseMetaData.NextCursor
	}

	if messageCount > 0 {
		logger.Info("channel extended", "new_messages", messageCount)
	}
	return nil
}

// fillGaps fetches messages that were missed during disconnection.
// It finds the newest message timestamp for each channel and fetches forward.
func (b *Backfiller) fillGaps(ctx context.Context) error {
	channels, err := b.queries.ListChannels(ctx)
	if err != nil {
		return err
	}

	b.logger.Info("filling gaps for channels", "count", len(channels))

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

			if err := b.fillChannelGap(ctx, ch); err != nil {
				b.logger.Debug("gap fill failed", "channel", ch.ID, "error", err)
			}
		}(ch)
	}

	wg.Wait()
	b.logger.Info("gap fill completed")
	return nil
}

func (b *Backfiller) fillChannelGap(ctx context.Context, ch db.Channel) error {
	// Get the newest message timestamp in this channel
	newestTS, err := b.queries.GetNewestMessageTS(ctx, ch.ID)
	if err != nil {
		return nil // No messages yet, skip
	}

	if newestTS == "" {
		return nil
	}

	logger := b.logger.With("channel", ch.ID, "name", db.TextValue(ch.Name))

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
			Oldest:    newestTS, // Fetch messages NEWER than this
		}

		resp, err := b.client.GetConversationHistory(ctx, params)
		if err != nil {
			return err
		}

		for _, msg := range resp.Messages {
			if msg.Timestamp == newestTS {
				continue // Skip the message we already have
			}
			if err := b.ingestMessage(ctx, ch.ID, &msg); err != nil {
				continue
			}
			messageCount++
		}

		if !resp.HasMore {
			break
		}
		cursor = resp.ResponseMetaData.NextCursor
	}

	if messageCount > 0 {
		logger.Info("gap filled", "new_messages", messageCount)
	}

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
	oldestTS := fmt.Sprintf("%d.000000", oldest)

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
			Oldest:    oldestTS,
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
