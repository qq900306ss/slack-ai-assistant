package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qq900306ss/slack-ai-assistant/internal/agent"
	"github.com/qq900306ss/slack-ai-assistant/internal/config"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := config.Load()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	a, err := agent.NewAgent(pool, cfg, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Slack AI Assistant")
	fmt.Println("==================")
	fmt.Println("Ask questions about your Slack workspace.")
	fmt.Println("Type 'exit' or 'quit' to end the session.")
	fmt.Println()

	var history []agent.Message
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			fmt.Println("Goodbye!")
			break
		}
		if input == "clear" {
			history = nil
			fmt.Println("Conversation cleared.")
			continue
		}

		response, newHistory, err := a.Chat(ctx, history, input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}

		history = newHistory
		fmt.Println()
		fmt.Println(response)
		fmt.Println()
	}
}
