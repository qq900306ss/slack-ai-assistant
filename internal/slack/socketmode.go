package slack

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// EventHandler processes incoming Slack events.
type EventHandler interface {
	HandleMessage(ctx context.Context, ev *slack.MessageEvent) error
	HandleReactionAdded(ctx context.Context, ev *slack.ReactionAddedEvent) error
	HandleReactionRemoved(ctx context.Context, ev *slack.ReactionRemovedEvent) error
	HandleChannelCreated(ctx context.Context, ev *slack.ChannelCreatedEvent) error
	HandleChannelRename(ctx context.Context, ev *slack.ChannelRenameEvent) error
	HandleUserChange(ctx context.Context, ev *slack.UserChangeEvent) error
}

// SocketModeClient manages the Socket Mode connection.
type SocketModeClient struct {
	client  *socketmode.Client
	handler EventHandler
	logger  *slog.Logger
}

// NewSocketModeClient creates a new Socket Mode client.
func NewSocketModeClient(appToken, userToken string, handler EventHandler, logger *slog.Logger) *SocketModeClient {
	api := slack.New(userToken, slack.OptionAppLevelToken(appToken))
	smClient := socketmode.New(api)

	return &SocketModeClient{
		client:  smClient,
		handler: handler,
		logger:  logger,
	}
}

// Run starts the Socket Mode event loop. Blocks until context is cancelled.
func (s *SocketModeClient) Run(ctx context.Context) error {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-s.client.Events:
				s.handleEvent(ctx, evt)
			}
		}
	}()

	return s.client.RunContext(ctx)
}

func (s *SocketModeClient) handleEvent(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		s.client.Ack(*evt.Request)
		s.handleEventsAPI(ctx, evt)

	case socketmode.EventTypeConnectionError:
		s.logger.Error("socket mode connection error", "data", evt.Data)

	case socketmode.EventTypeConnected:
		s.logger.Info("socket mode connected")

	case socketmode.EventTypeDisconnect:
		s.logger.Warn("socket mode disconnected")
	}
}

func (s *SocketModeClient) handleEventsAPI(ctx context.Context, evt socketmode.Event) {
	eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}

	innerEvent := eventsAPIEvent.InnerEvent
	switch ev := innerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		msgEv := &slack.MessageEvent{
			Msg: slack.Msg{
				Channel:         ev.Channel,
				User:            ev.User,
				Text:            ev.Text,
				Timestamp:       ev.TimeStamp,
				ThreadTimestamp: ev.ThreadTimeStamp,
				SubType:         ev.SubType,
				BotID:           ev.BotID,
			},
		}
		if err := s.handler.HandleMessage(ctx, msgEv); err != nil {
			s.logger.Error("failed to handle message", "error", err, "channel", ev.Channel, "ts", ev.TimeStamp)
		}

	case *slack.ReactionAddedEvent:
		if err := s.handler.HandleReactionAdded(ctx, ev); err != nil {
			s.logger.Error("failed to handle reaction_added", "error", err)
		}

	case *slack.ReactionRemovedEvent:
		if err := s.handler.HandleReactionRemoved(ctx, ev); err != nil {
			s.logger.Error("failed to handle reaction_removed", "error", err)
		}

	case *slack.ChannelCreatedEvent:
		if err := s.handler.HandleChannelCreated(ctx, ev); err != nil {
			s.logger.Error("failed to handle channel_created", "error", err)
		}

	case *slack.ChannelRenameEvent:
		if err := s.handler.HandleChannelRename(ctx, ev); err != nil {
			s.logger.Error("failed to handle channel_rename", "error", err)
		}

	case *slack.UserChangeEvent:
		if err := s.handler.HandleUserChange(ctx, ev); err != nil {
			s.logger.Error("failed to handle user_change", "error", err)
		}

	default:
		// Log unknown events for debugging
		raw, _ := json.Marshal(innerEvent)
		s.logger.Debug("unhandled event type", "type", innerEvent.Type, "data", string(raw))
	}
}
