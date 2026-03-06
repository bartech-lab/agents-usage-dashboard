# Agents Usage Monitor

Self-contained Go binary for monitoring AI assistant usage across **Kimi Code**, **Z-AI**, **OpenAI Codex**, and **Claude**.

> **Based on:** [konradozog-debug/AgentsUsageDashboard](https://github.com/konradozog-debug/AgentsUsageDashboard) - A complete Go rewrite of the original Python/Docker implementation with Firefox/VNC automation. This version uses manual credential configuration and requires no Docker or browser containers.

![Dashboard](docs/screenshot.png)

Dashboard showing all 4 AI assistants connected with real-time usage monitoring and live countdown timer.

## Features

- **Unified usage view** - Session and weekly usage for 4 AI assistants in one place
- **Real-time monitoring** - Robust auto-refresh with background tab support
- **Live countdown** - Shows exactly when data will refresh next
- **Smart refresh** - Visibility API ensures fresh data when switching tabs
- **Single binary** - No Docker required (~15MB)
- **Dark theme** - Clean, modern UI with color-coded usage bars
- **Zero dependencies** - Embedded frontend, no external services

## Quick Start

### Prerequisites

- Go 1.24+ (for building) or download pre-built binary

### Installation

```bash
# Clone and build (creates self-contained ~15MB binary)
git clone https://github.com/bartech-lab/agents-usage-dashboard.git
cd AgentsUsageDashboard
go build -o agents-dashboard

# Create config
cp config.yaml.example config.yaml
cp .env.example .env
```

### Configure

**All credentials must be manually configured.** Unlike the original Python version, this Go implementation does NOT automatically extract cookies from browsers. You must manually extract cookies from your browser and add them to `.env`.

Edit `.env` with your credentials:

```bash
# At minimum, add Z-AI API key
ZAI_API_KEY=your-id.your-secret

# Add others as needed
KIMI_AUTH_TOKEN=your-kimi-token
CODEX_SESSION_TOKEN=your-codex-token
CLAUDE_SESSION_KEY=your-claude-key
```

### Run

```bash
./agents-dashboard
```

Open http://localhost:8777

## Configuration

### Minimal (Z-AI only)

```yaml
providers:
  zai:
    api_key: "${ZAI_API_KEY}"
```

### Full (all providers)

```yaml
refresh_interval: 5m
server_port: 8777
providers:
  kimi:
    cookies:
      "kimi.com":
        "kimi-auth": "${KIMI_AUTH_TOKEN}"
  zai:
    api_key: "${ZAI_API_KEY}"
  codex:
    cookies:
      "chatgpt.com":
        "__Secure-next-auth.session-token": "${CODEX_SESSION_TOKEN}"
  claude:
    cookies:
      "claude.ai":
        "sessionKey": "${CLAUDE_SESSION_KEY}"
```

## Cookie Extraction

Cookie-based providers require extracting cookies from your browser:

**Chrome/Edge:**
1. Log in to service (chatgpt.com, kimi.com, or claude.ai)
2. F12 → Application → Cookies
3. Copy required cookie values

**Firefox:**
1. Log in to service
2. F12 → Storage → Cookies
3. Copy required cookie values

### Required Cookies

| Provider | Domain | Cookie Name |
|----------|--------|-------------|
| **Kimi** | kimi.com | `kimi-auth` |
| **Z-AI** | z.ai | API key (no cookie) |
| **Codex** | chatgpt.com | `__Secure-next-auth.session-token` |
| **Claude** | claude.ai | `sessionKey` |

**Z-AI API Key:** Get from [z.ai/manage-apikey/apikey-list](https://z.ai/manage-apikey/apikey-list)  
Format: `id.secret` (two parts separated by a dot)

## Auto-Refresh System

The dashboard implements a robust auto-refresh that works even in background tabs:

- **Polls every minute** to check if refresh is needed
- **Aligns with backend** - Only fetches when backend says it's time
- **Visibility API** - Instantly refreshes when you switch back to the tab if data is stale
- **Live countdown** - Shows "Next refresh in 4:23" in header
- **Staleness detection** - Refreshes if data is >30 seconds old
- **Error recovery** - Tracks consecutive errors with exponential backoff

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Dashboard UI |
| `/api/data` | GET | Current usage data (JSON) |
| `/api/refresh` | GET | Force immediate refresh |

## Resource Usage

- **RAM**: ~20MB
- **Binary**: ~15MB
- **CPU**: Minimal (polls every 5 minutes)
- **Network**: ~1 request per provider per 5 minutes

## Security

**⚠️ Designed for private, trusted networks only**

- **No authentication** - Place behind reverse proxy (Nginx, Caddy, Authelia) or VPN
- **Protect `.env`** - Contains sensitive API keys and session tokens
  ```bash
  chmod 600 .env
  ```
- **config.yaml is safe to commit** - Uses environment variable references (`${VAR}`), actual secrets are in `.env`
- **Never commit secrets** - `.env` is in `.gitignore`

## Troubleshooting

**No data showing:**
1. Check `.env` has correct credentials
2. Verify you can log in to services in browser
3. Check console output for errors
4. Test: `curl http://localhost:8777/api/data`

**Stale data:**
- Session expired - re-extract cookies from browser
- Check console for "Fetch failed" messages

**Z-AI not working:**
- Verify API key format: `id.secret` (two parts, dot-separated)

## Development

```bash
# Run tests
go test ./...

# Build optimized binary
go build -ldflags "-s -w" -o agents-dashboard

# Run with race detection
go run -race .
```

## Architecture

```
┌─────────────────────────────────────────┐
│  agents-dashboard (Go binary)           │
│                                         │
│  ┌──────────┐  .env/config.yaml  ┌────┐ │
│  │  Config  │  ───────────────→  │API │ │
│  │  Loader  │                    │Client│
│  └──────────┘                    └──┬─┘ │
│       ↑                             │   │
│       │    External APIs ◄──────────┘   │
│       │  (kimi, z.ai, chatgpt, claude)  │
│       └─────────────────────────────────┤
│              Scheduler (5min)           │
│                                         │
│       ┌────────────────────────┐        │
│       │  HTTP Server (:8777)   │        │
│       │  • /         Dashboard │        │
│       │  • /api/data  JSON API │        │
│       │  • /api/refresh Force  │        │
│       └────────────────────────┘        │
└─────────────────────────────────────────┘
```

## Tech Stack

- **Language**: Go 1.24
- **HTTP Client**: tls-client (Firefox fingerprinting)
- **Auth**: JWT (Z-AI), Cookie-based (others)
- **Frontend**: Vanilla HTML/CSS/JS (embedded)
- **Config**: YAML + godotenv

## Origins

This project is a complete Go rewrite of [konradozog-debug/AgentsUsageDashboard](https://github.com/konradozog-debug/AgentsUsageDashboard), which was a Python/Docker implementation with Firefox automation. The original used a Firefox container with automatic cookie extraction. This Go version replaces that with a simpler single-binary approach that requires manual credential configuration.

The original Python code is preserved in `legacy/` for historical reference.

## License

MIT License - see [LICENSE](LICENSE) for details.
