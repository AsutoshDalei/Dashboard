---
name: Codebase Audit Report
overview: Staff+ technical audit of the Go-based Pi Zero 2 W portfolio/dashboard. Findings are organized by severity with concrete file references, recommended fixes, effort, and risk. Concludes with a tiered refactor roadmap.
todos:
  - id: quick-wins
    content: "Apply quick-wins batch: gitignore + binaries + PII purge, go mod tidy, server timeouts/graceful shutdown, constant-time passkey + rate limit + crypto/rand fail-hard, LaTeX/HTML template escaping, security headers middleware, sanitized error responses, 0600 token file perms, tectonic concurrency cap, atomic clipboard write, dead footer links"
    status: pending
  - id: medium-improvements
    content: "Medium tier: SQL-side stats aggregation + cache, read-only Postgres role for /query, session janitor, shared layout template + per-page CSS/JS extraction, tracker.html split, trigram autocomplete index, slog structured logging, mobile UX pass, single-source-of-truth templates, chart resize without re-fetch"
    status: pending
  - id: large-strategic
    content: "Strategic: split into internal/{auth,tracker,email,coverletter,backup,httpx,config} packages + first tests, adopt migration framework, swap innerHTML builders for htmx-style server-rendered partials, observability (/healthz, /metrics, OTel)"
    status: pending
isProject: false
---


# Codebase Audit — `pi_portfolio`

## Scope reviewed

- 8 Go source files in `package main` (~95 KB): [main.go](main.go), [tracker.go](tracker.go), [tracker_query.go](tracker_query.go), [db_backup.go](db_backup.go), [email.go](email.go), [oauth.go](oauth.go), [coverletter.go](coverletter.go), [clipboard.go](clipboard.go), [runtime_templates.go](runtime_templates.go)
- 6 HTML templates (~110 KB), [static/zenith.css](static/zenith.css), [static/sidebar.js](static/sidebar.js)
- Build/deploy: [build.sh](build.sh), [.gitignore](.gitignore), [go.mod](go.mod)
- Runtime artifacts (committed): [pi_bundle/clipboard_items.json](pi_bundle/clipboard_items.json), `pi_bundle/pi_portfolio_arm64` (28 MB), `pi_bundle/pi_portfolio_normal` (29 MB)

---

# Executive Summary

- **Overall codebase health: 5.5 / 10.** It works, it ships, the database/backup layer is unexpectedly thoughtful, and code is mostly readable. But it has zero tests, leaks personal data into the repo, has a hot-path god-template, and several real (not theoretical) security/operational risks.
- **Top 10 highest impact improvements**
  1. Stop tracking compiled binaries and personal PII in git ([pi_bundle/pi_portfolio_arm64](pi_bundle), [pi_bundle/pi_portfolio_normal](pi_bundle), [pi_bundle/clipboard_items.json](pi_bundle/clipboard_items.json)); fix the wrong filename in [.gitignore](.gitignore).
  2. Harden authentication: constant-time passkey compare, rate-limit/lockout on `/auth`, and a periodic session-store reaper in [main.go](main.go).
  3. Add `http.Server` timeouts and graceful shutdown — production server is Slowloris-vulnerable and never closes the DB pool cleanly.
  4. Escape LaTeX/HTML inputs feeding `tectonic` and the Gmail body templates (RCE-class risk via cover-letter; HTML injection in outgoing email) — see [coverletter.go](coverletter.go) and [runtime_templates.go](runtime_templates.go).
  5. Replace `/api/applications/query`'s regex-based read-only check with a dedicated read-only Postgres role and `statement_timeout` ([tracker_query.go](tracker_query.go)).
  6. Push aggregation into SQL: today's stats handler currently SELECTs every row and aggregates in Go ([tracker.go](tracker.go) `fetchAllApplications` / `computeStats`).
  7. Split the 68 KB god-template [templates/tracker.html](templates/tracker.html) into a shared layout + partials + extracted CSS/JS files; eliminates the duplicated sidebar/topbar across all 6 templates.
  8. Move from a single `package main` flat layout into `internal/{auth,tracker,email,backup,httpx}` packages — required to even start writing tests.
  9. Stop returning raw DB / SDK error strings to clients (`respondJSON(..., err.Error(), ...)` everywhere) — leaks schema and stack info; log internally, return a generic message + correlation id.
  10. Run `go mod tidy` — every dep in [go.mod](go.mod) is marked `// indirect`, including directly imported packages like `pgx/v5`, `godotenv`, `ngrok/v2`, `oauth2`, `gmail/v1`, `google/uuid`. This is a broken module graph.
