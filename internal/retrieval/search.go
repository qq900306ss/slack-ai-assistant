package retrieval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SearchResult represents a single search result.
type SearchResult struct {
	MessageID   int64     `json:"message_id"`
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	SlackTS     string    `json:"slack_ts"`
	ThreadTS    string    `json:"thread_ts,omitempty"`
	UserID      string    `json:"user_id,omitempty"`
	UserName    string    `json:"user_name,omitempty"`
	Text        string    `json:"text"`
	CreatedAt   time.Time `json:"created_at"`
	Score       float64   `json:"score"`
	Permalink   string    `json:"permalink"`
}

// SearchFilter defines filtering options for search.
type SearchFilter struct {
	ChannelIDs []string   // Filter by specific channels
	UserIDs    []string   // Filter by specific users
	After      *time.Time // Messages after this time
	Before     *time.Time // Messages before this time
	ThreadTS   string     // Filter to specific thread
	Limit      int        // Max results (default 10)
}

// Searcher provides search functionality over messages.
type Searcher struct {
	pool *pgxpool.Pool
}

// NewSearcher creates a new searcher.
func NewSearcher(pool *pgxpool.Pool) *Searcher {
	return &Searcher{pool: pool}
}

// VectorSearch performs semantic similarity search using embeddings.
func (s *Searcher) VectorSearch(ctx context.Context, queryEmbedding []float32, filter SearchFilter) ([]SearchResult, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 10
	}

	query, args := s.buildVectorSearchQuery(queryEmbedding, filter, limit)
	return s.executeSearch(ctx, query, args)
}

// FullTextSearch performs keyword-based search using tsvector.
func (s *Searcher) FullTextSearch(ctx context.Context, queryText string, filter SearchFilter) ([]SearchResult, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 10
	}

	query, args := s.buildFullTextSearchQuery(queryText, filter, limit)
	return s.executeSearch(ctx, query, args)
}

// HybridSearch combines vector and full-text search with RRF (Reciprocal Rank Fusion).
func (s *Searcher) HybridSearch(ctx context.Context, queryText string, queryEmbedding []float32, filter SearchFilter) ([]SearchResult, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 10
	}

	// Get more results from each method for better fusion
	fetchLimit := limit * 3

	// Run both searches
	vectorFilter := filter
	vectorFilter.Limit = fetchLimit
	vectorResults, err := s.VectorSearch(ctx, queryEmbedding, vectorFilter)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	textFilter := filter
	textFilter.Limit = fetchLimit
	textResults, err := s.FullTextSearch(ctx, queryText, textFilter)
	if err != nil {
		return nil, fmt.Errorf("full-text search: %w", err)
	}

	// Apply RRF fusion
	return s.rrfFusion(vectorResults, textResults, limit), nil
}

func (s *Searcher) buildVectorSearchQuery(embedding []float32, filter SearchFilter, limit int) (string, []any) {
	args := []any{pgVectorLiteral(embedding)}
	argNum := 1

	var conditions []string
	conditions = append(conditions, "m.deleted_at IS NULL")

	if len(filter.ChannelIDs) > 0 {
		argNum++
		args = append(args, filter.ChannelIDs)
		conditions = append(conditions, fmt.Sprintf("m.channel_id = ANY($%d)", argNum))
	}

	if len(filter.UserIDs) > 0 {
		argNum++
		args = append(args, filter.UserIDs)
		conditions = append(conditions, fmt.Sprintf("m.user_id = ANY($%d)", argNum))
	}

	if filter.After != nil {
		argNum++
		args = append(args, *filter.After)
		conditions = append(conditions, fmt.Sprintf("m.created_at > $%d", argNum))
	}

	if filter.Before != nil {
		argNum++
		args = append(args, *filter.Before)
		conditions = append(conditions, fmt.Sprintf("m.created_at < $%d", argNum))
	}

	if filter.ThreadTS != "" {
		argNum++
		args = append(args, filter.ThreadTS)
		conditions = append(conditions, fmt.Sprintf("m.thread_ts = $%d", argNum))
	}

	argNum++
	args = append(args, limit)

	query := fmt.Sprintf(`
		SELECT m.id, m.channel_id, COALESCE(c.name, '') as channel_name,
		       m.slack_ts, COALESCE(m.thread_ts, '') as thread_ts,
		       COALESCE(m.user_id, '') as user_id, COALESCE(u.name, '') as user_name,
		       COALESCE(m.text, '') as text, m.created_at,
		       1 - (e.embedding <=> $1) as score
		FROM messages m
		JOIN message_embeddings e ON m.id = e.message_id
		LEFT JOIN channels c ON m.channel_id = c.id
		LEFT JOIN users u ON m.user_id = u.id
		WHERE %s
		ORDER BY e.embedding <=> $1
		LIMIT $%d
	`, strings.Join(conditions, " AND "), argNum)

	return query, args
}

