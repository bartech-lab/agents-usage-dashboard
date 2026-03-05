# Agent Usage Dashboard (Go Version)

Lightweight Go binary for monitoring AI assistant usage limits across **OpenAI Codex**, **Kimi Code**, **Claude**, and **Z-AI**.

![Dashboard](docs/screenshot.png)

## Features

- Unified view of session and weekly usage across 4 AI assistants
- Single binary, no Docker required (~15MB)
- YAML configuration with cookie/token auth
- Real-time dashboard with background polling
- Color-coded usage bars (ok / warning / critical)
- Stale data detection when services are unreachable
- Codex daily usage chart (last 14 days)
- Dark theme, zero JavaScript frameworks

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│  Dashboard Binary (Go)                                    │
│                                                           │
│  ┌─────────────┐    config.yaml     ┌─────────────────┐  │
│  │   Config     │   cookies/tokens   │   HTTP Client    │  │
│  │   Loader     │──────────────────→│   (tls-client)   │  │
│  └─────────────┘                    └────────┬────────┘  │
│        ↑                                      │           │
│        │                                      ↓           │
│        │                               External APIs      │
│        │                            chatgpt.com, kimi.com │
│        │                            claude.ai, z.ai       │
│        │                                      │           │
│        └──────────────────────────────────────┘           │
│                    Scheduler (background)                 │
│                                                           │
│  ┌─────────────────────────────────────────────────────┐  │
│  │  HTTP Server :8777                                   │  │
│  │  - /          → Dashboard UI                         │  │
│  │  - /api/data  → JSON API                             │  │
│  │  - /api/refresh → Force refresh                      │  │
│  └─────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────┘
```

## Installation

### Prerequisites

- Go 1.24+ (for building from source)
- Or download pre-built binary from releases

### Build from Source

```bash
# Clone the repository
git clone https://github.com/konradozog-debug/AgentsUsageDashboard.git
cd AgentsUsageDashboard/go-rewrite

# Build the binary
go build -o agents-dashboard

# Or build with version info
go build -ldflags "-s -w" -o agents-dashboard
```

### Download Pre-built Binary

Check the [releases page](https://github.com/konradozog-debug/AgentsUsageDashboard/releases) for pre-built binaries.

## Configuration

1. Copy the example configuration:

```bash
cp config.yaml.example config.yaml
```

2. Edit `config.yaml` with your credentials. See [Cookie Extraction](#cookie-extraction) below for details on obtaining cookies.

### Minimal Configuration (Z-AI only)

```yaml
refresh_interval: 5m
server_port: 8777
providers:
  zai:
    api_key: "your-api-key-id.secret"
```

### Full Configuration Example

```yaml
refresh_interval: 5m
server_port: 8777
providers:
  codex:
    cookies:
      "chatgpt.com":
        "__Secure-next-auth.session-token": "your-session-token"
  kimi:
    cookies:
      "kimi.com":
        "kimi-auth": "your-kimi-auth-token"
  claude:
    cookies:
      "claude.ai":
        "sessionKey": "your-session-key"
  zai:
    api_key: "your-api-key-id.secret"
```

## Cookie Extraction

Cookie-based authentication requires you to extract cookies from your browser after logging into each service.

### Chrome/Edge

1. Log in to the service you want to monitor (chatgpt.com, kimi.com, or claude.ai)
2. Open Developer Tools (F12 or Ctrl+Shift+I)
3. Go to Application tab → Cookies
4. Select the domain (chatgpt.com, kimi.com, or claude.ai)
5. Copy the required cookie values

### Firefox

1. Log in to the service you want to monitor
2. Open Developer Tools (F12 or Ctrl+Shift+I)
3. Go to Storage tab → Cookies
4. Select the domain
5. Copy the required cookie values

### Required Cookies per Provider

#### OpenAI Codex (chatgpt.com)

Codex uses the standard ChatGPT authentication. You need the session cookies that are set after logging in. Common cookies include:

- `__Secure-next-auth.session-token`
- `__Secure-next-auth.callback-url`
- Other session-related cookies

Copy all cookies from chatgpt.com domain to the config. The application will use them to fetch an access token.

#### Kimi Code (kimi.com)

Kimi requires the `kimi-auth` cookie:

- `kimi-auth`: The authentication token for Kimi Code

#### Claude (claude.ai)

Claude requires session cookies:

- `sessionKey`: The main session identifier
- Other session-related cookies as needed

#### Z-AI (z.ai)

Z-AI uses API key authentication (no cookies needed):

- Format: `id.secret` (two parts separated by a dot)
- Get your API key from [z.ai/manage-apikey/apikey-list](https://z.ai/manage-apikey/apikey-list)

### Environment Variables

For security, use environment variables in your `config.yaml`:

```yaml
providers:
  codex:
    cookies:
      "chatgpt.com":
        "__Secure-next-auth.session-token": "${CODEX_SESSION_TOKEN}"
