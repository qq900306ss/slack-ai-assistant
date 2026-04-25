package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qq900306ss/slack-ai-assistant/internal/agent"
	"github.com/qq900306ss/slack-ai-assistant/internal/config"
)

// Server provides a web UI for the chat agent.
type Server struct {
	agent    *agent.Agent
	sessions *agent.SessionManager
	logger   *slog.Logger
	mux      *http.ServeMux
}

// NewServer creates a new web server.
func NewServer(pool *pgxpool.Pool, cfg *config.Config, logger *slog.Logger) (*Server, error) {
	a, err := agent.NewAgent(pool, cfg, logger)
	if err != nil {
		return nil, err
	}

	s := &Server{
		agent:    a,
		sessions: agent.NewSessionManager(30 * time.Minute),
		logger:   logger,
		mux:      http.NewServeMux(),
	}

	s.setupRoutes()
	return s, nil
}

func (s *Server) setupRoutes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/api/chat", s.handleChat)
	s.mux.HandleFunc("/api/clear", s.handleClear)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// ChatRequest is the request body for chat API.
type ChatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id"`
}

// ChatResponse is the response body for chat API.
type ChatResponse struct {
	Response  string `json:"response"`
	SessionID string `json:"session_id"`
	Error     string `json:"error,omitempty"`
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		s.jsonError(w, "Message is required", http.StatusBadRequest)
		return
	}

	// Generate session ID if not provided
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	// Get history
	history := s.sessions.GetHistory(sessionID)

	// Call agent
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	response, newHistory, err := s.agent.Chat(ctx, history, req.Message)
	if err != nil {
		s.logger.Error("chat error", "error", err, "session", sessionID)
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Update session
	s.sessions.UpdateHistory(sessionID, newHistory)

	// Send response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ChatResponse{
		Response:  response,
		SessionID: sessionID,
	})
}

func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.SessionID != "" {
		s.sessions.ClearSession(req.SessionID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

func (s *Server) jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ChatResponse{Error: msg})
}

var (
	sessionCounter int64
	sessionMu      sync.Mutex
)

func generateSessionID() string {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	sessionCounter++
	return time.Now().Format("20060102150405") + "-" + string(rune('A'+sessionCounter%26))
}

const indexHTML = `<!DOCTYPE html>
<html lang="zh-TW">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Slack AI Assistant</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #1a1a2e;
            color: #eee;
            height: 100vh;
            display: flex;
            flex-direction: column;
        }
        .header {
            background: #16213e;
            padding: 16px 24px;
            border-bottom: 1px solid #0f3460;
        }
        .header h1 { font-size: 1.25rem; font-weight: 600; }
        .header p { font-size: 0.875rem; color: #888; margin-top: 4px; }
        .chat-container {
            flex: 1;
            overflow-y: auto;
            padding: 24px;
            display: flex;
            flex-direction: column;
            gap: 16px;
        }
        .message {
            max-width: 80%;
            padding: 12px 16px;
            border-radius: 12px;
            line-height: 1.5;
            white-space: pre-wrap;
            word-break: break-word;
        }
        .message.user {
            background: #0f3460;
            align-self: flex-end;
            border-bottom-right-radius: 4px;
        }
        .message.assistant {
            background: #1f4068;
            align-self: flex-start;
            border-bottom-left-radius: 4px;
        }
        .message.error {
            background: #8b0000;
            align-self: center;
        }
        .message a { color: #4fc3f7; }
        .input-container {
            padding: 16px 24px;
            background: #16213e;
            border-top: 1px solid #0f3460;
            display: flex;
            gap: 12px;
        }
        #input {
            flex: 1;
            padding: 12px 16px;
            border: 1px solid #0f3460;
            border-radius: 8px;
            background: #1a1a2e;
            color: #eee;
            font-size: 1rem;
            outline: none;
        }
        #input:focus { border-color: #4fc3f7; }
        button {
            padding: 12px 24px;
            border: none;
            border-radius: 8px;
            font-size: 1rem;
            cursor: pointer;
            transition: background 0.2s;
        }
        #send {
            background: #e94560;
            color: white;
        }
        #send:hover { background: #ff6b6b; }
        #send:disabled { background: #555; cursor: not-allowed; }
        #clear {
            background: #333;
            color: #aaa;
        }
        #clear:hover { background: #444; }
        .typing {
            color: #888;
            font-style: italic;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>Slack AI Assistant</h1>
        <p>搜尋並分析你的 Slack 訊息</p>
    </div>
    <div class="chat-container" id="chat"></div>
    <div class="input-container">
        <input type="text" id="input" placeholder="輸入問題，例如：最近有人討論過部署嗎？" autofocus>
        <button id="send">送出</button>
        <button id="clear">清除</button>
    </div>
    <script>
        const chat = document.getElementById('chat');
        const input = document.getElementById('input');
        const sendBtn = document.getElementById('send');
        const clearBtn = document.getElementById('clear');
        let sessionId = '';
        let isLoading = false;

        // URL regex for link detection
        const urlRegex = /https?:\/\/[^\s]+/g;

        function addMessage(text, type) {
            const div = document.createElement('div');
            div.className = 'message ' + type;

            // Parse text and create links safely
            const parts = text.split(urlRegex);
            const urls = text.match(urlRegex) || [];

            parts.forEach((part, i) => {
                if (part) {
                    div.appendChild(document.createTextNode(part));
                }
                if (urls[i]) {
                    const a = document.createElement('a');
                    a.href = urls[i];
                    a.target = '_blank';
                    a.rel = 'noopener noreferrer';
                    a.textContent = urls[i];
                    div.appendChild(a);
                }
            });

            chat.appendChild(div);
            chat.scrollTop = chat.scrollHeight;
            return div;
        }

        async function sendMessage() {
            const message = input.value.trim();
            if (!message || isLoading) return;

            addMessage(message, 'user');
            input.value = '';
            isLoading = true;
            sendBtn.disabled = true;

            const typingDiv = addMessage('正在思考...', 'typing');

            try {
                const res = await fetch('/api/chat', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ message, session_id: sessionId })
                });
                const data = await res.json();
                typingDiv.remove();

                if (data.error) {
                    addMessage('錯誤: ' + data.error, 'error');
                } else {
                    sessionId = data.session_id;
                    addMessage(data.response, 'assistant');
                }
            } catch (err) {
                typingDiv.remove();
                addMessage('網路錯誤: ' + err.message, 'error');
            }

            isLoading = false;
            sendBtn.disabled = false;
            input.focus();
        }

        async function clearChat() {
            if (sessionId) {
                await fetch('/api/clear', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ session_id: sessionId })
                });
            }
            sessionId = '';
            while (chat.firstChild) {
                chat.removeChild(chat.firstChild);
            }
            input.focus();
        }

        sendBtn.addEventListener('click', sendMessage);
        clearBtn.addEventListener('click', clearChat);
        input.addEventListener('keypress', e => {
            if (e.key === 'Enter') sendMessage();
        });
    </script>
</body>
</html>
`