func (s *Searcher) buildFullTextSearchQuery(queryText string, filter SearchFilter, limit int) (string, []any) {
	// Convert query to tsquery format
	tsQuery := strings.Join(strings.Fields(queryText), " | ")
	args := []any{tsQuery}
	argNum := 1

	var conditions []string
	conditions = append(conditions, "m.deleted_at IS NULL")
	conditions = append(conditions, "m.text_search @@ to_tsquery('simple', $1)")

	if len(filter.ChannelIDs) > 0 {
		argNum++
		args = append(args, filter.ChannelIDs)
		conditions = append(conditions, fmt.Sprintf("m.channel_id = ANY($%d)", argNum))
	}

	if len(filter.UserIDs) > 0 {
		argNum++
		args = append(args, filter.UserIDs)
		conditions = append(conditions, fmt.Sprintf("m.user_id = ANY($%d)", argNum))
	}

	if filter.After != nil {
		argNum++
		args = append(args, *filter.After)
		conditions = append(conditions, fmt.Sprintf("m.created_at > $%d", argNum))
	}

	if filter.Before != nil {
		argNum++
		args = append(args, *filter.Before)
		conditions = append(conditions, fmt.Sprintf("m.created_at < $%d", argNum))
	}

	if filter.ThreadTS != "" {
		argNum++
		args = append(args, filter.ThreadTS)
		conditions = append(conditions, fmt.Sprintf("m.thread_ts = $%d", argNum))
	}

	argNum++
	args = append(args, limit)

	query := fmt.Sprintf(`
		SELECT m.id, m.channel_id, COALESCE(c.name, '') as channel_name,
		       m.slack_ts, COALESCE(m.thread_ts, '') as thread_ts,
		       COALESCE(m.user_id, '') as user_id, COALESCE(u.name, '') as user_name,
		       COALESCE(m.text, '') as text, m.created_at,
		       ts_rank(m.text_search, to_tsquery('simple', $1)) as score
		FROM messages m
		LEFT JOIN channels c ON m.channel_id = c.id
		LEFT JOIN users u ON m.user_id = u.id
		WHERE %s
		ORDER BY score DESC
		LIMIT $%d
	`, strings.Join(conditions, " AND "), argNum)

	return query, args
}

func (s *Searcher) executeSearch(ctx context.Context, query string, args []any) ([]SearchResult, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		err := rows.Scan(
			&r.MessageID, &r.ChannelID, &r.ChannelName,
			&r.SlackTS, &r.ThreadTS,
			&r.UserID, &r.UserName,
			&r.Text, &r.CreatedAt, &r.Score,
		)
		if err != nil {
			return nil, err
		}
		r.Permalink = buildPermalink(r.ChannelID, r.SlackTS)
		results = append(results, r)
	}

	return results, rows.Err()
}

// rrfFusion combines results using Reciprocal Rank Fusion.
// RRF score = sum(1 / (k + rank)) for each result list
func (s *Searcher) rrfFusion(vectorResults, textResults []SearchResult, limit int) []SearchResult {
	const k = 60 // RRF constant

	scores := make(map[int64]float64)
	results := make(map[int64]SearchResult)

	// Score vector results
	for rank, r := range vectorResults {
		scores[r.MessageID] += 1.0 / float64(k+rank+1)
		results[r.MessageID] = r
	}

	// Score text results
	for rank, r := range textResults {
		scores[r.MessageID] += 1.0 / float64(k+rank+1)
		if _, exists := results[r.MessageID]; !exists {
			results[r.MessageID] = r
		}
	}

	// Sort by RRF score
	type scored struct {
		id    int64
		score float64
	}
	var sortable []scored
	for id, score := range scores {
		sortable = append(sortable, scored{id, score})
	}

	// Simple bubble sort (small list)
	for i := 0; i < len(sortable); i++ {
		for j := i + 1; j < len(sortable); j++ {
			if sortable[j].score > sortable[i].score {
				sortable[i], sortable[j] = sortable[j], sortable[i]
			}
		}
	}

	// Build result list
	var final []SearchResult
	for i, s := range sortable {
		if i >= limit {
			break
		}
		r := results[s.id]
		r.Score = s.score
		final = append(final, r)
	}

	return final
}

func buildPermalink(channelID, slackTS string) string {
	// Slack permalink format: https://slack.com/archives/{channel_id}/p{ts_without_dot}
	ts := strings.Replace(slackTS, ".", "", 1)
	return fmt.Sprintf("https://slack.com/archives/%s/p%s", channelID, ts)
}

func pgVectorLiteral(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%f", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
