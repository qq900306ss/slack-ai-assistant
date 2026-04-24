package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qq900306ss/slack-ai-assistant/internal/config"
	"github.com/qq900306ss/slack-ai-assistant/internal/retrieval"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: search <query>")
		fmt.Println("Example: search 改壞了")
		os.Exit(1)
	}

	query := strings.Join(os.Args[1:], " ")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := config.Load()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	client := retrieval.NewClient(pool, cfg, logger)

	fmt.Printf("Searching for: %q\n\n", query)

	results, err := client.Search(ctx, query, retrieval.SearchFilter{Limit: 10})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Search failed: %v\n", err)
		os.Exit(1)
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
		return
	}

	for i, r := range results {
		text := r.Text
		if len(text) > 100 {
			text = text[:100] + "..."
		}
		text = strings.ReplaceAll(text, "\n", " ")

		fmt.Printf("%d. [%.4f] #%s @%s\n", i+1, r.Score, r.ChannelName, r.UserName)
		fmt.Printf("   %s\n", text)
		fmt.Printf("   %s\n\n", r.Permalink)
	}
}
