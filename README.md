# Asutosh's Dashboard

A self-hosted web dashboard for sending job application emails, generating cover letters, tracking applications, and managing a clipboard — cross-compiled for **Raspberry Pi (DietPi)** and deployed via **GitHub Actions CI/CD**.

![Dashboard](images/app_screenshot.png)

## Features

- **Email Sender** — dual Gmail support (University via OAuth API, Personal via SMTP)
- **Cover Letter Generator** — LaTeX-generated PDFs via Tectonic
- **Job Tracker** — CRUD + stats + timeline + natural-language SQL query against Supabase
- **Clipboard** — save and copy text snippets
- **LLM Chat** — persistent chat sessions with OpenRouter models
- **Resume Tailor** — ATS scoring, keyword optimization, live PDF preview
- **Passkey auth** — session-based login

## CI/CD

Every push to `main` triggers a GitHub Actions workflow that:

1. Runs tests
2. Cross-compiles the Go binary for `linux/arm64`
3. Injects `.env` from repository secrets
4. Bundles everything into `myapp-linux-arm64.tar.gz`
5. Creates a GitHub Release with the tarball attached

## Deploy on Raspberry Pi

```bash
# One-time setup
echo 'ghp_xxx' > ~/.github_token && chmod 600 ~/.github_token

# Pull the latest release
./deploy.sh /opt/pi_portfolio

# Run
/opt/pi_portfolio/pi_portfolio_arm64
```

## Local Build

```bash
./build.sh
```

## Project Structure

```
├── main.go                    # HTTP server, routes, auth
├── internal/
│   ├── auth/                  # Passkey authentication
│   ├── clipboard/             # Clipboard tool
│   ├── config/                # Environment config
│   ├── coverletter/           # Cover letter PDF generation
│   ├── database/              # DB connection + migrations
│   ├── email/                 # Email sending (SMTP + Gmail API)
│   ├── llm/                   # LLM service (OpenRouter/Ollama)
│   ├── middleware/             # Auth, rate-limiting, logging
│   ├── router/                # HTTP router
│   ├── tracker/               # Job tracker
│   └── workspace/             # Workspace/chat features
├── pkg/
│   ├── mail/                  # Email templates
│   ├── ollama/                # Ollama client
│   ├── openrouter/            # OpenRouter client
│   └── observability/         # Logging
├── templates/                 # HTML templates
├── web/static/                # Tailwind CSS source
├── static/                    # Compiled CSS/JS
├── sql/                       # DB scripts
├── deploy.sh                  # Pull-based updater for Pi
├── build.sh                   # Local build script
└── .github/workflows/         # GitHub Actions CI/CD
```

## Configuration

Create a `.env` file with your secrets. Required variables:

| Variable | Description |
|----------|-------------|
| `DATABASE_URL` | Postgres connection URI |
| `ACCESS_PASSKEY` | Web login passkey |
| `EMAIL` / `PASSWORD` | Gmail SMTP credentials |
| `UNIVERSITY_EMAIL` / `PERSONAL_EMAIL` | Sender addresses |
| `OPENROUTER_API_KEY` | For LLM features |

Gmail API (OAuth) credentials:

| Variable | Description |
|----------|-------------|
| `GMAIL_ACCESS_TOKEN` | OAuth2 access token |
| `GMAIL_REFRESH_TOKEN` | OAuth2 refresh token |
| `GMAIL_CLIENT_ID` | OAuth2 client ID |
| `GMAIL_CLIENT_SECRET` | OAuth2 client secret |
| `GMAIL_TOKEN_URI` | Token endpoint URL |
| `GMAIL_EXPIRY` | Token expiry timestamp |

For the CI/CD pipeline, store the full `.env` in **GitHub Secrets** (`Settings → Secrets and variables → Actions`):

| Secret | Content |
|--------|---------|
| `PROD_ENV_FILE` | Full `.env` file contents |

## Tech Stack

- **Go** — single-binary HTTP server
- **Supabase** — Postgres database
- **Tectonic** — LaTeX PDF generation
- **OpenRouter** — LLM API gateway
- **ngrok** — public tunnel (optional)
- **GitHub Actions** — cross-compile & release pipeline

## Email API

The emailer exposes a JSON API endpoint. It requires session authentication (login via the web UI first to get a `session_token` cookie).

**Endpoint:** `POST /send-email`

**Request body:**
```json
{
  "name": "John Smith",
  "company": "Google",
  "email": "john@google.com",
  "sender_key": "university"
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Recipient's name |
| `company` | Yes | Target company |
| `email` | Yes | Recipient's email address |
| `sender_key` | No | `university` (default) or `personal` |

**Example with curl:**
```bash
# First login to get session cookie
curl -c cookies.txt -X POST https://your-domain:5001/login \
  -d "passkey=your_passkey"

# Send email
curl -b cookies.txt -X POST https://your-domain:5001/send-email \
  -H "Content-Type: application/json" \
  -d '{"name":"John Smith","company":"Google","email":"john@google.com"}'
```

**Response (success):**
```json
{"success": true, "message": "Email sent via Gmail API"}
```

**Response (error):**
```json
{"success": false, "error": "Missing required fields"}
```
