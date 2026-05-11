# Asutosh's Portfolio

A lightweight, self-hosted email application designed for Raspberry Pi Zero 2 W. Features a modern dark UI with passkey authentication and both web-based and programmatic access to email sending.

## Features

- **Web UI**: Modern dark theme dashboard with login protection
- **Email Sender**: Send personalized job application emails with resume attachment
- **Cover Letter Generator**: Generate customized cover letter PDFs using Tectonic LaTeX
- **Job Tracker**: Add/upsert companies into a Supabase `applications` table, look up existing companies, view summary stats, and visualize a day/week/month timeline
- **Clipboard**: Save and copy text snippets and links
- **Dual Gmail Support**: University Gmail (OAuth API) or Personal Gmail (SMTP)
- **Offline-Capable UI**: No external CSS/font dependencies - works without internet for UI
- **Lightweight**: Optimized for Pi Zero 2 W (512MB RAM)
- **Programmatic API**: Send emails and generate cover letters via HTTP POST requests

## Project Structure

```
asutosh_portfolio/
├── main.go              # HTTP server, routes, authentication
├── http_meta.go         # Request ID + security headers + JSON error helper
├── tracker_query.go     # NL/SQL query endpoint (OpenRouter + read-only execution)
├── email.go             # Email building and sending logic
├── coverletter.go       # Cover letter PDF generation using Tectonic
├── clipboard.go         # Clipboard tool storage + API
├── tracker.go           # Job Tracker handlers and stats
├── oauth.go             # Gmail OAuth token management
├── sql/                 # Optional DB DDL (e.g. pg_trgm index)
├── go.mod / go.sum      # Go dependencies
├── build.sh             # Cross-compilation script
├── templates/           # UI HTML templates + runtime content templates
│   ├── login.html
│   ├── dashboard.html
│   ├── email.html
│   ├── coverletter.html
│   ├── clipboard.html
│   ├── tracker.html
│   ├── email_body.tmpl
│   └── coverletter.tex.tmpl
├── static/              # CSS files (embedded in binary)
│   └── zenith.css
└── pi_bundle/           # Deployment folder
    ├── .env             # Configuration (add your ACCESS_PASSKEY)
    ├── credentials.json # Google OAuth credentials
    ├── token.json       # Gmail API token
    └── templates/       # Runtime-editable templates
```

## Configuration

### Environment Variables (`.env`)

```env
# Gmail credentials
EMAIL="your-email@gmail.com"
PASSWORD="your-app-password"              # For SMTP (personal Gmail)
UNIVERSITY_EMAIL="your-university@edu"    # Optional, defaults to EMAIL
PERSONAL_EMAIL="your-personal@gmail.com"  # Optional, defaults to EMAIL

# Authentication
ACCESS_PASSKEY="your-secret-passkey"      # Required for web UI login

# Database (Job Tracker, backups, natural-language query)
DATABASE_URL="postgresql://user:pass@host:5432/dbname?sslmode=require"   # Required (direct Postgres URI; prefer 5432 over pooler for backups)
DATABASE_URL_READER=""                    # Optional: second URI (e.g. read-only role) for `/api/applications/query` only
DB_MAX_CONNS="4"                          # Optional: primary `pgxpool` max connections (default 4)
DB_READER_MAX_CONNS="4"                   # Optional: reader pool max connections (default 4, hard cap 8)
ENABLE_SUGGEST_TRGM="1"                   # Optional: use trigram-based autocomplete when `pg_trgm` is installed (see `sql/optional_pg_trgm.sql`)

# Job Tracker (Supabase)
SUPABASE_URL="https://<project-ref>.supabase.co"   # Required for Job Tracker tool
SUPABASE_SERVICE_KEY="<service_role_key>"          # Service-role key (bypasses RLS for inserts/updates)

# Optional
DEFAULT_SENDER_KEY="university"           # "university" or "personal"
RESUME_FILENAME="YOUR_RESUME.pdf"         # Name of resume file to attach
GMAIL_TOKEN_PATH="token.json"             # Path to OAuth token
EMAIL_TEMPLATE_PATH="templates/email_body.tmpl"              # Runtime email template path
COVERLETTER_TEMPLATE_PATH="templates/coverletter.tex.tmpl"   # Runtime cover letter template path
OPENROUTER_API_KEY="sk-or-v1-..."         # Required for Job Tracker "Query your data" (NL-to-SQL via OpenRouter)
OPENROUTER_MODEL="google/gemma-3n-e4b-it:free,minimax/minimax-m2.5:free,openai/gpt-oss-120b:free"   # Optional. Comma-separated: primary,fallback1,fallback2,...

# Database backups (native data.sql + optional schema via pg_dump)
DB_DUMP_DIR="db_dumps"                    # Where schema.sql and data.sql are written (relative to cwd)
DB_BACKUP_INTERVAL="24h"                  # How often the data dump is refreshed; Go duration string
DB_BACKUP_MAX_RETRIES="2"                 # Max re-dumps when verification fails
DB_BACKUP_ROW_TOLERANCE="0"               # Allowed |dump - live| row delta per table before flagging mismatch
DB_BACKUP_SCHEMAS="public"                # Comma-separated schemas to dump; "*" for all non-system
DISABLE_DB_BACKUP=""                      # Set to "1" / "true" to skip automated dumps entirely
```

