package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/sashabaranov/go-openai"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qq900306ss/slack-ai-assistant/internal/config"
	"github.com/qq900306ss/slack-ai-assistant/internal/db"
	"github.com/qq900306ss/slack-ai-assistant/internal/retrieval"
)

const systemPrompt = `You are a helpful AI assistant that can search and analyze Slack messages.

IMPORTANT: Always respond in Traditional Chinese (繁體中文) as used in Taiwan.
- Use 資訊 not 信息
- Use 訊息 not 消息
- Use 軟體 not 软件
- Use 程式 not 程序
- Never use Simplified Chinese characters.

Your capabilities:
1. Search messages by keywords or semantic meaning
2. Retrieve full conversation threads
3. Look up user information
4. List available channels
5. Get messages from a specific user for personality/style analysis
6. Expand context - when user asks for more context (e.g., "給我更多上下文", "show more"), use expand_context tool

CRITICAL: When presenting search results to users:
1. After finding relevant messages, ALWAYS use get_thread to fetch the full conversation context
2. Summarize what the conversation is about, not just the single message
3. Explain the context: who said what, what was the discussion about, what was the conclusion
4. Only show isolated messages if they are truly standalone (not part of a thread)

When answering questions:
- Provide context and summaries, not just raw message snippets
- Always cite your sources with Slack permalinks
- If you can't find relevant information, say so honestly
- Be concise but complete
- Recognize that Slack conversations are often casual - people joke, exaggerate, and use humor
- Don't over-interpret casual banter as serious statements (e.g., "變亞特蘭提斯" is likely joking about rain, not actual flooding)

If the user asks about something that happened in Slack, use the search_messages tool first, then use get_thread to understand the full context before responding.`

// Agent handles conversations with OpenAI
type Agent struct {
	client   *openai.Client
	executor *ToolExecutor
	logger   *slog.Logger
	model    string
}

// NewAgent creates a new agent
func NewAgent(pool *pgxpool.Pool, cfg *config.Config, logger *slog.Logger) (*Agent, error) {
	if cfg.OpenAIAPIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}

	client := openai.NewClient(cfg.OpenAIAPIKey)
	retrievalClient := retrieval.NewClient(pool, cfg, logger)
	queries := db.New(pool)

	return &Agent{
		client:   client,
		executor: NewToolExecutor(retrievalClient, queries),
		logger:   logger,
		model:    "gpt-4o-mini",
	}, nil
}

// Message represents a conversation message
type Message struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

// Chat processes a user message and returns the assistant's response
func (a *Agent) Chat(ctx context.Context, history []Message, userMessage string) (string, []Message, error) {
	// Build message history for API
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
	}

	for _, m := range history {
		role := openai.ChatMessageRoleUser
		if m.Role == "assistant" {
			role = openai.ChatMessageRoleAssistant
		}
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    role,
			Content: m.Content,
		})
	}

	// Add current user message
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: userMessage,
	})

	// Update history
	newHistory := append(history, Message{Role: "user", Content: userMessage})

	// Agent loop
	for iterations := 0; iterations < 10; iterations++ {
		resp, err := a.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    a.model,
			Messages: messages,
			Tools:    AllTools(),
		})
		if err != nil {
			return "", nil, fmt.Errorf("openai api error: %w", err)
		}

		if len(resp.Choices) == 0 {
			return "", nil, fmt.Errorf("no response from openai")
		}

		choice := resp.Choices[0]
		assistantMsg := choice.Message

		// Add assistant message to conversation
		messages = append(messages, assistantMsg)

		// If no tool calls, we're done
		if len(assistantMsg.ToolCalls) == 0 {
			newHistory = append(newHistory, Message{Role: "assistant", Content: assistantMsg.Content})
			return assistantMsg.Content, newHistory, nil
		}

		// Execute tools
		for _, tc := range assistantMsg.ToolCalls {
			a.logger.Debug("executing tool", "name", tc.Function.Name, "input", tc.Function.Arguments)

			result, err := a.executor.Execute(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}

			// Add tool result
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	return "", nil, fmt.Errorf("max iterations reached")
}
