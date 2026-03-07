# Codex OAuth Setup Guide

## Overview

Codex uses OAuth tokens from Codex CLI for reliable access without Cloudflare blocking.

## Quick Setup (5 minutes)

### Step 1: Install Codex CLI

**macOS:**
```bash
brew install codex
```

**Linux:**
```bash
# Download from GitHub releases
# Visit: https://github.com/openai/codex/releases
wget https://github.com/openai/codex/releases/latest/download/codex-linux-amd64
chmod +x codex-linux-amd64
sudo mv codex-linux-amd64 /usr/local/bin/codex
```

**Windows:**
```bash
# Download from: https://github.com/openai/codex/releases
# Add to PATH
```

### Step 2: Authenticate

```bash
codex login
```

This opens your browser. Log in to ChatGPT with your OpenAI account.

### Step 3: Verify Tokens

```bash
ls -la ~/.codex/auth.json
cat ~/.codex/auth.json | jq '.tokens.access_token' | head -c 50
```

You should see a JSON file with OAuth tokens.

### Step 4: Optional - Uninstall CLI

If you don't need Codex CLI for development:

```bash
brew uninstall codex  # macOS
# Linux: rm /usr/local/bin/codex
```

**Tokens remain in `~/.codex/auth.json`** and continue working for weeks/months.

### Step 5: Start Dashboard

```bash
./agents-dashboard
```

Codex provider will automatically use the OAuth tokens.

## How It Works

1. Codex CLI performs OAuth2 authentication with OpenAI
2. Tokens are stored in `~/.codex/auth.json` (standard JSON)
3. Dashboard reads tokens and calls ChatGPT API directly
4. No Cloudflare, no cookies, reliable access
5. CLI is optional after initial setup

## Token Expiration

- **Lifetime**: Tokens last several weeks to months
- **Detection**: Dashboard logs warning if tokens are >7 days old
- **Refresh**: Run `codex login` again to refresh tokens

## Troubleshooting

### "Token file not found"

```bash
# Run authentication
codex login
```

### "Token expired (status 401)"

```bash
# Re-authenticate
codex login
```

### "Parse error"

```bash
# Verify file format
cat ~/.codex/auth.json | jq '.'
# Should show valid JSON with "tokens" object
```

### "access_token missing"

The token file may be corrupted. Re-run:

```bash
codex login
```

## Manual Token Extraction (Advanced)

If you cannot install Codex CLI, you can manually extract OAuth tokens from your browser:

1. Open chatgpt.com in Chrome
2. DevTools → Application → Local Storage → chatgpt.com
3. Find OAuth-related keys
4. Create `~/.codex/auth.json`:

```json
{
  "tokens": {
    "access_token": "eyJ...",
    "refresh_token": "...",
    "id_token": "eyJ...",
    "account_id": "..."
  },
  "last_refresh": "2026-03-07T11:00:00Z"
}
```

⚠️ **Note**: Manual extraction is fragile. CLI method strongly recommended.

## Technical Details

### OAuth Token Structure

```json
{
  "tokens": {
    "access_token": "eyJhbGciOiJSUzI1NiIs...",
    "refresh_token": "def50200...",
    "id_token": "eyJhbGciOiJSUzI1NiIs...",
    "account_id": "org-..."
  },
  "last_refresh": "2026-03-07T11:00:00Z"
}
```

### API Endpoint

Dashboard calls: `GET https://chatgpt.com/backend-api/wham/usage`

Headers:
```
Authorization: Bearer <access_token>
User-Agent: AgentsDashboard/1.0
Accept: application/json
ChatGPT-Account-Id: <account_id>  (if available)
```

### Why CLI Can Be Uninstalled

- Tokens are standalone JWTs (JSON Web Tokens)
- No CLI-specific encryption or binding
- Dashboard only reads JSON file
- CLI is purely a token fetcher

## Security Notes

- **Token file location**: `~/.codex/auth.json` (user home directory)
- **File permissions**: Should be 600 (read/write for owner only)
  ```bash
  chmod 600 ~/.codex/auth.json
  ```
- **Do not commit**: Add `.codex/` to `.gitignore`
- **Token rotation**: OpenAI may rotate tokens periodically

## Alternative: Keep CLI Installed

If you use Codex CLI for development, keep it installed:

```bash
# CLI stays installed, dashboard uses same tokens
brew install codex
codex login
# Both CLI and dashboard work together
```

Benefits:
- Automatic token refresh when using CLI
- No need to manually re-authenticate
- Seamless development workflow