**Operations / hardening:** the HTTP server uses timeouts and graceful shutdown on `SIGINT`/`SIGTERM`, sets security headers, rate-limits `POST /auth`, and exposes `GET /healthz` (database ping, no auth) for probes. Runtime templates (`email_body.tmpl`, `coverletter.tex.tmpl`) are embedded in the binary; `EMAIL_TEMPLATE_PATH` / `COVERLETTER_TEMPLATE_PATH` still override with files on disk when set.

**Autocomplete index:** for large `applications` tables, run `sql/optional_pg_trgm.sql` once, then set `ENABLE_SUGGEST_TRGM=1`.

**Layout refactor:** splitting the large `tracker.html` into a shared layout plus external CSS/JS is deferred to a follow-up change set (see audit roadmap).

> The Job Tracker reads and writes a Supabase `applications` table with the schema below. Reads work with the publishable/anon key, but inserts and updates are blocked by Row Level Security unless you use the **service-role key** (or add an open RLS policy). Get the service-role key from Supabase Dashboard → Project Settings → API.
>
> ```sql
> CREATE TABLE IF NOT EXISTS applications (
>     id SERIAL PRIMARY KEY,
>     organization VARCHAR(255),
>     job_role TEXT,
>     location VARCHAR(255),
>     contacts VARCHAR(255),
>     applied_dates DATE,
>     remarks TEXT,
>     status VARCHAR(50),
>     category VARCHAR(100),
>     count INTEGER,
>     username_password TEXT,
>     created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
> );
> ```

### Google OAuth Setup (for University Gmail)

