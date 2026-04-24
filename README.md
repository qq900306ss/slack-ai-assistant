# Slack AI Assistant

A self-hosted, conversational AI assistant for Slack. Ask questions about your workspace history, get summaries, and trace every answer back to the original message.

## Features

- **Self-hosted**: All data stays on your machine
- **BYOK**: Bring your own Anthropic API key
- **Single-tenant**: One deployment = one Slack workspace
- **Full history**: Backfills messages and keeps live sync
- **Traceable**: Every answer links back to original Slack messages

## Quick Start

### 1. Create Slack App

1. Go to [api.slack.com/apps](https://api.slack.com/apps)
2. Click "Create New App" → "From a manifest"
3. Paste contents of `slack-app-manifest.yaml`
4. Install to your workspace
5. Copy tokens:
   - **App-Level Token**: Settings → Basic Information → App-Level Tokens → Generate (scope: `connections:write`)
   - **User Token**: OAuth & Permissions → User OAuth Token (`xoxp-...`)

### 2. Configure

```bash
cp .env.example .env
# Edit .env with your tokens
```

### 3. Run

```bash
docker compose up
```

The server will:
1. Connect to Postgres with pgvector
2. Run database migrations
3. Start backfilling your Slack history (last 30 days by default)
4. Generate embeddings for messages (if `OPENAI_API_KEY` is set)
5. Listen for new messages via Socket Mode

### 4. Verify

Connect to Postgres and query:

```sql
SELECT COUNT(*) FROM messages;
SELECT * FROM messages ORDER BY created_at DESC LIMIT 10;
```

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SLACK_APP_TOKEN` | Yes | - | Socket Mode token (`xapp-...`) |
| `SLACK_USER_TOKEN` | Yes | - | User OAuth token (`xoxp-...`) |
| `DATABASE_URL` | Yes | - | Postgres connection string |
| `BACKFILL_DAYS` | No | 30 | Days of history to backfill |
| `SLACK_EXCLUDE_CHANNELS` | No | - | Comma-separated channel IDs to skip |
| `OPENAI_API_KEY` | No | - | OpenAI API key for embeddings |
| `EMBEDDING_MODEL` | No | text-embedding-3-small | Embedding model |
| `EMBEDDING_BATCH_SIZE` | No | 32 | Messages per embedding batch |

## Development

```bash
# Install tools
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Generate sqlc code
make sqlc

# Run locally (requires local Postgres)
make run

# Run tests
make test
```

## Privacy

This app requires a **User Token** with broad read permissions. It can access:
- All public channels you're in
- All private channels you're in
- All DMs and group DMs

**Only run this on workspaces you own or have explicit permission to archive.**

## Roadmap

- [x] M1: Ingest pipeline (Slack → Postgres)
- [x] M2: Embedding pipeline (pgvector + OpenAI)
- [ ] M3: Retrieval layer (BM25 + vector hybrid)
- [ ] M4: Agent loop + tools (CLI)
- [ ] M5: Multi-turn conversation state
- [ ] M6: Web UI

## License

MIT
