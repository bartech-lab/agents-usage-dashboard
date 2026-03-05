# Architecture

## Overview

Agents Usage Monitor is a self-contained Go binary that monitors AI assistant usage limits across multiple providers. It features a robust auto-refresh system, embedded web interface, and zero external dependencies.

## Data Flow

```
┌─────────────────────────────────────────────────────────┐
│  agents-dashboard (Go Binary)                           │
│                                                         │
│  .env/config.yaml                                       │
│         │                                               │
│         ↓                                               │
│  ┌──────────────┐                      ┌─────────────┐  │
│  │  Config      │     credentials      │ HTTP Client │  │
│  │  Loader      │   ───────────────→   │ (tls-client)│  │
│  └──────────────┘                      └──────┬──────┘  │
│                                               │         │
│                                               ↓         │
│                                      ┌────────────────┐ │
│                                      │ External APIs  │ │
│                                      │ • kimi.com     │ │
│                                      │ • z.ai         │ │
│                                      │ • chatgpt.com  │ │
│                                      │ • claude.ai    │ │
│                                      └───────┬────────┘ │
│                                              │          │
│                                              ↓          │
│  ┌──────────────┐   fetch & cache   ┌─────────────┐     │
│  │  Scheduler   │  ←──────────────  │   Provider  │     │
│  │  (5min poll) │                   │   Fetchers  │     │
│  └──────┬───────┘                   └─────────────┘     │
│         │                                               │
│         ↓ data                                          │
│  ┌────────────────────────────────────┐                 │
│  │  HTTP Server (:8777)               │                 │
│  │  • /         → Dashboard UI        │                 │
│  │  • /api/data → JSON API            │                 │
│  │  • /api/refresh → Force refresh    │                 │
│  └────────────────────────────────────┘                 │
└─────────────────────────────────────────────────────────┘
```

## Components

### 1. Configuration Loader (`config.go`)

**Purpose:** Load and validate configuration from YAML and environment variables

**Process:**
1. Loads `.env` file using `godotenv` (automatic, on startup)
2. Parses `config.yaml` with environment variable interpolation (`${VAR}`)
3. Validates configuration (at least one provider configured)
4. Applies defaults (refresh interval: 5min, port: 8777)

**Security:**
- Supports environment variable substitution for secrets
- Validates provider configuration before starting
- File permissions check (recommends `chmod 600 config.yaml`)

### 2. Scheduler (`scheduler.go`)

**Purpose:** Background data fetching with thread-safe caching

**Process:**
1. **Initial fetch** on startup (parallel provider requests)
2. **Background polling** every `REFRESH_INTERVAL` (default: 5 minutes)
3. **Manual refresh** via `/api/refresh` endpoint
4. **Thread-safe caching** with mutex protection

**Resilience:**
- Handles provider failures gracefully
- Shows stale data when providers unreachable
- Maintains last successful data for recovery
- Atomic fetch lock prevents concurrent refreshes

**Provider Fetching:**
- **Kimi**: Cookie-based auth (`kimi-auth` token)
- **Z-AI**: API key auth (JWT generation)
- **Codex**: Cookie-based auth (ChatGPT session)
- **Claude**: Cookie-based auth (session key)

### 3. HTTP Client (`providers.go`)

**Purpose:** Authenticated API requests with TLS fingerprinting

**Features:**
- Uses `tls-client` library with Firefox fingerprint
- Avoids bot detection by mimicking real browser
- Handles cookie-based and API key authentication
- Parses JSON responses with custom types (e.g., `FlexInt` for string/int fields)

**Request Flow:**
```
Config → Build Request → Add Auth Headers → Execute → Parse Response → Cache
```

### 4. HTTP Server (`main.go`)

**Purpose:** Serve dashboard UI and API endpoints

**Endpoints:**
- **`GET /`** - Dashboard UI (embedded HTML)
- **`GET /api/data`** - Current cached data (JSON)
- **`GET /api/refresh`** - Force immediate refresh, return new data

**Implementation:**
- Single HTTP server on configured port (default: 8777)
- Embedded frontend (no external files needed)
- JSON responses with proper headers
- Graceful shutdown on SIGTERM/SIGINT

### 5. Frontend (`dashboard.html`)

**Purpose:** Real-time dashboard with auto-refresh

