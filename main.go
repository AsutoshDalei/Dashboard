package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"golang.ngrok.com/ngrok/v2"
)

func logDatabaseConnectionTarget(rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		slog.Info("Database: could not parse DATABASE_URL host (using your connection as configured)")
		return
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if i := strings.Index(dbName, "?"); i >= 0 {
		dbName = dbName[:i]
	}
	if dbName == "" {
		dbName = "(default)"
	}
	slog.Info("Database", "host", u.Host, "db", dbName)
}

//go:embed templates/*
var templateFiles embed.FS

//go:embed static/*
var staticFiles embed.FS

var templates *template.Template
var dbPool *pgxpool.Pool
var dbPoolReader *pgxpool.Pool

type EmailRequest struct {
	Name      string `json:"name"`
	Company   string `json:"company"`
	Email     string `json:"email"`
	SenderKey string `json:"sender_key"`
}

type LoginData struct {
	Error string
	Year  int
}

// Session management
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]time.Time
}

var sessionStore = &SessionStore{
	sessions: make(map[string]time.Time),
}

const sessionCookieName = "session_token"
const sessionDuration = 24 * time.Hour

func (s *SessionStore) Create() string {
	token := generateToken()
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(sessionDuration)
	s.mu.Unlock()
	return token
}

func (s *SessionStore) Validate(token string) bool {
	s.mu.RLock()
	expiry, exists := s.sessions[token]
	s.mu.RUnlock()

	if !exists {
		return false
	}

	if time.Now().After(expiry) {
		s.Delete(token)
		return false
	}

	return true
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
	deleteChatSession(token)
}

func (s *SessionStore) pruneExpired() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for tok, exp := range s.sessions {
		if now.After(exp) {
			delete(s.sessions, tok)
		}
	}
	valid := make(map[string]bool, len(s.sessions))
	for tok := range s.sessions {
		valid[tok] = true
	}
	pruneChatSessions(valid)
}

func (s *SessionStore) runJanitor(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.pruneExpired()
		}
	}
}

func generateToken() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		slog.Error("crypto/rand failed for session token", "err", err)
		os.Exit(1)
	}
	return hex.EncodeToString(bytes)
}

func constantTimePasskeyEqual(a, b string) bool {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	if n == 0 {
		return len(a) == 0 && len(b) == 0
	}
	bufA := make([]byte, n)
	bufB := make([]byte, n)
	copy(bufA, a)
	copy(bufB, b)
	return subtle.ConstantTimeCompare(bufA, bufB) == 1
}

func parseEnvInt(key string, def int) int {
	s := strings.TrimSpace(os.Getenv(key))
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return def
	}
	return n
}

func openPrimaryPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = int32(parseEnvInt("DB_MAX_CONNS", 4))
	cfg.MinConns = 1
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.MaxConnLifetime = time.Hour
	return pgxpool.NewWithConfig(ctx, cfg)
}

func openReaderPool(ctx context.Context) (*pgxpool.Pool, error) {
	u := strings.TrimSpace(os.Getenv("DATABASE_URL_READER"))
	if u == "" {
		return nil, nil
	}
	cfg, err := pgxpool.ParseConfig(u)
	if err != nil {
		return nil, err
	}
	maxR := int32(parseEnvInt("DB_READER_MAX_CONNS", 4))
	if maxR > 8 {
		maxR = 8
	}
	cfg.MaxConns = maxR
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.MaxConnLifetime = time.Hour
	return pgxpool.NewWithConfig(ctx, cfg)
}

func firstNonLoopbackIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if v4 := ipNet.IP.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ""
}

func init() {
	var err error
	templates, err = template.ParseFS(templateFiles, "templates/*.html")
	if err != nil {
		slog.Error("Error parsing templates", "err", err)
		os.Exit(1)
	}
	initResumeTailor()
}

