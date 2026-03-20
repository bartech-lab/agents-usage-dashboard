# Authentication Guide

> **⚠️ Important:** Codex now uses **OAuth tokens** instead of cookies. Cookie-based Codex authentication is deprecated and blocked by Cloudflare. See [codex-oauth.md](codex-oauth.md) for Codex setup. This guide covers cookie-based auth for **Kimi** and **Claude** only.

This guide explains how to extract authentication credentials for each AI assistant provider.

## Overview

The Agents Usage Monitor uses three authentication methods:

1. **Cookie-based authentication** - For Kimi and Claude
2. **API key authentication** - For Z-AI
3. **OAuth tokens** - For Codex (recommended, see [codex-oauth.md](codex-oauth.md))

## Cookie Extraction (Chrome/Chromium)

### Prerequisites

- Log in to the service you want to monitor in your browser
- Keep the browser tab open during extraction

### Step-by-Step Process

#### 1. Open Developer Tools

**Chrome/Edge/Brave:**
- Press `F12` or `Ctrl+Shift+I` (Windows/Linux)
- Press `Cmd+Option+I` (macOS)
- Or right-click → Inspect

#### 2. Navigate to Cookies

1. Click the **Application** tab in Developer Tools
2. Expand **Cookies** in the left sidebar
3. Click on the domain (e.g., `https://chatgpt.com`)

#### 3. Extract Cookie Value

1. Find the required cookie name (see table below)
2. Double-click the **Value** field to select it
3. Copy the value (`Ctrl+C` / `Cmd+C`)

#### 4. Add to Configuration

Add the cookie value to your `.env` file:

```bash
KIMI_AUTH_TOKEN=eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9...
```

Or directly in `config.yaml`:

```yaml
providers:
  kimi:
    cookies:
      "kimi.com":
        "kimi-auth": "eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9..."
```

## Required Cookies by Provider

| Provider | Domain | Cookie Name | Environment Variable |
|----------|--------|-------------|---------------------|
| **Z-AI** | z.ai | API Key (not cookie) | `ZAI_API_KEY` |
| **Kimi** | kimi.com | `kimi-auth` | `KIMI_AUTH_TOKEN` |
| **Codex** | chatgpt.com | `__Secure-next-auth.session-token` | `CODEX_SESSION_TOKEN` |
| **Claude** | claude.ai | `sessionKey` | `CLAUDE_SESSION_KEY` |

### Provider-Specific Notes

#### Kimi Code (kimi.com)

- **Cookie:** `kimi-auth`
- **Format:** JWT token (long string starting with `eyJ`)
- **Validity:** Long-lived (weeks/months)
- **Tip:** The token is valid across sessions; no need to refresh frequently

#### OpenAI Codex (chatgpt.com)

- **Primary cookie:** `__Secure-next-auth.session-token`
- **Additional cookies:** May need `__Secure-next-auth.callback-url` and others
- **Validity:** Session-based, expires after inactivity
- **Tip:** Extract all cookies from chatgpt.com domain if the primary cookie doesn't work

#### Claude (claude.ai)

- **Cookie:** `sessionKey`
- **Format:** UUID-like string
- **Validity:** Session-based
- **Tip:** Log in to claude.ai before extraction

#### Z-AI (z.ai)

- **Authentication:** API Key (not cookie-based)
- **Format:** `id.secret` (two parts separated by a dot)
- **Get your key:** [z.ai/manage-apikey/apikey-list](https://z.ai/manage-apikey/apikey-list)
- **Tip:** Treat like a password - never commit to version control

## Alternative Browsers

The process is similar in other browsers:

### Firefox
1. Press `F12` → **Storage** tab → **Cookies**
2. Find and copy the required cookie

### Safari
1. Enable Developer Menu: Safari → Preferences → Advanced → Show Develop menu
2. Press `Cmd+Option+I` → **Storage** tab → **Cookies**
3. Find and copy the required cookie

## Security Best Practices

### ✅ DO
- Use environment variables (`.env` file)
- Add `.env` to `.gitignore` (config.yaml uses environment variable references like `${VAR}` and is safe to commit)
- Set file permissions: `chmod 600 .env`
- Rotate credentials periodically
- Use a password manager to store tokens

### ❌ DON'T
- Commit credentials to version control
- Share tokens in chat/email
- Use production tokens in development
- Store tokens in plain text files

## Troubleshooting

### Cookie Expired
**Symptom:** Provider shows "Auth failed - reconnect needed"

**Solution:**
1. Log in to the service in your browser
2. Extract fresh cookie
3. Update `.env` file
4. Restart the dashboard

### Invalid Cookie Format
**Symptom:** "Cookie extraction failed" error

**Solution:**
1. Ensure you copied the entire cookie value
2. Check for extra spaces or line breaks
3. Verify you're copying the **Value** field, not the name

### Multiple Cookies Required (Codex)
**Symptom:** Codex authentication fails with session token alone

**Solution:**
1. Extract all cookies from chatgpt.com domain
2. Add them to config.yaml:
```yaml
providers:
  codex:
    cookies:
      "chatgpt.com":
        "__Secure-next-auth.session-token": "value1"
        "__Secure-next-auth.callback-url": "value2"
        # Add other cookies as needed
```

### Z-AI API Key Format
**Symptom:** "Invalid API key format"

**Solution:**
1. Ensure format is `id.secret` (exactly one dot)
2. Example: `${ZAI_API_KEY}`
3. Get fresh key from [z.ai/manage-apikey/apikey-list](https://z.ai/manage-apikey/apikey-list)

## Environment Variable Template

Use this template in your `.env` file:

```bash
# Z-AI - Get from https://z.ai/manage-apikey/apikey-list
ZAI_API_KEY=your-id.your-secret

# Kimi Code - Extract from kimi.com cookies
KIMI_AUTH_TOKEN=your-kimi-jwt-token

# OpenAI Codex - Extract from chatgpt.com cookies
CODEX_SESSION_TOKEN=your-session-token

# Claude - Extract from claude.ai cookies
CLAUDE_SESSION_KEY=your-session-key
```

## Refresh Frequency

Cookie-based authentication tokens have different lifespans:

| Provider | Typical Lifetime | Refresh Recommendation |
|----------|-----------------|----------------------|
| Z-AI API Key | Months/Years | When revoked or rotated |
| Kimi | Weeks/Months | When auth fails |
| Codex | Days/Weeks | When auth fails |
| Claude | Days/Weeks | When auth fails |

The dashboard will automatically detect authentication failures and display error status, prompting you to refresh credentials.