**Features:**
- **Vanilla JS** - No frameworks, embedded in binary
- **Auto-refresh** - Robust polling with visibility API
- **Live countdown** - Shows next refresh time
- **Background tab support** - Refreshes when switching back to tab
- **Staleness detection** - Immediate refresh if data >30s old
- **Error tracking** - Logs consecutive failures

**Refresh Strategy:**
```javascript
// Poll every minute, but only fetch if:
// 1. Backend says it's time (next_refresh_at)
// 2. Data is stale (>30s old)
// 3. User switches back to tab with stale data

setInterval(() => {
  if (shouldFetch() || isDataStale()) {
    fetchData();
  }
}, 60000);
```

**UI Components:**
- Agent cards (2 per row)
- Usage bars (session + weekly)
- Countdown timer
- Color-coded status (ok/warn/critical)

## Data Models (`models.go`)

### CacheData
```go
type CacheData struct {
    Kimi          *ProviderData
    Zai           *ProviderData
    Codex         *ProviderData
    Claude        *ProviderData
    LastFetch     string
    NextRefreshAt string
}
```

### ProviderData
```go
type ProviderData struct {
    Status         string  // "ok", "error", "offline", "stale"
    Plan           string  // "Moderato", "pro", "plus", etc.
    Session        *UsageWindow
    Weekly         *UsageWindow
    Models         *ClaudeModels  // Claude-specific
    DailyBreakdown []DailyEntry   // Codex-specific
    Error          string
    LastSuccess    string
}
```

### UsageWindow
```go
type UsageWindow struct {
    UsagePct         float64
    ResetAt          string
    RemainingSeconds int
    Used             int  // Kimi-specific
    Limit            int  // Kimi-specific
    Remaining        int  // Kimi-specific
}
```

## Security Model

### Authentication
- **No web auth** - Designed for trusted networks only
- **Provider auth** - Cookie-based (Kimi, Codex, Claude) or API key (Z-AI)
- **Environment isolation** - Secrets in `.env`, not committed to git

### Network Security
- **Localhost only** by default (bind to `:8777`)
- **Reverse proxy recommended** for external access (Nginx, Caddy, Authelia)
- **VPN/Tailscale** for remote access

### Data Security
- **No persistent storage** - All data in memory
- **Read-only config** - Never modifies configuration files
- **Sanitized errors** - No sensitive data in error messages

## Performance

### Resource Usage
- **RAM**: ~20MB typical
- **Binary size**: ~15MB (static linking)
- **CPU**: Minimal (only polls at configured interval)
- **Network**: 4 parallel HTTP requests every 5 minutes

### Scalability
- **Single binary** - No horizontal scaling needed
- **In-memory cache** - Fast API responses
- **Thread-safe** - Concurrent request handling

## Error Handling

### Provider Failures
1. **Network error** → Show stale data (if available) with error message
2. **Auth failure** → Mark as "error", preserve last successful data
3. **Parse error** → Log details, return error status

### Recovery
- **Automatic retry** on next poll cycle
- **Stale data preserved** until successful fetch
- **Error counter** tracks consecutive failures

### Logging
- **Console output** for server events
- **Browser console** for frontend errors
- **Structured errors** in API responses

## Deployment

### Build
```bash
go build -o agents-dashboard
```

### Run
```bash
./agents-dashboard
```

### Options
- **Configuration**: Edit `config.yaml` and `.env`
- **Port**: Change `server_port` in config
- **Interval**: Change `refresh_interval` in config

### Production Checklist
- [ ] Set up `.env` with credentials
- [ ] Configure `config.yaml` for providers
- [ ] Set file permissions: `chmod 600 config.yaml .env`
- [ ] Place behind reverse proxy with auth
- [ ] Use systemd/launchd for auto-start
- [ ] Monitor logs for errors

## Comparison with Python Version

The Go version replaces the Python/Docker architecture with a simpler, more efficient design:

| Aspect | Python (Legacy) | Go (Current) |
|--------|----------------|--------------|
| **Deployment** | Docker Compose (2 containers) | Single binary |
| **Dependencies** | Python, Flask, Firefox container | None (static binary) |
| **Resource usage** | ~200MB+ (Firefox + Python) | ~20MB |
| **Setup complexity** | High (Docker, Firefox, volumes) | Low (build + run) |
| **Auth method** | Firefox cookies | Browser cookie extraction |
| **Auto-refresh** | Basic polling | Robust with visibility API |
| **Startup time** | Slow (containers) | Instant |

The Python version is preserved in `legacy/` for reference.
