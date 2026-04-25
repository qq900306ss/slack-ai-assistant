package ingest

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/slack-go/slack"

	"github.com/qq900306ss/slack-ai-assistant/internal/config"
	"github.com/qq900306ss/slack-ai-assistant/internal/db"
)

// BotResponder handles bot mentions and responds.
type BotResponder interface {
	HandleMessage(ctx context.Context, ev *slack.MessageEvent) (handled bool, err error)
}

// Handler processes Slack events and persists them to the database.
type Handler struct {
	queries      *db.Queries
	pool         *pgxpool.Pool
	cfg          *config.Config
	logger       *slog.Logger
	botResponder BotResponder
}

// NewHandler creates an event handler.
func NewHandler(pool *pgxpool.Pool, cfg *config.Config, logger *slog.Logger) *Handler {
	return &Handler{
		queries: db.New(pool),
		pool:    pool,
		cfg:     cfg,
		logger:  logger,
	}
}

// SetBotResponder sets the bot responder for handling mentions.
func (h *Handler) SetBotResponder(br BotResponder) {
	h.botResponder = br
}

// HandleMessage processes message events (new, edited, deleted).
func (h *Handler) HandleMessage(ctx context.Context, ev *slack.MessageEvent) error {
	if h.cfg.IsChannelExcluded(ev.Channel) {
		return nil
	}

	// Check for bot mentions first (for new messages only)
	if h.botResponder != nil && (ev.SubType == "" || ev.SubType == "thread_broadcast") {
		handled, err := h.botResponder.HandleMessage(ctx, ev)
		if err != nil {
			h.logger.Error("bot responder error", "error", err)
		}
		if handled {
			// Still ingest the message, but we've already responded
			h.logger.Debug("bot handled message", "channel", ev.Channel, "ts", ev.Timestamp)
		}
	}

	switch ev.SubType {
	case "":
		return h.insertMessage(ctx, ev)
	case "message_changed":
		return h.updateMessage(ctx, ev)
	case "message_deleted":
		return h.deleteMessage(ctx, ev)
	case "thread_broadcast":
		return h.insertMessage(ctx, ev)
	default:
		if ev.SubType != "channel_join" && ev.SubType != "channel_leave" {
			return h.insertMessage(ctx, ev)
		}
		return nil
	}
}

func (h *Handler) insertMessage(ctx context.Context, ev *slack.MessageEvent) error {
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}

	// Ensure channel exists
	if err := h.queries.UpsertChannel(ctx, db.UpsertChannelParams{
		ID:        ev.Channel,
		IsPrivate: false,
	}); err != nil {
		return err
	}

	// Ensure user exists (if present)
	if ev.User != "" {
		if err := h.queries.UpsertUser(ctx, db.UpsertUserParams{
			ID: ev.User,
		}); err != nil {
			return err
		}
	}

	threadTS := ""
	if ev.ThreadTimestamp != "" && ev.ThreadTimestamp != ev.Timestamp {
		threadTS = ev.ThreadTimestamp
	}

	_, err = h.queries.InsertMessage(ctx, db.InsertMessageParams{
		ChannelID: ev.Channel,
		SlackTs:   ev.Timestamp,
		ThreadTs:  db.TextFromString(threadTS),
		UserID:    db.TextFromString(ev.User),
		Text:      db.TextFromString(ev.Text),
		RawJson:   raw,
	})
	if err != nil {
		return err
	}

	return h.queries.UpdateChannelLastIngestedTS(ctx, db.UpdateChannelLastIngestedTSParams{
		ID:             ev.Channel,
		LastIngestedTs: db.TextFromString(ev.Timestamp),
	})
}

