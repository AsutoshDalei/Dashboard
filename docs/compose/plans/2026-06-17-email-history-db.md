# Email History Database Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use compose:subagent (recommended) or compose:execute to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a database-backed email history system that tracks sent emails, shows duplicate warnings in the UI, and enforces safety on programmatic sends.

**Architecture:** New `emails` table in existing Supabase PostgreSQL. Repository pattern (like clipboard/tracker). New check endpoint for UI dot indicator. New programmatic send endpoint with safe/unsafe flag. UI gets blur-triggered status dot.

**Tech Stack:** Go, PostgreSQL (Supabase), pgx, vanilla JS, Tailwind CSS

---

### Task 1: Database Migration

**Covers:** [S1]

**Files:**
- Modify: `internal/database/migrations.go`

- [ ] **Step 1: Add migration to migrations.go**

Add after the last migration (004_clipboard_sort_order):

```go
{
    Name: "005_emails",
    SQL: `CREATE TABLE IF NOT EXISTS emails (
        id SERIAL PRIMARY KEY,
        recipient_name TEXT NOT NULL,
        recipient_email TEXT NOT NULL,
        template_key TEXT NOT NULL,
        sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
    );
    CREATE INDEX IF NOT EXISTS idx_emails_recipient_email ON emails(recipient_email);`,
},
```

- [ ] **Step 2: Verify migration runs**

```bash
go build ./...
```

Expected: Build succeeds. Migration runs automatically on next server start.

- [ ] **Step 3: Commit**

```bash
git add internal/database/migrations.go
git commit -m "feat(email): add emails table migration"
```

---

### Task 2: Email Repository

**Covers:** [S1, S2, S3]

**Files:**
- Create: `internal/email/repository.go`

- [ ] **Step 1: Create repository.go**

```go
package email

import (
    "context"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

type EmailRecord struct {
    ID        int       `json:"id"`
    Name      string    `json:"name"`
    Email     string    `json:"email"`
    Template  string    `json:"template"`
    SentAt    time.Time `json:"sent_at"`
}

type Repository struct {
    pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
    return &Repository{pool: pool}
}

func (r *Repository) FindByEmail(ctx context.Context, email string) (*EmailRecord, error) {
    var rec EmailRecord
    err := r.pool.QueryRow(ctx,
        `SELECT id, recipient_name, recipient_email, template_key, sent_at
         FROM emails WHERE recipient_email = $1
         ORDER BY sent_at DESC LIMIT 1`, email,
    ).Scan(&rec.ID, &rec.Name, &rec.Email, &rec.Template, &rec.SentAt)
    if err != nil {
        return nil, err
    }
    return &rec, nil
}

func (r *Repository) RecordSend(ctx context.Context, name, email, templateKey string) error {
    _, err := r.pool.Exec(ctx,
        `INSERT INTO emails (recipient_name, recipient_email, template_key, sent_at)
         VALUES ($1, $2, $3, NOW())
         ON CONFLICT (recipient_email) DO UPDATE
         SET recipient_name = EXCLUDED.recipient_name,
             template_key = EXCLUDED.template_key,
             sent_at = NOW()`, name, email, templateKey,
    )
    if err != nil {
        return fmt.Errorf("record send: %w", err)
    }
    return nil
}
```

- [ ] **Step 2: Add unique constraint for upsert**

Update migration in `internal/database/migrations.go` to add unique constraint:

```go
{
    Name: "005_emails",
    SQL: `CREATE TABLE IF NOT EXISTS emails (
        id SERIAL PRIMARY KEY,
        recipient_name TEXT NOT NULL,
        recipient_email TEXT NOT NULL UNIQUE,
        template_key TEXT NOT NULL,
        sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
    );
    CREATE INDEX IF NOT EXISTS idx_emails_recipient_email ON emails(recipient_email);`,
},
```

- [ ] **Step 3: Verify build**

