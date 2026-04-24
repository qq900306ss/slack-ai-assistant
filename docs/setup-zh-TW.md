# Slack AI Assistant 安裝指南

## 目錄

1. [建立 Slack App](#1-建立-slack-app)
2. [取得 OpenAI API Key](#2-取得-openai-api-key)
3. [設定與啟動](#3-設定與啟動)
4. [驗證安裝](#4-驗證安裝)

---

## 1. 建立 Slack App

### Step 1: 建立 App

1. 前往 [api.slack.com/apps](https://api.slack.com/apps)
2. 點擊右上角 **Create New App**
3. 選擇 **From scratch**（從頭建立）
4. 輸入 App 名稱（例如 `Slack AI Assistant`）
5. 選擇你的 Workspace
6. 點擊 **Create App**

### Step 2: 啟用 Socket Mode

1. 左側選單點擊 **Settings → Socket Mode**
2. 開啟 **Enable Socket Mode**
3. 系統會要求你建立 App-Level Token：
   - Token Name：輸入任意名稱（例如 `socket-token`）
   - Scope：選擇 `connections:write`
   - 點擊 **Generate**
4. **複製這個 Token**（`xapp-` 開頭），這是 `SLACK_APP_TOKEN`

### Step 3: 設定 User Token Scopes

1. 左側選單點擊 **OAuth & Permissions**
2. 往下滾到 **User Token Scopes**
3. 點擊 **Add an OAuth Scope**，逐一加入以下 scope：

| Scope | 用途 |
|-------|------|
| `channels:history` | 讀取公開頻道訊息 |
| `channels:read` | 取得頻道列表 |
| `groups:history` | 讀取私人頻道訊息 |
| `groups:read` | 取得私人頻道列表 |
| `im:history` | 讀取私訊 |
| `im:read` | 取得私訊列表 |
| `mpim:history` | 讀取群組私訊 |
| `mpim:read` | 取得群組私訊列表 |
| `users:read` | 取得使用者資訊 |
| `reactions:read` | 讀取 emoji reaction |

### Step 4: 訂閱事件

1. 左側選單點擊 **Event Subscriptions**
2. 開啟 **Enable Events**
3. 展開 **Subscribe to events on behalf of users**
4. 點擊 **Add Workspace Event**，逐一加入：

| Event | 用途 |
|-------|------|
| `message.channels` | 公開頻道新訊息 |
| `message.groups` | 私人頻道新訊息 |
| `message.im` | 私訊新訊息 |
| `message.mpim` | 群組私訊新訊息 |
| `reaction_added` | 新增 emoji reaction |
| `reaction_removed` | 移除 emoji reaction |
| `channel_created` | 新建頻道 |
| `channel_rename` | 頻道改名 |
| `user_change` | 使用者資訊更新 |

5. 點擊右下角 **Save Changes**

### Step 5: 安裝 App 到 Workspace

1. 左側選單點擊 **OAuth & Permissions**
2. 點擊上方的 **Install to Workspace**
3. 檢視權限後點擊 **Allow**
4. **複製 User OAuth Token**（`xoxp-` 開頭），這是 `SLACK_USER_TOKEN`

### Token 總覽

| Token | 格式 | 位置 |
|-------|------|------|
| App Token | `xapp-...` | Settings → Basic Information → App-Level Tokens |
| User Token | `xoxp-...` | OAuth & Permissions → User OAuth Token |

---

## 2. 取得 OpenAI API Key

> 這是可選的。沒有 OpenAI key 也能跑，只是不會產生 embedding（無法做語意搜尋）。

### Step 1: 註冊 / 登入 OpenAI

1. 前往 [platform.openai.com](https://platform.openai.com)
2. 點擊右上角 **Sign up** 或 **Log in**
3. 使用 Google / Microsoft / Apple 帳號，或 Email 註冊

### Step 2: 加入付款方式

1. 登入後點擊右上角頭像 → **Billing**
2. 點擊 **Add payment method**
3. 輸入信用卡資訊
4. 建議設定 **Usage limits** 避免超支（例如 $10/月）

### Step 3: 建立 API Key

1. 左側選單點擊 **API keys**
2. 點擊 **Create new secret key**
3. 輸入名稱（例如 `slack-assistant`）
4. 點擊 **Create secret key**
5. **立即複製！** 關掉視窗後就看不到了

### 費用參考

| Model | 用途 | 價格 |
|-------|------|------|
| `text-embedding-3-small` | Embedding（預設）| $0.02 / 1M tokens |
| `text-embedding-3-large` | 更高品質 Embedding | $0.13 / 1M tokens |

一般 Slack workspace 的 30 天訊息，embedding 成本大約 $0.1 ~ $1。

---

## 3. 設定與啟動

### Step 1: Clone 專案

```bash
git clone https://github.com/qq900306ss/slack-ai-assistant.git
cd slack-ai-assistant
```

### Step 2: 建立設定檔

```bash
cp .env.example .env
```

編輯 `.env`：

```bash
# Slack（必填）
SLACK_APP_TOKEN=xapp-1-xxx...        # Step 2 拿到的
SLACK_USER_TOKEN=xoxp-xxx...         # Step 5 拿到的

# OpenAI（選填，沒填就不跑 embedding）
OPENAI_API_KEY=sk-xxx...

# 其他（可用預設值）
BACKFILL_DAYS=30                     # 回溯幾天的訊息
```

### Step 3: 啟動

```bash
docker compose up
```

首次啟動會：
1. 下載 Docker images
2. 建立 PostgreSQL 資料庫
3. 執行 migrations
4. 開始同步 Slack 資料

---

## 4. 驗證安裝

### 檢查 Log

正常應該看到：
```
connected to database
syncing channel and user metadata
synced channels count=XX
synced users count=XX
starting channel backfill
socket mode connected
```

### 連進資料庫確認

```bash
docker compose exec postgres psql -U postgres -d slack_assistant
```

```sql
-- 確認有資料
SELECT COUNT(*) FROM channels;
SELECT COUNT(*) FROM users;
SELECT COUNT(*) FROM messages;

-- 看最近訊息
SELECT text, created_at FROM messages ORDER BY created_at DESC LIMIT 5;

-- 確認 embedding 有在跑（需設定 OPENAI_API_KEY）
SELECT COUNT(*) FROM message_embeddings;
```

### 常見問題

| 症狀 | 原因 | 解法 |
|------|------|------|
| `socket mode connection error` | App Token 錯誤 | 確認 `SLACK_APP_TOKEN` 是否正確 |
| `invalid_auth` | User Token 錯誤 | 確認 `SLACK_USER_TOKEN` 是否正確 |
| messages 表一直是 0 | Scope 沒加齊 | 回去檢查 Step 3 的所有 scope |
| embedding 表一直是 0 | 沒設 OpenAI key | 檢查 `OPENAI_API_KEY` |
| `rate limited` | 正常現象 | 系統會自動重試，不用處理 |

---

## 下一步

安裝完成後，你可以：

1. 等 backfill 跑完（視訊息量，可能幾分鐘到幾小時）
2. 用 SQL 查詢訊息和 embedding
3. 測試語意搜尋：

```sql
-- 找一則訊息的 embedding
SELECT message_id FROM message_embeddings LIMIT 1;

-- 用它搜尋相似訊息（假設 message_id = 123）
SELECT m.text,
       e.embedding <=> (SELECT embedding FROM message_embeddings WHERE message_id = 123) AS distance
FROM messages m
JOIN message_embeddings e ON m.id = e.message_id
WHERE m.id != 123
ORDER BY distance
LIMIT 5;
```
