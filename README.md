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
├── email.go             # Email building and sending logic
├── coverletter.go       # Cover letter PDF generation using Tectonic
├── clipboard.go         # Clipboard tool storage + API
├── tracker.go           # Job Tracker: Supabase REST client + handlers
├── oauth.go             # Gmail OAuth token management
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
```

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

## Building

### Prerequisites

- Go 1.21+ installed on your build machine
- [Tectonic](https://tectonic-typesetting.github.io/) LaTeX engine (for cover letter generation)

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

## License

MIT