```bash
go build ./...
```

Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add internal/email/repository.go internal/database/migrations.go
git commit -m "feat(email): add email repository with FindByEmail and RecordSend"
```

---

### Task 3: Wire Repository into Handler and Service

**Covers:** [S2, S3]

**Files:**
- Modify: `internal/email/model.go` — add `EmailCheckResponse`, `SafetyFlag` constants
- Modify: `internal/email/handler.go` — add repo field, `HandleCheckEmail`, `HandleSendAPI`, update `HandleSend`
- Modify: `internal/email/service.go` — no changes needed (handler calls repo directly)
- Modify: `internal/router/router.go` — register new routes
- Modify: `main.go` — create repo, pass to handler

- [ ] **Step 1: Update model.go**

Add after `EmailTemplate`:

```go
type EmailCheckResponse struct {
    Exists   bool   `json:"exists"`
    Name     string `json:"name,omitempty"`
    Template string `json:"template,omitempty"`
    SentAt   string `json:"sent_at,omitempty"`
}

type EmailAPIRequest struct {
    Name        string `json:"name"`
    Company     string `json:"company"`
    Email       string `json:"email"`
    SenderKey   string `json:"sender_key"`
    TemplateKey string `json:"template_key"`
    Role        string `json:"role"`
    Safety      string `json:"safety"`
}
```

- [ ] **Step 2: Update handler.go — add repo and endpoints**

Replace the Handler struct and add new methods:

```go
type Handler struct {
    svc    *Service
    tmpl   *template.Template
    config *GmailConfig
    repo   *Repository
}

func NewHandler(svc *Service, tmpl *template.Template, gmailConfig *GmailConfig, repo *Repository) *Handler {
    return &Handler{svc: svc, tmpl: tmpl, config: gmailConfig, repo: repo}
}

func (h *Handler) HandleCheckEmail(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
        return
    }
    email := r.URL.Query().Get("email")
    if email == "" {
        middleware.RespondJSON(w, http.StatusBadRequest, false, "Missing email parameter", "")
        return
    }
    rec, err := h.repo.FindByEmail(r.Context(), email)
    if err != nil {
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(EmailCheckResponse{Exists: false})
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(EmailCheckResponse{
        Exists:   true,
        Name:     rec.Name,
        Template: rec.Template,
        SentAt:   rec.SentAt.Format("Jan 02, 2006 3:04 PM"),
    })
}

func (h *Handler) HandleSendAPI(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
        return
    }

    var req EmailAPIRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
        return
    }

    if req.Name == "" || req.Company == "" || req.Email == "" {
        middleware.RespondJSON(w, http.StatusBadRequest, false, "Missing required fields", "")
        return
    }
    if _, err := mail.ParseAddress(req.Email); err != nil {
        middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid email address", "")
        return
    }

    if req.SenderKey == "" {
        req.SenderKey = "university"
    }
    if req.TemplateKey == "" {
        req.TemplateKey = "referral"
    }
    if req.Safety == "" {
        req.Safety = "safe"
    }

    existing, _ := h.repo.FindByEmail(r.Context(), req.Email)
    if existing != nil && req.Safety == "safe" {
        middleware.RespondJSON(w, http.StatusConflict, false,
            fmt.Sprintf("Email already sent to %s on %s via %s",
                req.Email, existing.SentAt.Format("Jan 02, 2006"), existing.Template), "")
        return
    }

    provider := NewGmailProvider(
        h.config.AccessToken, h.config.RefreshToken,
        h.config.ClientID, h.config.ClientSecret,
        h.config.TokenURI, h.config.Expiry,
    )
    senderLabel, err := h.svc.Send(req.Email, req.Name, req.Company,
        strings.ToLower(strings.TrimSpace(req.SenderKey)), req.TemplateKey, req.Role, provider)
    if err != nil {
        middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
        return
    }

    _ = h.repo.RecordSend(r.Context(), req.Name, req.Email, req.TemplateKey)

    middleware.RespondJSON(w, http.StatusOK, true, "", "Email sent via "+senderLabel)
}
```

- [ ] **Step 3: Update HandleSend to record sends**

In `HandleSend`, add after successful send (before the final RespondJSON):

```go
_ = h.repo.RecordSend(r.Context(), req.Name, req.Email, req.TemplateKey)
```

- [ ] **Step 4: Update main.go to create repo and pass to handler**

Find where `email.NewHandler` is called and add repo:

```go
emailRepo := email.NewRepository(dbPool.Pool)
emailHandler := email.NewHandler(emailSvc, templates, gmailConfig, emailRepo)
```

- [ ] **Step 5: Register new routes in router.go**

Add after existing email routes:

```go
r.mux.HandleFunc("/api/email/check", auth(r.deps.Email.HandleCheckEmail))
r.mux.HandleFunc("/api/email/send", auth(r.deps.Email.HandleSendAPI))
```

- [ ] **Step 6: Verify build**

```bash
go build ./...
```

Expected: Build succeeds.

- [ ] **Step 7: Commit**

```bash
git add internal/email/model.go internal/email/handler.go internal/router/router.go main.go
git commit -m "feat(email): add check endpoint, programmatic send API, and record sends"
```

---

### Task 4: UI Dot Indicator

**Covers:** [S4]

**Files:**
- Modify: `templates/email.html` — add dot element, blur handler, tooltip CSS

- [ ] **Step 1: Add dot element next to email field**

After the email input div (the `mb-6` div containing `emailAddress`), add:

```html
<div id="emailStatus" class="flex items-center gap-1.5 mt-1 mb-6 hidden">
    <span id="emailDot" class="w-2 h-2 rounded-full"></span>
    <span id="emailTooltip" class="text-xs text-on-surface-muted"></span>