func registerRoutes(mux *http.ServeMux) {
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		slog.Error("Error setting up static filesystem", "err", err)
		os.Exit(1)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/login", handleLogin)
	mux.HandleFunc("/auth", handleAuth)
	mux.HandleFunc("/logout", handleLogout)
	mux.HandleFunc("/healthz", handleHealthz)

	mux.HandleFunc("/dashboard", requireAuth(handleDashboard))
	mux.HandleFunc("/tools/email", requireAuth(handleEmailTool))
	mux.HandleFunc("/tools/cover-letter", requireAuth(handleCoverLetterTool))
	mux.HandleFunc("/generate-cover-letter", requireAuth(handleGenerateCoverLetter))
	mux.HandleFunc("/tools/clipboard", requireAuth(handleClipboardTool))
	mux.HandleFunc("/api/clipboard", requireAuth(handleClipboardAPI))
	mux.HandleFunc("/api/clipboard/", requireAuth(handleClipboardItemAPI))
	mux.HandleFunc("/tools/tracker", requireAuth(handleTrackerTool))
	mux.HandleFunc("/api/applications", requireAuth(handleApplicationsUpsert))
	mux.HandleFunc("/api/applications/check", requireAuth(handleApplicationsCheck))
	mux.HandleFunc("/api/applications/suggest", requireAuth(handleApplicationsSuggest))
	mux.HandleFunc("/api/applications/stats", requireAuth(handleApplicationsStats))
	mux.HandleFunc("/api/applications/timeline", requireAuth(handleApplicationsTimeline))
	mux.HandleFunc("/api/applications/contribution", requireAuth(handleApplicationsContribution))
	mux.HandleFunc("/api/applications/query", requireAuth(handleApplicationsQuery))
	mux.HandleFunc("/send-email", requireAuth(handleSendEmail))

	mux.HandleFunc("/tools/chat", requireAuth(handleChatTool))
	mux.HandleFunc("/api/chat/send", requireAuth(handleChatSend))
	mux.HandleFunc("/api/chat/prompts", requireAuth(handleChatPrompts))
	mux.HandleFunc("/api/chat/prompts/add", requireAuth(handleChatPromptsAdd))
	mux.HandleFunc("/api/chat/prompts/delete", requireAuth(handleChatPromptsDelete))
	mux.HandleFunc("/api/chat/clear", requireAuth(handleChatClear))
	mux.HandleFunc("/api/chat/history", requireAuth(handleChatHistory))
	mux.HandleFunc("/api/chat/skill", requireAuth(handleChatSkill))

	mux.HandleFunc("/tools/resume", requireAuth(handleResumeTool))
	mux.HandleFunc("/api/resume/analyze", requireAuth(handleResumeAnalyze))
	mux.HandleFunc("/api/resume/generate", requireAuth(handleResumeGenerate))
	mux.HandleFunc("/api/resume/reanalyze", requireAuth(handleResumeReanalyze))
	mux.HandleFunc("/api/resume/chat", requireAuth(handleResumeChat))
	mux.HandleFunc("/api/resume/compile", requireAuth(handleResumeCompile))
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	log.SetFlags(0)
	log.SetOutput(slog.NewLogLogger(slog.Default().Handler(), slog.LevelInfo).Writer())

	if err := godotenv.Load(".env"); err != nil {
		if err = godotenv.Load("../.env"); err != nil {
			slog.Info("No .env file found in .env or ../.env; using system environment variables")
		}
	}

	if _, err := GetConfig("university"); err != nil {
		slog.Warn("failed to load university config", "err", err)
	}

	passkey := os.Getenv("ACCESS_PASSKEY")
	if passkey == "" {
		slog.Warn("ACCESS_PASSKEY not set. Authentication will fail for all requests.")
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	dbPool, err = openPrimaryPool(ctx, databaseURL)
	if err != nil {
		slog.Error("Failed to create database pool", "err", err)
		os.Exit(1)
	}
	if pingErr := dbPool.Ping(ctx); pingErr != nil {
		slog.Error("Failed to connect to database", "err", pingErr)
		os.Exit(1)
	}
	logDatabaseConnectionTarget(databaseURL)
	defer dbPool.Close()

	dbPoolReader, err = openReaderPool(ctx)
	if err != nil {
		slog.Error("Failed to create reader database pool", "err", err)
		os.Exit(1)
	}
	if dbPoolReader != nil {
		defer dbPoolReader.Close()
		if pingErr := dbPoolReader.Ping(ctx); pingErr != nil {
			slog.Error("Failed to ping reader database pool", "err", pingErr)
			os.Exit(1)
		}
		slog.Info("Reader pool enabled (DATABASE_URL_READER)")
	}

	startDatabaseBackups(ctx, databaseURL)
	go sessionStore.runJanitor(ctx)

	mux := http.NewServeMux()
	registerRoutes(mux)
	handler := withRequestID(withSecurityHeaders(mux))

	port := os.Getenv("PORT")
	if port == "" {
		port = "5001"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	switch strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_NGROK"))) {
	case "1", "true", "yes":
		token := strings.TrimSpace(os.Getenv("NGROK_AUTHTOKEN"))
		if token == "" {
			slog.Error("ENABLE_NGROK is set but NGROK_AUTHTOKEN is empty")
			os.Exit(1)
		}

		agent, err := ngrok.NewAgent(ngrok.WithAuthtoken(token))
		if err != nil {
			slog.Error("ngrok NewAgent", "err", err)
			os.Exit(1)
		}
		listenOpts := []ngrok.EndpointOption{ngrok.WithPoolingEnabled(true)}
		if internalURL := strings.TrimSpace(os.Getenv("NGROK_INTERNAL_ENDPOINT_URL")); internalURL != "" {
			listenOpts = append(listenOpts, ngrok.WithURL(internalURL))
		}

		ln, err := agent.Listen(ctx, listenOpts...)
		if err != nil {
			slog.Error("ngrok Listen failed", "err", err)
			os.Exit(1)
		}

		tunnelURL := ln.URL()
		envPublic := strings.TrimSpace(os.Getenv("NGROK_PUBLIC_URL"))
		isInternal := tunnelURL != nil && strings.HasSuffix(strings.ToLower(tunnelURL.Hostname()), "internal")

		slog.Info("Ready: ngrok listener active")
		if lan := firstNonLoopbackIPv4(); lan != "" {
			slog.Info("Local/LAN HTTP not used while ENABLE_NGROK=true", "lan_ip", lan, "port", port)
		} else {
			slog.Info("Local/LAN HTTP not used while ENABLE_NGROK=true", "port", port)
		}
		switch {
		case envPublic != "":
			slog.Info("Internet (website)", "url", envPublic)
		case isInternal:
			slog.Info("Internet (website)", "hint", "Cloud Endpoint URL; optional NGROK_PUBLIC_URL in .env", "tunnel", tunnelURL.String())
		default:
			slog.Info("Internet (website)", "url", tunnelURL.String())
		}

		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed", "err", err)
			os.Exit(1)
		}
	default:
		lan := firstNonLoopbackIPv4()
		slog.Info("Ready: listening for HTTP", "local", "http://127.0.0.1:"+port)
		if lan != "" {
			slog.Info("LAN", "url", fmt.Sprintf("http://%s:%s", lan, port))
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed to start", "err", err)
			os.Exit(1)
		}
	}
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := dbPool.Ping(pctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unhealthy\n"))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// Authentication middleware
func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || !sessionStore.Validate(cookie.Value) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				respondJSONAPI(w, r, http.StatusUnauthorized, false, "authentication required", "", nil)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	return sessionStore.Validate(cookie.Value)
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	if isAuthenticated(r) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if isAuthenticated(r) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := LoginData{Year: time.Now().Year()}
	if err := templates.ExecuteTemplate(w, "login.html", data); err != nil {
		slog.Error("Template rendering error", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func handleAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		renderLoginError(w, "Invalid form submission")
		return
	}

	if !allowAuthAttempt(r) {
		renderLoginError(w, "Invalid access key")
		return
	}

	passkey := r.FormValue("passkey")
	expectedPasskey := os.Getenv("ACCESS_PASSKEY")
	if strings.TrimSpace(expectedPasskey) == "" {
		renderLoginError(w, "Invalid access key")
		return
	}

	if passkey == "" || !constantTimePasskeyEqual(passkey, expectedPasskey) {
		renderLoginError(w, "Invalid access key")
		return
	}

	token := sessionStore.Create()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   int(sessionDuration.Seconds()),
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func renderLoginError(w http.ResponseWriter, errorMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "login.html", LoginData{Error: errorMsg, Year: time.Now().Year()}); err != nil {
		slog.Error("Template rendering error", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		sessionStore.Delete(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "dashboard.html", nil); err != nil {
		slog.Error("Template rendering error", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func handleEmailTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "email.html", nil); err != nil {
		slog.Error("Template rendering error", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func handleSendEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req EmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, false, "Invalid JSON payload", "")
		return
	}

	if req.Name == "" || req.Company == "" || req.Email == "" {
		respondJSON(w, http.StatusBadRequest, false, "Missing required fields", "")
		return
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		respondJSON(w, http.StatusBadRequest, false, "Invalid email address", "")
		return
	}

	if req.SenderKey == "" {
		defaultKey := os.Getenv("DEFAULT_SENDER_KEY")
		if defaultKey == "" {
			defaultKey = "university"
		}
		req.SenderKey = defaultKey
	}

	senderLabel, err := SendEmail(req.Email, req.Name, req.Company, strings.ToLower(strings.TrimSpace(req.SenderKey)))
	if err != nil {
		respondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	respondJSON(w, http.StatusOK, true, "", fmt.Sprintf("Email sent successfully via %s!", senderLabel))
}

func handleCoverLetterTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "coverletter.html", nil); err != nil {
		slog.Error("Template rendering error", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

type CoverLetterRequest struct {
	Company string `json:"company"`
}

func handleGenerateCoverLetter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CoverLetterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, false, "Invalid JSON payload", "")
		return
	}

	if req.Company == "" {
		respondJSON(w, http.StatusBadRequest, false, "Company name is required", "")
		return
	}

	companyName := strings.TrimSpace(req.Company)

	pdfData, err := GenerateCoverLetterPDF(companyName)
	if err != nil {
		respondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	safeCompanyName := strings.ReplaceAll(companyName, " ", "_")
	safeCompanyName = strings.ReplaceAll(safeCompanyName, "/", "_")
	safeCompanyName = strings.ReplaceAll(safeCompanyName, "\\", "_")

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"ASUTOSH_DALEI_COVERLETTER_%s.pdf\"", safeCompanyName))

	if _, err := w.Write(pdfData); err != nil {
		slog.Error("Error writing PDF to client", "err", err)
	}
}

func respondJSON(w http.ResponseWriter, status int, success bool, errMessage, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := map[string]any{
		"success": success,
	}
	if errMessage != "" {
		resp["error"] = errMessage
	}
	if message != "" {
		resp["message"] = message
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func handleResumeTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "resume.html", nil); err != nil {
		slog.Error("Template rendering error", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func handleResumeAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req AnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, false, "Invalid JSON payload", "")
		return
	}

	if req.JobDescription == "" {
		respondJSON(w, http.StatusBadRequest, false, "Job description is required", "")
		return
	}

	provider := req.Provider
	if provider == "" {
		provider = "ollama"
	}

	result, err := AnalyzeResume(req.JobDescription, provider, req.Model, req.OllamaHost)
	if err != nil {
		respondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	respondJSONWithData(w, http.StatusOK, true, "", "", result)
}

func respondJSONWithData(w http.ResponseWriter, status int, success bool, errMessage, message string, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]any{
		"success": success,
	}
	if errMessage != "" {
		resp["error"] = errMessage
	}
	if message != "" {
		resp["message"] = message
	}
	if data != nil {
		resp["data"] = data
	}
	json.NewEncoder(w).Encode(resp)
}

func handleResumeGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, false, "Invalid JSON payload", "")
		return
	}

	if req.JobDescription == "" {
		respondJSON(w, http.StatusBadRequest, false, "Job description is required", "")
		return
	}

	provider := req.Provider
	if provider == "" {
		provider = "ollama"
	}

	result, err := GenerateTailoredResume(
		req.JobDescription, req.Score, req.Keywords, req.Recommendations,
		req.ChatHistory, provider, req.Model, req.OllamaHost, req.CompanyName,
	)
	if err != nil {
		respondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	respondJSONWithData(w, http.StatusOK, true, "", "", result)
}

func handleResumeReanalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		LatexSource    string `json:"latex_source"`
		JobDescription string `json:"job_description"`
		Provider       string `json:"provider"`
		Model          string `json:"model"`
		OllamaHost     string `json:"ollama_host"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, false, "Invalid JSON payload", "")
		return
	}

	if req.LatexSource == "" || req.JobDescription == "" {
		respondJSON(w, http.StatusBadRequest, false, "latex_source and job_description are required", "")
		return
	}

	provider := req.Provider
	if provider == "" {
		provider = "ollama"
	}

	result, err := ReanalyzeResume(req.LatexSource, req.JobDescription, provider, req.Model, req.OllamaHost)
	if err != nil {
		respondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	respondJSONWithData(w, http.StatusOK, true, "", "", result)
}

func handleResumeChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, false, "Invalid JSON payload", "")
		return
	}

	if req.Message == "" || req.CurrentLatex == "" {
		respondJSON(w, http.StatusBadRequest, false, "message and current_latex are required", "")
		return
	}

	provider := req.Provider
	if provider == "" {
		provider = "ollama"
	}

	result, err := ChatRefine(req.Message, req.ChatHistory, req.CurrentLatex, req.JobDescription, provider, req.Model, req.OllamaHost)
	if err != nil {
		respondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	respondJSONWithData(w, http.StatusOK, true, "", "", result)
}

func handleResumeCompile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		LatexSource string `json:"latex_source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, false, "Invalid JSON payload", "")
		return
	}

	if req.LatexSource == "" {
		respondJSON(w, http.StatusBadRequest, false, "latex_source is required", "")
		return
	}

	pdfData, err := CompileLatexToPDF(req.LatexSource)
	if err != nil {
		respondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Write(pdfData)
}