```

**Option 1: `.env` file (recommended)**

The binary automatically loads `.env` from the current directory on startup:

```bash
# .env file
ZAI_API_KEY=your-id.your-secret
CODEX_SESSION_TOKEN=your-token
KIMI_AUTH_TOKEN=your-token
CLAUDE_SESSION_KEY=your-key
```

Quotes are optional - both `KEY=value` and `KEY="value"` work.

**Option 2: Export directly**

```bash
export CODEX_SESSION_TOKEN="your-actual-token"
./agents-dashboard
```

## Running

```bash
./agents-dashboard
```

The server will start on http://localhost:8777

### Command Line Options

There are no command line options. All configuration is done via `config.yaml`.

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Dashboard UI (HTML) |
| `/api/data` | GET | Current usage data (JSON) |
| `/api/refresh` | GET | Force immediate refresh |

### Example API Response

```bash
curl http://localhost:8777/api/data
```

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "providers": {
    "codex": {
      "status": "ok",
      "plan": "plus",
      "limit_reached": false,
      "session": {
        "usage_pct": 45.2,
        "reset_at": "2024-01-15T18:00:00Z",
        "remaining_seconds": 27000
      },
      "weekly": {
        "usage_pct": 23.5,
        "reset_at": "2024-01-22T00:00:00Z",
        "remaining_seconds": 604800
      },
      "daily_breakdown": [...]
    },
    ...
  }
}
```

## Resource Usage

- **RAM**: ~20MB (typical)
- **Binary size**: ~15MB
- **No external dependencies** (single static binary)
- **CPU**: Minimal (only polls at configured interval)

## Security Notes

This dashboard is designed for **private, self-hosted use** on a trusted network.

- **No authentication** on the web interface. Place behind a reverse proxy with auth (Nginx, Caddy, Authelia) or access via VPN/Tailscale if exposed beyond localhost.
- **Protect your config.yaml** - it contains sensitive cookies and API keys. Set file permissions to 600:
  ```bash
  chmod 600 config.yaml
  ```
- **Use environment variables** for credentials when possible
- **Never commit config.yaml** with real credentials (it's in .gitignore)
- Port 8777 shows your usage data. Restrict access as needed.

## Troubleshooting

### No data showing

1. Check your config.yaml has correct cookies/tokens
2. Verify you can access the services in your browser
3. Check the console output for error messages
4. Test with `curl http://localhost:8777/api/data`

### Stale data

Session expired. Re-extract fresh cookies from your browser and update config.yaml.

### Z-AI not working

Verify your API key format is `id.secret` (two parts separated by a dot).

### Permission denied

Make sure the binary has execute permissions:

```bash
chmod +x agents-dashboard
```

## Development

```bash
# Run tests
go test ./...

# Run with race detection
go run -race .

# Build optimized binary
go build -ldflags "-s -w" -o agents-dashboard
```

## Tech Stack

- **Backend**: Go 1.24
- **HTTP Client**: tls-client (Firefox fingerprinting)
- **Auth**: JWT (Z-AI), Cookie-based (others)
- **Frontend**: Vanilla HTML/CSS/JS (embedded)
- **Config**: YAML with environment variable support

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