- **Architectural concerns**: flat `package main`; 6 templates duplicating the entire chrome; god-template for tracker; tracker stats double-computed (Go + SQL); no abstraction between HTTP handler and storage.
- **Performance concerns**: `SELECT *` of all applications on every stats request; `fetchAllActivityLogs` LIMIT 200000; per-table `count(*)` in backup verify; no caching anywhere; resize re-fetches whole timeline.
- **UI/UX concerns**: ~500 lines of inline CSS per page; dead footer links (`href="#"`); marginal color contrast on muted text; tracker form has 10 fields stacked on mobile with no sectioning; no favicon; no empty/error illustrations for tracker timeline/heatmap; no toast pattern (everything is `alert()` or inline status).
- **Security concerns**: timing-attackable passkey compare, no CSRF token (mitigated only by `SameSite=Strict`), no rate limit, no security headers, world-readable token.json (0644), regex-only SQL allow-list, LaTeX injection via cover letter, OpenRouter API key in env exposed in stack traces, no request size limits.

---

# Detailed Findings

## CRITICAL

### C1. Compiled binaries and personal data committed to git
- **Area**: DevEx / Security / Repo hygiene
- **Files**: `pi_bundle/pi_portfolio_arm64` (28 MB), `pi_bundle/pi_portfolio_normal` (29 MB), [pi_bundle/clipboard_items.json](pi_bundle/clipboard_items.json), [.gitignore](.gitignore)
- **Problem**: Two ~30 MB binaries are tracked. `clipboard_items.json` contains personal URLs, phone-number adjacent text, and full job-experience descriptions. The `.gitignore` lists `pi_bundle/clipboard.json` — wrong filename, so the actual file (`clipboard_items.json`, set in [clipboard.go](clipboard.go:29)) is not ignored.
- **Why it matters**: Repo grows by 60 MB per build. PII is now in git history forever (`git log -- pi_bundle/clipboard_items.json`). Anyone with read access (or if the repo is ever made public) gets your contact data, your résumé bullet points, and your past employer notes.
- **Fix**: 
  1. Update [.gitignore](.gitignore) — change `pi_bundle/clipboard.json` to `pi_bundle/clipboard_items.json`, add `pi_bundle/pi_portfolio_*`, `pi_bundle/*.zip`, `pi_bundle/*.tar.gz`, `pi_bundle/.DS_Store`, `.DS_Store`, `db_dumps/`, `Export DB/.ipynb_checkpoints/`.
  2. `git rm --cached pi_bundle/pi_portfolio_arm64 pi_bundle/pi_portfolio_normal pi_bundle/clipboard_items.json`.
  3. If the repo is or will be public, rewrite history with `git filter-repo` to purge PII and binaries.
- **Effort**: S (15 min) for new commits; M for history rewrite.
- **Risk**: Low for ignore changes; Medium for history rewrite (forces a re-clone for everyone).

### C2. Authentication is timing-attackable and unthrottled
- **Area**: Security
- **Files**: [main.go](main.go) — `handleAuth` lines 310-340
- **Problem**:
  - `passkey != expectedPasskey` uses Go's `!=` on strings, which is not constant-time. Statistically, an attacker on the LAN can shave off a few bytes of leakage per million attempts.
  - There is no rate limit, no IP lockout, no exponential backoff. Anyone who reaches `/auth` can hammer it indefinitely.
  - Failed-login responses always re-render `login.html` with HTTP 200, so failures are observationally identical to a slow success only at the cookie level.
  - The fallback in `generateToken` (`fmt.Sprintf("%d", time.Now().UnixNano())`) is **deterministic and predictable** — if `crypto/rand` ever returns an error, an attacker who can predict process start time can forge a session token.
- **Fix**:
  ```go
  // crypto/subtle for constant-time
  if subtle.ConstantTimeCompare([]byte(passkey), []byte(expected)) != 1 {
      renderLoginError(...)
      return
  }
  ```
  Add a tiny in-memory leaky-bucket keyed by `r.RemoteAddr` (or a real one like `github.com/sethvargo/go-limiter`) capping `/auth` to ~5/min per IP. On crypto/rand failure, `log.Fatal` rather than fall back to a predictable token.
- **Effort**: S (~1 hour total).
- **Risk**: Low.