1. Create a project in [Google Cloud Console](https://console.cloud.google.com/)
2. Enable the Gmail API
3. Create OAuth 2.0 credentials (Desktop app)
4. Download `credentials.json` to `pi_bundle/`
5. Generate `token.json` using Google's OAuth flow

### Automated Database Backups

After the DB ping succeeds, the server writes backups into `DB_DUMP_DIR` (default `db_dumps/`, relative to cwd). The same filenames are overwritten on every refresh; no timestamped names are created.

| File | When | How |
|------|------|-----|
| `db_dumps/data.sql` | At startup, then every `DB_BACKUP_INTERVAL` (default 24h) | **Native Go:** streams `COPY ... TO STDOUT` over your existing `DATABASE_URL` connection (via `pgx`) into PostgreSQL text-format `COPY ... FROM stdin` blocks—no `pg_dump` required for data. Tables are ordered using foreign-key dependencies when possible (cycles fall back to alphabetical order). |
| `db_dumps/schema.sql` | Once at startup | **Optional:** only if `pg_dump` is on `PATH`. If it is missing, this file is skipped; use your migrations, Supabase schema, or repo DDL (for example the `applications` snippet above and `ensureApplicationActivityLogs` in the code) as the schema source. |

Each dump is verified before it replaces the previous file:

- The data dump is parsed for `COPY ... FROM stdin` row counts per table, and compared with `SELECT count(*)` from the live DB. On a mismatch the dump is retried up to `DB_BACKUP_MAX_RETRIES` (default `2`), with a short sleep so any in-flight write can settle. `DB_BACKUP_ROW_TOLERANCE` (default `0`) lets you accept small deltas on busier DBs.
- When `schema.sql` is produced, it is checked by comparing the count of `CREATE TABLE` statements to `information_schema.tables` for the configured schemas.
- Output is written to `*.sql.tmp` and atomically renamed to the final filename only after verification, so consumers never see a half-written file. If verification still fails after all retries, the latest attempt is kept on disk and a `WARN` is logged with the table-by-table delta.

Requirements / notes:

- **`DATABASE_URL` must allow server-side `COPY ... TO STDOUT`.** Use a normal Postgres session (Supabase **direct** connection on port `5432` with `sslmode=require` is typical). Transaction pooling (port `6543`) can interfere with long-running or COPY-related operations; prefer the direct URI for backups.
- **`pg_dump` (optional).** Install PostgreSQL client tools if you want `schema.sql` snapshots (e.g. `apt install postgresql-client` on Raspberry Pi OS). Match the major version of your Supabase Postgres when possible. Data export does not use `pg_dump`.
- Set `DISABLE_DB_BACKUP=1` to turn backups off entirely (e.g. on a build host).

## Building

### Prerequisites

- Go 1.21+ installed on your build machine
- [Tectonic](https://tectonic-typesetting.github.io/) LaTeX engine (for cover letter generation)
- `pg_dump` on `PATH` only if you want automated `schema.sql` snapshots; `data.sql` backups do not require it

### Build for Raspberry Pi Zero 2 W

```bash
# Make build script executable
chmod +x build.sh

# Run the build
./build.sh
```

Or manually:

```bash
GOOS=linux GOARCH=arm64 go build -o pi_bundle/pi_portfolio_arm64
```

## Deployment

1. **Configure**: Edit `pi_bundle/.env` with your credentials and passkey
2. **Templates**: Keep `pi_bundle/templates/` with `email_body.tmpl` and `coverletter.tex.tmpl`
3. **Add Resume** (optional): Copy your resume PDF to `pi_bundle/`
4. **Transfer**: Zip and copy `pi_bundle/` to your Pi Zero 2 W
5. **Run**:
   ```bash
   cd pi_bundle
   chmod +x pi_portfolio_arm64
   ./pi_portfolio_arm64
   ```

The server starts on port **5001** by default.

## Usage

### Web Interface

1. Navigate to `http://<pi-ip>:5001`
2. Enter your access passkey to log in
3. Use the dashboard to access the Email Sender tool
4. Fill in recipient details and send

### Programmatic API Access

You can send emails programmatically using HTTP requests. This is useful for automation, scripts, or integrating with other tools.

#### Authentication

First, authenticate to get a session cookie:

```bash
# Get session cookie
curl -X POST http://<pi-ip>:5001/auth \
  -d "passkey=your-secret-passkey" \
  -c cookies.txt \
  -L
```

#### Send Email

With the session cookie, send an email:

```bash
curl -X POST http://<pi-ip>:5001/send-email \
  -H "Content-Type: application/json" \
  -b cookies.txt \
  -d '{
    "name": "John Smith",
    "company": "Acme Inc",
    "email": "john@acme.com",
    "sender_key": "university"
  }'
```

**Request Body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Recipient's name |
| `company` | string | Yes | Target company name |
| `email` | string | Yes | Recipient's email address |
| `sender_key` | string | No | `"university"` (Gmail API) or `"personal"` (SMTP). Defaults to `university` |

**Response:**

```json
// Success
{
  "success": true,
  "message": "Email sent successfully via University Gmail (API)!"
}

// Error
{
  "success": false,
  "error": "Error message here"
}
```

#### Python Example

```python
import requests

BASE_URL = "http://<pi-ip>:5001"
PASSKEY = "your-secret-passkey"

session = requests.Session()
session.post(f"{BASE_URL}/auth", data={"passkey": PASSKEY})

response = session.post(
    f"{BASE_URL}/send-email",
    json={
        "name": "Jane Doe",
        "company": "Tech Corp",
        "email": "jane@techcorp.com",
        "sender_key": "university",
    },
)

result = response.json()
if result["success"]:
    print("Email sent!")
else:
    print(f"Error: {result['error']}")
```

### Cover Letter Generator

Generate customized cover letter PDFs with your company name inserted. The PDF is compiled locally using [Tectonic](https://tectonic-typesetting.github.io/) and streamed directly to your browser for download.

#### Web Interface

1. Navigate to `http://<pi-ip>:5001/tools/cover-letter`
2. Enter the target company name
3. Click "Generate Cover Letter"
4. The PDF will download automatically

#### API Access

Generate a cover letter PDF via HTTP:

```bash
curl -X POST http://<pi-ip>:5001/generate-cover-letter \
  -H "Content-Type: application/json" \
  -b cookies.txt \
  -d '{"company": "Google"}' \
  --output CoverLetter_Google.pdf
```

**Request Body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `company` | string | Yes | Company name to insert into the cover letter |

**Response:**
- Success: Returns PDF binary with `Content-Type: application/pdf`
- Error: Returns JSON with `success: false` and `error` message

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/` | No | Redirects to `/login` or `/dashboard` |
| GET | `/login` | No | Login page |
| POST | `/auth` | No | Authenticate with passkey |
| GET | `/logout` | No | Clear session and logout |
| GET | `/dashboard` | Yes | Main dashboard |
| GET | `/tools/email` | Yes | Email sender tool |
| POST | `/send-email` | Yes | Send email (JSON API) |
| GET | `/tools/cover-letter` | Yes | Cover letter generator tool |
| POST | `/generate-cover-letter` | Yes | Generate cover letter PDF (JSON API) |
| GET | `/tools/clipboard` | Yes | Clipboard tool |
| GET | `/api/clipboard` | Yes | List/add clipboard items (GET/POST) |
| DELETE | `/api/clipboard/{id}` | Yes | Delete a clipboard item |
| PATCH | `/api/clipboard/{id}/move` | Yes | Reorder a clipboard item (`{ direction: "up" \| "down" }`) |
| GET | `/tools/tracker` | Yes | Job Tracker tool |
| GET | `/api/applications/check?company=NAME` | Yes | Check if a company is already logged |
| GET | `/api/applications/suggest?q=PREFIX` | Yes | Autocomplete suggestions for organization names |
| POST | `/api/applications` | Yes | Add a new company or add applications to an existing one |
| GET | `/api/applications/stats` | Yes | Aggregated tracker stats (companies, applications, %, etc.) |
| GET | `/api/applications/timeline?freq=day\|week\|month` | Yes | Bucketed application/company counts |
| POST | `/api/applications/query` | Yes | Run a read-only SELECT/WITH query on `applications` (natural-language or raw SQL mode) |
| GET | `/static/*` | No | Static assets (CSS) |

## Troubleshooting

### "ACCESS_PASSKEY not set"
Add `ACCESS_PASSKEY="your-secret"` to your `.env` file.

### Gmail API errors
- Ensure `credentials.json` and `token.json` are in the same directory as the binary
- Token may need refreshing if expired

### SMTP authentication failed
- Use an [App Password](https://support.google.com/accounts/answer/185833) for Gmail, not your regular password
- Enable "Less secure app access" or use App Passwords

### Port already in use
The server runs on port 5001. Kill any existing process or modify `main.go` to use a different port.

### Cover letter generation fails
- Ensure [Tectonic](https://tectonic-typesetting.github.io/) is installed and available in PATH
- On first run, Tectonic downloads required LaTeX packages automatically (requires internet)
- Check that the `tectonic` command works: `tectonic --version`

### Automated DB backups not running / verification mismatches
- `backup: pg_dump not found ... schema.sql skipped` — expected if client tools are not installed; `data.sql` still runs. Install `postgresql-client` (or similar) only if you want `schema.sql` via `pg_dump`.
- `native data dump: COPY ...` / pooler errors — switch `DATABASE_URL` to the **direct** Supabase connection (`5432`, `sslmode=require`) instead of transaction pooling (`6543`) if COPY or long sessions fail.
- `pg_dump schema: ...` — when `schema.sql` is enabled, install a `pg_dump` whose major version is >= the Supabase Postgres major version, or ignore and rely on migrations for schema.
- `data verify mismatch: <table>: dump=N live=M` — usually means writes happened between the dump finishing and the verification query; the loop retries automatically. If you legitimately have a busy DB, raise `DB_BACKUP_ROW_TOLERANCE`
- `WARN data dump kept despite mismatch` — retries were exhausted; the latest attempt is still on disk at `db_dumps/data.sql` for forensic use, but treat it as best-effort until the next successful refresh

## License

MIT