func (h *Handler) updateMessage(ctx context.Context, ev *slack.MessageEvent) error {
	if ev.SubMessage == nil {
		return nil
	}

	raw, err := json.Marshal(ev.SubMessage)
	if err != nil {
		return err
	}

	if err := h.queries.UpdateMessage(ctx, db.UpdateMessageParams{
		ChannelID: ev.Channel,
		SlackTs:   ev.SubMessage.Timestamp,
		Text:      db.TextFromString(ev.SubMessage.Text),
		RawJson:   raw,
	}); err != nil {
		return err
	}

	msg, err := h.queries.GetMessageBySlackTS(ctx, db.GetMessageBySlackTSParams{
		ChannelID: ev.Channel,
		SlackTs:   ev.SubMessage.Timestamp,
	})
	if err != nil {
		return nil
	}

	return h.queries.DeleteEmbedding(ctx, msg.ID)
}

func (h *Handler) deleteMessage(ctx context.Context, ev *slack.MessageEvent) error {
	return h.queries.SoftDeleteMessage(ctx, db.SoftDeleteMessageParams{
		ChannelID: ev.Channel,
		SlackTs:   ev.DeletedTimestamp,
	})
}

// HandleReactionAdded processes reaction_added events.
func (h *Handler) HandleReactionAdded(ctx context.Context, ev *slack.ReactionAddedEvent) error {
	if h.cfg.IsChannelExcluded(ev.Item.Channel) {
		return nil
	}

	msg, err := h.queries.GetMessageBySlackTS(ctx, db.GetMessageBySlackTSParams{
		ChannelID: ev.Item.Channel,
		SlackTs:   ev.Item.Timestamp,
	})
	if err != nil {
		h.logger.Debug("reaction for unknown message", "channel", ev.Item.Channel, "ts", ev.Item.Timestamp)
		return nil
	}

	if err := h.queries.UpsertUser(ctx, db.UpsertUserParams{ID: ev.User}); err != nil {
		return err
	}

	return h.queries.UpsertReaction(ctx, db.UpsertReactionParams{
		MessageID: msg.ID,
		Emoji:     ev.Reaction,
		UserID:    ev.User,
	})
}

// HandleReactionRemoved processes reaction_removed events.
func (h *Handler) HandleReactionRemoved(ctx context.Context, ev *slack.ReactionRemovedEvent) error {
	if h.cfg.IsChannelExcluded(ev.Item.Channel) {
		return nil
	}

	msg, err := h.queries.GetMessageBySlackTS(ctx, db.GetMessageBySlackTSParams{
		ChannelID: ev.Item.Channel,
		SlackTs:   ev.Item.Timestamp,
	})
	if err != nil {
		return nil
	}

	return h.queries.DeleteReaction(ctx, db.DeleteReactionParams{
		MessageID: msg.ID,
		Emoji:     ev.Reaction,
		UserID:    ev.User,
	})
}

// HandleChannelCreated processes channel_created events.
func (h *Handler) HandleChannelCreated(ctx context.Context, ev *slack.ChannelCreatedEvent) error {
	return h.queries.UpsertChannel(ctx, db.UpsertChannelParams{
		ID:        ev.Channel.ID,
		Name:      db.TextFromString(ev.Channel.Name),
		IsPrivate: false,
	})
}

// HandleChannelRename processes channel_rename events.
func (h *Handler) HandleChannelRename(ctx context.Context, ev *slack.ChannelRenameEvent) error {
	return h.queries.UpsertChannel(ctx, db.UpsertChannelParams{
		ID:   ev.Channel.ID,
		Name: db.TextFromString(ev.Channel.Name),
	})
}

// HandleUserChange processes user_change events.
func (h *Handler) HandleUserChange(ctx context.Context, ev *slack.UserChangeEvent) error {
	return h.queries.UpsertUser(ctx, db.UpsertUserParams{
		ID:          ev.User.ID,
		Name:        db.TextFromString(ev.User.Name),
		DisplayName: db.TextFromString(ev.User.Profile.DisplayName),
		RealName:    db.TextFromString(ev.User.RealName),
		IsBot:       ev.User.IsBot,
	})
}
