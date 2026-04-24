package slack

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"golang.org/x/time/rate"
)

// Client wraps slack.Client with rate limiting and retry logic.
type Client struct {
	api     *slack.Client
	limiter *rate.Limiter
	logger  *slog.Logger
}

// NewClient creates a Slack client with rate limiting.
// Tier 3 APIs allow ~50 req/min, we limit to 80% = 40 req/min.
func NewClient(userToken string, logger *slog.Logger) *Client {
	return &Client{
		api:     slack.New(userToken),
		limiter: rate.NewLimiter(rate.Every(1500*time.Millisecond), 1), // ~40/min
		logger:  logger,
	}
}

func (c *Client) API() *slack.Client {
	return c.api
}

// wait applies rate limiting before making an API call.
func (c *Client) wait(ctx context.Context) error {
	return c.limiter.Wait(ctx)
}

// GetConversationHistory fetches messages with automatic rate limiting and retry.
func (c *Client) GetConversationHistory(ctx context.Context, params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
	var resp *slack.GetConversationHistoryResponse
	var err error

	for attempt := 0; attempt < 5; attempt++ {
		if err = c.wait(ctx); err != nil {
			return nil, err
		}

		resp, err = c.api.GetConversationHistoryContext(ctx, params)
		if err == nil {
			return resp, nil
		}

		if rateLimitErr, ok := err.(*slack.RateLimitedError); ok {
			wait := rateLimitErr.RetryAfter
			if wait == 0 {
				wait = time.Duration(1<<attempt) * time.Second
			}
			c.logger.Warn("rate limited, backing off", "retry_after", wait, "attempt", attempt+1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			continue
		}

		// Non-retryable error
		return nil, err
	}
	return nil, err
}

// GetConversationReplies fetches thread replies with rate limiting and retry.
func (c *Client) GetConversationReplies(ctx context.Context, params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
	var msgs []slack.Message
	var hasMore bool
	var cursor string
	var err error

	for attempt := 0; attempt < 5; attempt++ {
		if err = c.wait(ctx); err != nil {
			return nil, false, "", err
		}

		msgs, hasMore, cursor, err = c.api.GetConversationRepliesContext(ctx, params)
		if err == nil {
			return msgs, hasMore, cursor, nil
		}

		if rateLimitErr, ok := err.(*slack.RateLimitedError); ok {
			wait := rateLimitErr.RetryAfter
			if wait == 0 {
				wait = time.Duration(1<<attempt) * time.Second
			}
			c.logger.Warn("rate limited on replies, backing off", "retry_after", wait)
			select {
			case <-ctx.Done():
				return nil, false, "", ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		return nil, false, "", err
	}
	return nil, false, "", err
}

// GetConversations lists all conversations the user is in.
func (c *Client) GetConversations(ctx context.Context, types string) ([]slack.Channel, error) {
	var all []slack.Channel
	cursor := ""

	for {
		if err := c.wait(ctx); err != nil {
			return nil, err
		}

		params := &slack.GetConversationsParameters{
			Types:           strings.Split(types, ","),
			Cursor:          cursor,
			Limit:           200,
			ExcludeArchived: false,
		}

		channels, nextCursor, err := c.api.GetConversationsContext(ctx, params)
		if err != nil {
			if rateLimitErr, ok := err.(*slack.RateLimitedError); ok {
				time.Sleep(rateLimitErr.RetryAfter)
				continue
			}
			return nil, err
		}

		all = append(all, channels...)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return all, nil
}

// GetUsers lists all users in the workspace.
func (c *Client) GetUsers(ctx context.Context) ([]slack.User, error) {
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	return c.api.GetUsersContext(ctx)
}

// retryAfter extracts Retry-After header value in seconds.
func retryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	return 0
}