### C3. LaTeX/Tex injection through the cover-letter generator
- **Area**: Security (RCE-adjacent)
- **Files**: [coverletter.go](coverletter.go), [runtime_templates.go](runtime_templates.go), [templates/coverletter.tex.tmpl](templates/coverletter.tex.tmpl)
- **Problem**: `companyName` is rendered into a LaTeX template via `text/template` and then compiled with `tectonic`. LaTeX is Turing-complete. A `Company` value containing `\input{/etc/passwd}` or `\write18{...}` (tectonic disables `\write18` by default — but `\input` and `\verbatiminput` still leak files; on older tectonic builds shell-escape can be enabled). Even a malformed value with `}` will break compilation.
- **Exploit scenario**: Authenticated user submits `\input{/home/pi/.env}` as the company name. `tectonic` reads the file and embeds it into the PDF that's returned. Or `\catcode\`@=11 \input{...}` to break out of any sanitization.
- **Fix**: Escape LaTeX special characters before template substitution. Specifically `\ { } $ & # ^ _ ~ %`. Also pass `--keep-intermediates=false` and reject input longer than e.g. 200 chars.
  ```go
  var latexEscaper = strings.NewReplacer(
    `\`, `\textbackslash{}`,
    `{`, `\{`, `}`, `\}`,
    `$`, `\$`, `&`, `\&`, `#`, `\#`,
    `^`, `\^{}`, `_`, `\_`, `~`, `\~{}`, `%`, `\%`,
  )
  ```
- **Effort**: S.
- **Risk**: Low.

---

## HIGH IMPACT

### H1. `respondJSON` leaks internal errors to clients
- **Area**: Security / DevEx
- **Files**: throughout [tracker.go](tracker.go), [tracker_query.go](tracker_query.go), [main.go](main.go), [clipboard.go](clipboard.go)
- **Problem**: Every error path does `respondJSON(w, ..., err.Error(), "")`. The client receives raw pgx error strings (with column names, schema, position info), oauth2 error wrapper text, file paths from `os.ReadFile`, etc.
- **Fix**: Introduce one `httpError(w, r, status, err, "user-facing message")` helper that logs `err` with a request id and returns `{"success": false, "error": "user-facing message", "request_id": "..."}`. Never inline `err.Error()`.
- **Effort**: M.
- **Risk**: Low.

### H2. SQL allow-list relies on regex, not a real parser; no DB-level enforcement
- **Area**: Security
- **Files**: [tracker_query.go](tracker_query.go) — `validateReadOnlySQL`, `writeKeywordRe`
- **Problem**:
  - `writeKeywordRe` matches keywords anywhere in the body with `\b...\b`. This false-positives (a string literal `'this is an update'` is rejected — actually fine) but more importantly it does **not** prevent function-based DoS: `SELECT pg_sleep(60)` is allowed, as is `SELECT * FROM applications, generate_series(1, 1e9)` (cartesian explosion) and on un-managed Postgres `SELECT pg_read_file('postgresql.conf')`.
  - 15-second `statement_timeout` is only on the Go side via `context.WithTimeout` — pgx will cancel the query at the client, but the server may continue executing briefly.
  - Even with the LLM, prompt-injection in the natural-language input can coerce it to emit weird SQL.
- **Fix** (defense in depth):
  1. Create a dedicated Postgres role: `CREATE ROLE dashboard_reader LOGIN; GRANT CONNECT ON DATABASE ...; GRANT USAGE ON SCHEMA public; GRANT SELECT ON applications TO dashboard_reader; ALTER ROLE dashboard_reader SET statement_timeout = '5s'; ALTER ROLE dashboard_reader SET default_transaction_read_only = on;`. Open a second `pgxpool` with this role's `DATABASE_URL` and route `/api/applications/query` through it.
  2. Keep the regex check as a UX-quality gate.
  3. Wrap every NL/raw query in `BEGIN; SET LOCAL statement_timeout = ...; ...; ROLLBACK;` for safety.
- **Effort**: M.
- **Risk**: Low; requires Supabase role creation.

### H3. No HTTP server timeouts; no graceful shutdown
- **Area**: Performance / Reliability
- **Files**: [main.go](main.go) lines 244, 254
- **Problem**: Both `http.Serve(ln, nil)` and `http.ListenAndServe(":"+port, nil)` use the zero-value `http.Server` — no `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, no `MaxHeaderBytes`. A single Slowloris client holds a goroutine forever. Equally, signals (`SIGTERM`) are not handled: the deferred `dbPool.Close()` in `main` never executes, and the backup goroutine never sees `ctx.Done()` because `context.Background()` is passed and never cancelled.
- **Fix**:
  ```go
  srv := &http.Server{
      Addr: ":" + port,
      ReadHeaderTimeout: 10 * time.Second,
      ReadTimeout:       30 * time.Second,
      WriteTimeout:      60 * time.Second,
      IdleTimeout:       120 * time.Second,
      MaxHeaderBytes:    1 << 16,
  }
  ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
  defer stop()
  go func() { <-ctx.Done(); srv.Shutdown(context.Background()) }()
  ```
  Pass `ctx` (not `context.Background()`) into `startDatabaseBackups`.
- **Effort**: S.
- **Risk**: Low.

