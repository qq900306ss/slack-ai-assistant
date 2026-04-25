# Slack AI Assistant

A self-hosted, conversational AI assistant for Slack. Ask questions about your workspace history, get summaries, and trace every answer back to the original message.

> 📖 **[中文安裝指南](docs/setup-zh-TW.md)**

## Features

- **Self-hosted**: All data stays on your machine
- **BYOK**: Bring your own API keys
- **Single-tenant**: One deployment = one Slack workspace
- **Full history**: Backfills messages and keeps live sync
- **Traceable**: Every answer links back to original Slack messages

## Quick Start

### 1. Create Slack App

#### Option A: Using Manifest (Recommended)

1. Go to [api.slack.com/apps](https://api.slack.com/apps)
2. Click **Create New App** → **From an app manifest**
3. Select your workspace
4. Choose **YAML** tab, paste contents of `slack-app-manifest.yaml`
5. Click **Create**
6. Click **Install to Workspace** and authorize

#### Option B: Manual Setup

1. Go to [api.slack.com/apps](https://api.slack.com/apps)
2. Click **Create New App** → **From scratch**
3. Name it (e.g., "Slack AI Assistant"), select workspace

**Enable Socket Mode:**
- Settings → **Socket Mode** → Enable
- Create an app-level token with scope `connections:write`
- Save the token (`xapp-...`)

**Add User Token Scopes:**
- **OAuth & Permissions** → **User Token Scopes** → Add these scopes:
  ```
  channels:history
  channels:read
  groups:history
  groups:read
  im:history
  im:read
  mpim:history
  mpim:read
  users:read
  reactions:read
  ```

**Subscribe to Events:**
- **Event Subscriptions** → Enable Events
- **Subscribe to events on behalf of users** → Add these events:
  ```
  message.channels
  message.groups
  message.im
  message.mpim
  reaction_added
  reaction_removed
  channel_created
  channel_rename
  user_change
  ```

**Install App:**
- **OAuth & Permissions** → Click **Install to Workspace**
- Authorize the permissions
- Copy **User OAuth Token** (`xoxp-...`)

### 2. Get Your Tokens

After installation, you need two tokens:

| Token | Where to Find | Format |
|-------|---------------|--------|
| App Token | Settings → Basic Information → App-Level Tokens | `xapp-...` |
| User Token | OAuth & Permissions → User OAuth Token | `xoxp-...` |

### 3. Configure

```bash
cp .env.example .env
```

Edit `.env`:
```bash
SLACK_APP_TOKEN=xapp-1-xxx...
SLACK_USER_TOKEN=xoxp-xxx...
OPENAI_API_KEY=sk-xxx...  # Optional, for embeddings
```

### 4. Run

```bash
docker compose up
```

The server will:
1. Connect to Postgres with pgvector
2. Run database migrations
3. Sync channels and users
4. Start backfilling your Slack history (last 30 days)
5. Generate embeddings (if `OPENAI_API_KEY` is set)
6. Listen for new messages via Socket Mode

### 5. Verify

Connect to Postgres and check:

```bash
docker compose exec postgres psql -U postgres -d slack_assistant
```

```sql
-- Check data is flowing
SELECT COUNT(*) FROM channels;
SELECT COUNT(*) FROM users;
SELECT COUNT(*) FROM messages;
SELECT COUNT(*) FROM message_embeddings;

-- See recent messages
SELECT m.text, u.name, m.created_at
FROM messages m
LEFT JOIN users u ON m.user_id = u.id
ORDER BY m.created_at DESC
LIMIT 10;
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

## Troubleshooting

| Problem | Cause | Solution |
|---------|-------|----------|
| `socket mode connection error` | Invalid App Token | Check `SLACK_APP_TOKEN`, ensure Socket Mode is enabled |
| No messages syncing | Missing scopes | Verify all user token scopes are added |
| `rate limited` in logs | Normal | App automatically backs off and retries |
| Embeddings not generating | No API key | Set `OPENAI_API_KEY` in `.env` |

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
- [x] M3: Retrieval layer (BM25 + vector hybrid)
- [x] M4: Agent loop + tools (CLI)
- [ ] M5: Multi-turn conversation state
- [ ] M6: Web UI

## License

MIT