</div>
```

Replace the existing email field div to include the status:

```html
<div class="mb-6">
    <label class="block text-sm font-medium text-on-surface-variant mb-1" for="emailAddress">Recipient's Email</label>
    <input type="email" id="emailAddress" class="w-full px-3 py-2 rounded-lg bg-surface-high text-on-surface border border-outline focus:border-primary focus:outline-none focus:ring-1 focus:ring-primary placeholder:text-on-surface-muted text-sm" placeholder="e.g. john@company.com" autocomplete="off" required>
    <div id="emailStatus" class="flex items-center gap-1.5 mt-1 hidden">
        <span id="emailDot" class="w-2 h-2 rounded-full"></span>
        <span id="emailTooltip" class="text-xs text-on-surface-muted"></span>
    </div>
</div>
```

- [ ] **Step 2: Add blur handler in JavaScript**

In the script section, add after the existing event listeners:

```javascript
var emailInput = document.getElementById('emailAddress');
var emailStatus = document.getElementById('emailStatus');
var emailDot = document.getElementById('emailDot');
var emailTooltip = document.getElementById('emailTooltip');

emailInput.addEventListener('blur', function() {
    var val = emailInput.value.trim();
    if (!val) {
        emailStatus.classList.add('hidden');
        return;
    }
    fetch('/api/email/check?email=' + encodeURIComponent(val))
        .then(function(r) { return r.json(); })
        .then(function(d) {
            emailStatus.classList.remove('hidden');
            if (d.exists) {
                emailDot.className = 'w-2 h-2 rounded-full bg-yellow-400';
                emailTooltip.textContent = 'Last sent on ' + d.sent_at + ' via ' + d.template;
            } else {
                emailDot.className = 'w-2 h-2 rounded-full bg-green-500';
                emailTooltip.textContent = 'Ready to send';
            }
        })
        .catch(function() {
            emailStatus.classList.add('hidden');
        });
});

emailInput.addEventListener('input', function() {
    emailStatus.classList.add('hidden');
});
```

- [ ] **Step 3: Verify build**

```bash
go build ./...
```

Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add templates/email.html
git commit -m "feat(email): add blur-triggered email status dot indicator"
```

---

### Task 5: Update README

**Covers:** [S5]

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add Email History section to README**

Find the Email Tool section in README and add:

```markdown
### Email History

Every sent email is recorded in the database. When entering a recipient's email address, a status dot appears on blur:

- **Green dot** — New recipient, ready to send
- **Yellow dot** — Already contacted; hover to see when and via which template

#### Programmatic API

Send emails via REST API with duplicate safety:

```bash
POST /api/email/send
Content-Type: application/json

{
  "name": "John Smith",
  "company": "Google",
  "email": "john@google.com",
  "sender_key": "university",
  "template_key": "referral",
  "role": "ML Engineer",
  "safety": "safe"
}
```

**Safety flags:**
- `safe` (default) — Returns 409 if email already exists in database
- `unsafe` — Sends regardless of prior history

#### Check Email Status

```bash
GET /api/email/check?email=john@google.com
```

Returns `{exists: bool, name: "...", template: "...", sent_at: "..."}` or `{exists: false}`.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add email history and programmatic API documentation"
```