### H4. Session store has no janitor → unbounded memory growth
- **Area**: Performance / Reliability (matters on 512 MB Pi)
- **Files**: [main.go](main.go) `SessionStore`
- **Problem**: `sessions map[string]time.Time` only deletes a key when `Validate` is called for that token. Cookies that the browser never revisits (closed tabs, rotated cookies after re-auth) live forever. Over a year of single-user usage that may only be tens of KB; in any multi-user scenario, or against a malicious script that creates real (post-auth) sessions, it's unbounded.
- **Fix**: Either replace with a fixed-size LRU (e.g. `hashicorp/golang-lru`), or add a background goroutine that walks the map every `sessionDuration/24` and drops expired entries. Keep the `sync.RWMutex`.
- **Effort**: S.
- **Risk**: Low.

### H5. Tracker stats handler scans the whole `applications` table in Go
- **Area**: Performance
- **Files**: [tracker.go](tracker.go) — `handleApplicationsStats`, `fetchAllApplications`, `computeStats`
- **Problem**:
  - `SELECT id, organization, ... FROM applications LIMIT 100000` runs on every page load (and again after every form submit). Then status/category breakdown, last_30_days, top company etc. are computed in Go. Today's data is then **recomputed in SQL** and overwrites the Go-computed values — the local computation for today/week is pure waste.
  - `fetchAllActivityLogs` (used by `/timeline`, `/contribution`) also `SELECT`s up to 200 000 rows on every request, with no LIMIT/OFFSET pagination and no caching.
- **Fix**: Push aggregation into Postgres. A single CTE returns all the panel values in one round-trip and avoids transferring per-row JSON over the wire:
  ```sql
  WITH base AS (SELECT * FROM applications),
  agg AS (
    SELECT
      count(*) AS companies,
      coalesce(sum(count),0) AS applications,
      count(*) FILTER (WHERE lower(status)='applied') AS applied,
      ...
    FROM base
  ),
  today AS (SELECT ... FROM application_activity_logs WHERE activity_date = current_date),
  week AS (...)
  SELECT * FROM agg, today, week;
  ```
  Add a cheap in-memory cache (`sync.Map` keyed by handler + 30 s TTL) so multiple browser tabs aren't a 10× multiplier.
- **Effort**: M.
- **Risk**: Low — cover with golden-row tests for the aggregations.

### H6. God-template: [templates/tracker.html](templates/tracker.html) (1485 lines, 68 KB)
- **Area**: Maintainability / Performance / UI/UX
- **Files**: [templates/tracker.html](templates/tracker.html)
- **Problem**: One file contains the sidebar, topbar, stats grid, lookup form, upsert form, timeline chart (SVG renderer), GitHub-style contribution heatmap, and natural-language SQL UI — plus ~500 lines of inline CSS and ~700 lines of inline JS. Nothing is reusable; the chart code can't be tested; the CSS doesn't get cached across pages; adding a new tool requires touching every template's sidebar copy. The XSS surface is large (`innerHTML = ...` is used in 7+ places).
- **Fix**:
  1. Extract a base layout (`templates/layout.html`) with `{{ define "layout" }}...{{ block "content" . }}{{ end }}...{{ end }}` and convert each page to `{{ template "layout" . }} {{ define "content" }}...{{ end }}`. Same for the sidebar partial.
  2. Move tracker-specific CSS into `static/tracker.css`. Move JS into `static/tracker.js` (or split into `static/tracker.stats.js`, `static/tracker.chart.js`, `static/tracker.contribution.js`, `static/tracker.query.js`).
  3. Browsers will then cache CSS/JS across navigations on a Pi.
  4. Replace string-concat-`innerHTML` with `document.createElement` for user-controlled fields, or systematically reuse `escapeHtml` (some places use it, some don't, e.g. line 1131 `data-date="${escapeHtml(d.date)}"` ✅ but other server-supplied fields trust pgx output).
- **Effort**: M (1 week).
- **Risk**: Low if test the visual output side-by-side.

### H7. `package main` monolith — testing is structurally blocked
- **Area**: Architecture / Testing
- **Files**: every `.go` in repo root
- **Problem**: There are no `_test.go` files anywhere, and the design makes them painful to add: every handler reads `os.Getenv` directly, queries the package-level `dbPool`, parses files from disk, and writes to the global `sessionStore`. Unit-testing `handleApplicationsUpsert` requires standing up a real Postgres.
- **Fix**: Refactor to:
  ```
  internal/
    auth/        session store + middleware
    config/      env loading & validation, returns typed struct
    tracker/     handlers, queries, stats; takes *pgxpool.Pool via DI
    email/       SendEmail with a Sender interface (Gmail API impl + SMTP impl + fake)
    coverletter/ tectonic runner with a Compiler interface
    backup/      Backuper struct with *pgxpool.Pool
    httpx/       respondJSON, error wrapping, request-id middleware
  cmd/server/main.go   wires everything
  ```
  Then a 100-line `handlers_test.go` per package using `httptest.Server` + `pgxmock` covers 80% of paths.
- **Effort**: L (~2 weeks).
- **Risk**: Medium (lots of moving parts, but no behavior change if guarded by manual smoke tests).

### H8. Chrome (sidebar + topbar + head) is duplicated across all 6 templates
- **Area**: Maintainability / DevEx
- **Files**: [templates/dashboard.html](templates/dashboard.html), [templates/email.html](templates/email.html), [templates/coverletter.html](templates/coverletter.html), [templates/clipboard.html](templates/clipboard.html), [templates/tracker.html](templates/tracker.html), [templates/login.html](templates/login.html)
- **Problem**: Each page repeats the entire sidebar `<aside>` (~25 lines) and topbar `<header>` (~15 lines). Adding a new nav item requires editing 5 files. Marking a page "active" is done by hand-toggling `class="nav-link active"` per file — easy to forget.
- **Fix**: Use Go's `html/template` block/define mechanism (cite C2 for migration plan). Active state can be passed via template data:
  ```html
  {{ define "navlink" }}<a href="{{.Href}}" class="nav-link{{if eq $.Active .Key}} active{{end}}">{{.Label}}</a>{{ end }}
  ```
- **Effort**: M.
- **Risk**: Low.

### H9. Inconsistent template engine choice creates an HTML-injection vector via email
- **Area**: Security
- **Files**: [runtime_templates.go](runtime_templates.go), [email.go](email.go), [templates/email_body.tmpl](templates/email_body.tmpl)
- **Problem**: `renderRuntimeTemplate` uses `text/template` (not `html/template`). The email body template is HTML and gets the user-supplied `Name`/`Company` interpolated **unescaped**. A recipient name like `<img src=x onerror=alert(1)>` is shipped as live HTML in the email. Most webmail strips JS but rich-formatting attacks and convincing link injection still work. Same engine is used for the LaTeX template — see C3 above.
- **Fix**: Use `html/template` for `email_body.tmpl` (and the cover-letter template should keep `text/template` but with the latex-escape replacer from C3). Easiest: split into `renderHTMLRuntime` and `renderTextRuntime`.
- **Effort**: S.
- **Risk**: Low.

### H10. `go.mod` is in a broken / dirty state
- **Area**: DevEx / Dependency hygiene
- **Files**: [go.mod](go.mod)
- **Problem**: Every dependency is marked `// indirect`, including `pgx/v5`, `godotenv`, `ngrok/v2`, `oauth2`, `gmail/v1`, `google/uuid` which are directly imported. This means a fresh `go build` works but `go mod tidy` will rewrite the file — a sign the module graph was last touched in a different layout or with a different tool. Reviewers cannot tell what is actually a direct dependency. The pinned `google.golang.org/genproto/googleapis/rpc v0.0.0-20260401024825-...` is a pre-released pseudo-version against a future date that won't exist in proxies for long.
- **Fix**: `go mod tidy && go mod verify`, commit the result. Pin only what you intentionally pin.
- **Effort**: S.
- **Risk**: Low (verify a clean build after).

---

## MEDIUM IMPACT

### M1. Stats computation does both Go-side and SQL-side aggregation, then overwrites Go results
- [tracker.go](tracker.go) `handleApplicationsStats` builds `stats` in Go, then `fetchTodayWeekActivityTotals` overwrites today/week. Pick one source of truth. Fold into H5.

### M2. `loadActivityLogsForHandlers` called on every timeline / contribution request — no caching
- Same data scanned over and over. A `sync.Map`-backed cache with a 1 minute TTL keyed by the date range would eliminate ~95% of DB roundtrips. **Effort**: S.

### M3. Backfill DDL runs on every server startup
- [tracker.go](tracker.go) `ensureActivityLogBootstrap` is gated by an in-process boolean, but on every cold start it runs the table-creation DDL plus a giant `INSERT ... SELECT ... NOT EXISTS` correlated subquery. After the first run it's a no-op but the planning overhead is real. Move to an idempotent migration in [Export DB/insert_missing_rows.sql](Export DB/insert_missing_rows.sql)-style files and a single bootstrap migration run. **Effort**: M. **Risk**: Low.

### M4. Embedded vs runtime templates create source-of-truth confusion
- [main.go](main.go) embeds `templates/*.html` via `//go:embed`, but [runtime_templates.go](runtime_templates.go) reads `templates/email_body.tmpl` and `templates/coverletter.tex.tmpl` **from disk**, walking the filesystem with `..` fallback. The HTML templates are embedded; the data templates are not. The `build.sh` script `cp`s them into `pi_bundle/templates/` separately. The two copies can drift silently — they will eventually contain different placeholder names.
- **Fix**: Embed both, then conditionally allow filesystem override via env var only if you actually want runtime tweaks without rebuild. **Effort**: S.

### M5. Per-keystroke `/api/applications/check` and `/api/applications/suggest` — no autocomplete index
- [tracker.html](templates/tracker.html) lines 864-877: lookup runs on every keystroke (debounced 220 ms / 300 ms). On a few hundred rows it's instantaneous. At ~10k rows the `LIKE '%foo%'` table-scan in `handleApplicationsSuggest`'s CTE becomes the slowest part of the app. Add a functional index `CREATE INDEX applications_org_trgm ON applications USING gin (lower(organization) gin_trgm_ops);` and switch to `%>` similarity ranking. **Effort**: S. **Risk**: Low.

### M6. Backup verifier does `count(*)` per table sequentially
- [db_backup.go](db_backup.go) `liveRowCounts` opens one query per table. For a handful of tables this is fine, but it scales linearly. Switch to one query: `SELECT n.nspname || '.' || c.relname, c.reltuples::bigint FROM pg_class c JOIN pg_namespace n ON c.relnamespace = n.oid WHERE c.relkind = 'r' AND ...` (or, for accuracy, a single `UNION ALL` of `count(*)` queries built dynamically). **Effort**: S.

### M7. Frontend resize handler re-fetches `/api/applications/timeline`
- [tracker.html](templates/tracker.html) line 1413: `loadTimeline(currentFreq)` on every breakpoint cross. The data hasn't changed — only the layout. Re-render from `latestTimelineBuckets` instead. **Effort**: S.

### M8. `clipboard_items.json` write-then-replace race
- [clipboard.go](clipboard.go) `saveClipboardItems` does `os.WriteFile`. On crash mid-write the file is corrupted. Use `os.WriteFile` to `clipboard_items.json.tmp` then `os.Rename` (same pattern as the backup module). **Effort**: S.

### M9. Token file rewritten to 0644
- [oauth.go](oauth.go) line 85: `os.WriteFile(tokenPath, updatedData, 0644)`. OAuth refresh tokens are credentials; on a multi-user Pi (or anywhere with shared filesystems) this is readable by other users. Should be `0600`, and credentials.json should also be enforced to `0600` on first read. **Effort**: S.

### M10. Resume path resolution is fragile
- [email.go](email.go) lines 84-89 try `../<filename>` first, then `<filename>`. Cover-letter and oauth do the same dance. Centralize "look up file by env-var or default in current/parent dir" or just demand an absolute path via env. **Effort**: S.

### M11. Tectonic compilation has no concurrency cap
- [coverletter.go](coverletter.go): every POST spawns a `tectonic` subprocess that may download packages from CTAN on first run. Two concurrent users on Pi Zero 2 W can OOM the device. Add a `chan struct{}` semaphore of size 1 (Pi) or 2. **Effort**: S. **Risk**: Low.

### M12. `pgxpool.New` uses defaults, not tuned for Pi or Supabase
- [main.go](main.go) line 161: default `max_conns = max(4, NumCPU)`. Supabase free tier limits direct connections. Read pool size from env (`DB_MAX_CONNS`, default 4) and set `MinConns: 1, MaxConnIdleTime: 5m, MaxConnLifetime: 1h`. **Effort**: S.

### M13. Mobile UX of tracker is rough
- The upsert form has 10 fields, no section headings, and just stacks vertically on phones. The contribution heatmap cells shrink to 16×16 px at the smallest breakpoint and become hard to tap accurately. The timeline chart resizes by triggering a full data re-fetch (M7). **Fix**: collapse the upsert form into accordion sections (Required / Optional details / Misc); make heatmap cells at least 28×28 px and add `touch-action: manipulation`. **Effort**: M.

### M14. Accessibility gaps
- Footer links in [templates/login.html](templates/login.html) point to `#` (dead links — confusing for screen-reader users who hear "Privacy, link"). Several buttons use emoji as their visible label (copy 📋, delete 🗑); they have `title` and `aria-label`, but the emoji itself isn't marked `aria-hidden="true"`, so AT users hear it announced. Color is the only signaling on `.status-message.success/.error`. `--on-surface-muted` (#71717a) on `--surface-lowest` (#09090b) gives a 4.06:1 contrast — below the WCAG AA target of 4.5:1 for body text. **Effort**: M.

### M15. No security headers
- No CSP, no `X-Content-Type-Options: nosniff`, no `Referrer-Policy: strict-origin-when-cross-origin`, no `X-Frame-Options: DENY`. Trivial to add as middleware around the handlers. **Effort**: S.

### M16. No favicon → every page request triggers a 404 for `/favicon.ico`
- Add a small embed in [main.go](main.go) and a route. **Effort**: S.

### M17. Logging strategy is `log.Printf` only
- No log levels, no structured fields, no request id. Compare `log.Printf("tracker stats error: %v", err)` (no path, no auth state, no IP) against using `log/slog` (stdlib since Go 1.21): `slog.Error("tracker stats", "err", err, "request_id", id, "path", r.URL.Path)`. Production debugging on a Pi via SSH is much easier with structured logs and a `journalctl | jq` workflow. **Effort**: S–M.

### M18. Per-page inline JS duplicates `escapeHtml`, `parseApiResponse`, `fetchJson`
- Promoting `static/utils.js` with `<script src="/static/utils.js" defer></script>` saves bytes and reduces drift. **Effort**: S.

### M19. `/send-email` does not validate the email address
- [main.go](main.go) `handleSendEmail`: `req.Email == ""` is the only check. The Gmail API will reject obviously malformed addresses, but client-side `type="email"` is too lenient. Use `net/mail.ParseAddress`. **Effort**: S.

### M20. `tracker.go` `LIMIT 100000` and `LIMIT 200000` are silent data clips
- Either paginate or stream. Better still, move aggregation server-side (H5). **Effort**: M.

---

## LOW IMPACT

### L1. `defer dbPool.Close()` in `main` never runs because the process exits via `log.Fatal*` paths or the http server is blocking. Folded into H3.

### L2. `nullIfEmpty` returns `interface{}`. Switch to `pgtype.Text` for type clarity.

### L3. `clipboardStore.Items = append([]ClipboardItem{item}, clipboardStore.Items...)` is O(n) per insert ([clipboard.go](clipboard.go) line 151). Trivial at current scale.

### L4. `parseAppliedDate` tries 7 layouts. The actual column is `DATE`. Trim to one or two and explicitly error otherwise.

### L5. `validateReadOnlySQL` re-runs `Index(";")` checks twice. Consolidate.

### L6. Comments referencing macOS-specific paths in [tracker.go](tracker.go) line 30 ("Raspberry Pi OS often uses UTC while macOS uses local time") leak the developer's environment in the binary.

### L7. `ngrok` flow `log.Fatalf` on errors — should be retryable or at least logged with a hint to swap to direct HTTP.

### L8. `embed.FS` for templates means template edits require recompile. The runtime override pattern (M4) is half-implemented. Pick one.

### L9. `Asutosh's Portfolio` is hardcoded in 6 places. Pull into `SiteConfig` and inject via template data.

### L10. `footer` reads `&copy; 2024` in [login.html](templates/login.html) line 49. Will keep saying 2024 in 2031. Use server-side year injection.

### L11. The `Export DB/` folder contains a Jupyter notebook with .ipynb_checkpoints and 100 KB of CSVs that shouldn't live in this repo at all. Move to a separate `data-migrations/` repo or at least `git rm -r` after the migration is done.

### L12. `runtime_templates.go` walks paths with `..` fallback that doesn't bound the directory. Low impact only because all callers are internal, but it's a smell that would matter if a user-supplied path ever flowed in.

### L13. `oauth.go` time-zone comment about Python's google-auth refers to a Python-ism that doesn't apply now — clean up.

---

## NICE-TO-HAVE

### N1. PWA / service-worker for offline use
- The README claims "Offline-capable UI." A trivial service worker that caches `/static/*` and the dashboard would deliver on that claim. ~50 lines of JS.

### N2. Replace inline `<script>` blocks with `<script type="module" defer src="...">` so they can be unit-tested with Vitest + jsdom.

### N3. Add a `/healthz` endpoint that returns 200 if `dbPool.Ping` succeeds. Useful for `systemd`'s `WatchdogSec=`.

### N4. Add OpenAPI / OpenAPI-lite for the API endpoints — the README's hand-maintained table is already drifting (e.g. `/api/applications/contribution` is in [main.go](main.go) line 199 but not in the docs table at line 314).

### N5. Use `templ` or `gomponents` for typed templates → compile-time check that you're not passing the wrong struct to a template.

### N6. Add a `make` target (or a `mage`/`task` file) to replace [build.sh](build.sh) for cross-platform devs.

### N7. Migrate from a global `dbPool` to context-scoped pool injection — pairs with H7.

### N8. Add Prometheus metrics endpoint behind auth: request counts, p95 latency per route, backup duration.

---

# Refactor Roadmap

## Quick wins (1–3 days, S effort, low risk)

1. **Repo hygiene** (C1): fix `.gitignore`, `git rm --cached` the binaries and PII, add `.DS_Store` and `db_dumps/` patterns.
2. **`go mod tidy`** (H10).
3. **Server timeouts + graceful shutdown + signal handling** (H3).
4. **Constant-time passkey compare; remove deterministic token fallback; rate-limit `/auth`** (C2).
5. **LaTeX escaper + HTML template engine for email** (C3 + H9).
6. **Security headers middleware** (M15).
7. **Sanitize error responses** — single helper, no raw `err.Error()` on the wire (H1).
8. **0600 perms on token files** (M9).
9. **Concurrency cap on tectonic** (M11).
10. **Atomic clipboard write** (M8).
11. **Dead footer links** (L10).

## Medium improvements (1–3 weeks, M effort)

1. **SQL-side aggregation for `/api/applications/stats`** + 30 s server-side cache (H5, M1, M2).
2. **Read-only Postgres role for `/api/applications/query`** + `statement_timeout` + transactional read-only wrapper (H2).
3. **Session-store janitor** (H4).
4. **Extract sidebar/topbar to a shared layout template; per-page CSS/JS files** (H8, M18).
5. **Split [tracker.html](templates/tracker.html) into partials and external `tracker.*.js`** (H6).
6. **Trigram index + autocomplete query rewrite** (M5).
7. **Structured logging with `log/slog` and request ids** (M17).
8. **Mobile UX pass on the tracker form and heatmap** (M13).
9. **Single source of truth for templates (embed only, optional override clearly documented)** (M4).
10. **Frontend chart re-renders on resize without re-fetch** (M7).

## Large strategic improvements (1–3 months, L effort)

1. **Repackage into `internal/{auth,tracker,email,coverletter,backup,httpx,config}` and add the first 2 layers of tests** (H7). This unblocks everything else: every quick-win becomes far less risky once you can run `go test ./...` and see the green light.
2. **Migration framework** (`golang-migrate`, `goose`, or a small homegrown one) — replace the in-handler `CREATE TABLE IF NOT EXISTS` and backfill logic (M3, L4).
3. **Replace ad-hoc HTML/JS in tracker with islands of vanilla JS components or htmx** — htmx fits this codebase's style (server-rendered Go templates + small JS) better than a full SPA, and lets you delete most of the `innerHTML` builders. The chart is the only "real" client work.
4. **Observability: `/healthz`, `/metrics`, structured access log, optional OpenTelemetry traces over `otelhttp`** (N3, N8). The OTel deps are already pulled in transitively — wiring them is incremental.

### Prioritization rationale

- **Quick wins** target the highest-severity items (PII leak, auth weakness, RCE-class LaTeX injection) and require zero structural change.
- **Medium tier** maximizes ROI per hour: stats SQL aggregation alone reduces tracker page load from ~all-rows-over-the-wire to a few rows; the layout extraction pays back on every future template change.
- **Large tier** is what unlocks long-term sustainability — tests, migrations, and observability. Worth doing only if this project is meant to outlive a personal portfolio.

---

# Final Assessment

## Biggest long-term risks

1. **The repo is leaking PII and binaries into permanent history.** Every commit forward makes the cleanup harder.
2. **Zero tests + tight coupling = every refactor is a manual smoke test.** This is the largest single drag on velocity.
3. **The tracker template (and to a lesser extent every template) is on an unbounded complexity trajectory.** It's 68 KB today; if the analytics ambitions in the README grow, it will be unfixable.
4. **Security posture assumes a single trusted user behind a passkey.** The moment this is exposed to the internet via ngrok without rate-limiting, the auth surface and the SQL-query endpoint become real attack vectors.

## Technical debt summary

| Area | Debt level | Notes |
|---|---|---|
| Architecture | High | Flat `package main`; god template; global singletons. |
| Testing | Critical | None. Refactoring is uninsured. |
| Security | Medium-High | Several real but addressable issues; nothing exotic. |
| Performance | Medium | Works fine at current scale; obvious cliffs at 10× data. |
| Observability | High | `log.Printf` only; no metrics, no traces, no health. |
| Documentation | Low | README is unusually good for a personal project. |
| Dependency hygiene | Medium | Dirty `go.mod`; future-dated pseudo-version. |

## Scalability readiness

- **Vertical (more data on the Pi)**: hits limits around 10k applications because every page load scans the whole table. Fix with H5.
- **Horizontal (more users)**: not designed for it. Single passkey, single in-memory session map, single JSON file for clipboard. Acceptable for the stated personal-use scope.

## Maintainability readiness

- Reasonable as a one-person codebase. Becomes painful the moment anyone else needs to add a feature, because: no tests, no abstraction, and every template is its own copy of the chrome.

## Production readiness

- It runs, it backs up its data, it has a sane deployment story. It is **not** production-ready in the "I'd put this on the open internet" sense — the quick-wins list addresses that gap in a couple of days of focused work.

## Suggested next engineering investments (in order)

1. The **Quick wins** list above, sequentially — biggest security/ops payoff per hour.
2. **The package split (H7)** — every later change depends on this.
3. **Migration framework + structured logging** — together they make the system observable and reproducible.
4. **The tracker UI surgery (H6)** — pays back every UI change after.
