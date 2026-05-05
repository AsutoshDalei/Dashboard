package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"golang.ngrok.com/ngrok/v2"
)

//go:embed templates/*
var templateFiles embed.FS

//go:embed static/*
var staticFiles embed.FS

var templates *template.Template
var dbPool *pgxpool.Pool

type EmailRequest struct {
	Name      string `json:"name"`
	Company   string `json:"company"`
	Email     string `json:"email"`
	SenderKey string `json:"sender_key"`
}

type LoginData struct {
	Error string
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
}

func generateToken() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		log.Printf("Error generating token: %v", err)
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
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
		log.Fatalf("Error parsing templates: %v", err)
	}
}

func main() {
	if err := godotenv.Load(".env"); err != nil {
		if err = godotenv.Load("../.env"); err != nil {
			log.Println("Warning: No .env file found in .env or ../.env. Relying on system environment variables.")
		}
	}

	if _, err := GetConfig("university"); err != nil {
		log.Printf("Warning: failed to load university config: %v", err)
	}

	passkey := os.Getenv("ACCESS_PASSKEY")
	if passkey == "" {
		log.Println("Warning: ACCESS_PASSKEY not set. Authentication will fail for all requests.")
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	var err error
	dbPool, err = pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		log.Fatalf("Failed to create database pool: %v", err)
	}
	if pingErr := dbPool.Ping(context.Background()); pingErr != nil {
		log.Fatalf("Failed to connect to database: %v", pingErr)
	}
	defer dbPool.Close()

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("Error setting up static filesystem: %v", err)
	}
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Public routes
	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/login", handleLogin)
	http.HandleFunc("/auth", handleAuth)
	http.HandleFunc("/logout", handleLogout)

	// Protected routes
	http.HandleFunc("/dashboard", requireAuth(handleDashboard))
	http.HandleFunc("/tools/email", requireAuth(handleEmailTool))
	http.HandleFunc("/tools/cover-letter", requireAuth(handleCoverLetterTool))
	http.HandleFunc("/generate-cover-letter", requireAuth(handleGenerateCoverLetter))
	http.HandleFunc("/tools/clipboard", requireAuth(handleClipboardTool))
	http.HandleFunc("/api/clipboard", requireAuth(handleClipboardAPI))
	http.HandleFunc("/api/clipboard/", requireAuth(handleClipboardItemAPI))
	http.HandleFunc("/tools/tracker", requireAuth(handleTrackerTool))
	http.HandleFunc("/api/applications", requireAuth(handleApplicationsUpsert))
	http.HandleFunc("/api/applications/check", requireAuth(handleApplicationsCheck))
	http.HandleFunc("/api/applications/suggest", requireAuth(handleApplicationsSuggest))
	http.HandleFunc("/api/applications/stats", requireAuth(handleApplicationsStats))
	http.HandleFunc("/api/applications/timeline", requireAuth(handleApplicationsTimeline))
	http.HandleFunc("/api/applications/contribution", requireAuth(handleApplicationsContribution))
	http.HandleFunc("/api/applications/query", requireAuth(handleApplicationsQuery))
	http.HandleFunc("/send-email", requireAuth(handleSendEmail))

	port := "5001"
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_NGROK"))) {
	case "1", "true", "yes":
		token := strings.TrimSpace(os.Getenv("NGROK_AUTHTOKEN"))
		if token == "" {
			log.Fatal("ENABLE_NGROK is set but NGROK_AUTHTOKEN is empty; add your authtoken to the environment or .env")
		}

		agent, err := ngrok.NewAgent(ngrok.WithAuthtoken(token))
		if err != nil {
			log.Fatalf("ngrok NewAgent: %v", err)
		}
		listenOpts := []ngrok.EndpointOption{ngrok.WithPoolingEnabled(true)}
		if internalURL := strings.TrimSpace(os.Getenv("NGROK_INTERNAL_ENDPOINT_URL")); internalURL != "" {
			listenOpts = append(listenOpts, ngrok.WithURL(internalURL))
		}

		ln, err := agent.Listen(context.Background(), listenOpts...)
		if err != nil {
			log.Fatalf("ngrok Listen failed: %v (check token, network, and https://status.ngrok.com)", err)
		}

		tunnelURL := ln.URL()
		envPublic := strings.TrimSpace(os.Getenv("NGROK_PUBLIC_URL"))
		isInternal := tunnelURL != nil && strings.HasSuffix(strings.ToLower(tunnelURL.Hostname()), "internal")

		log.Println("Ready. Checks: configuration loaded, database connected, HTTP routes registered, ngrok listener active.")
		if lan := firstNonLoopbackIPv4(); lan != "" {
			log.Printf("Local/LAN HTTP: not used while ENABLE_NGROK=true (this host LAN IP is %s; use ENABLE_NGROK=false for http://127.0.0.1:%s and http://%s:%s)", lan, port, lan, port)
		} else {
			log.Printf("Local/LAN HTTP: not used while ENABLE_NGROK=true (use ENABLE_NGROK=false for http://127.0.0.1:%s)", port)
		}
		switch {
		case envPublic != "":
			log.Printf("Internet (website): %s", envPublic)
		case isInternal:
			log.Printf("Internet (website): your Cloud Endpoint URL in the browser (traffic policy forward-internal → %s); optional NGROK_PUBLIC_URL in .env to print the public URL here", tunnelURL.String())
		default:
			log.Printf("Internet (website): %s", tunnelURL.String())
		}

		if err := http.Serve(ln, nil); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	default:
		lan := firstNonLoopbackIPv4()
		log.Println("Ready. Checks: configuration loaded, database connected, HTTP routes registered, listening for HTTP.")
		log.Printf("Local:  http://127.0.0.1:%s", port)
		if lan != "" {
			log.Printf("LAN:    http://%s:%s", lan, port)
		}
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			log.Fatalf("Server failed to start: %v", err)
		}
	}
}

// Authentication middleware
func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || !sessionStore.Validate(cookie.Value) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				respondJSON(w, http.StatusUnauthorized, false, "authentication required", "")
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
	if err := templates.ExecuteTemplate(w, "login.html", LoginData{}); err != nil {
		log.Printf("Template rendering error: %v", err)
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

	passkey := r.FormValue("passkey")
	expectedPasskey := os.Getenv("ACCESS_PASSKEY")

	if passkey == "" || passkey != expectedPasskey {
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
	if err := templates.ExecuteTemplate(w, "login.html", LoginData{Error: errorMsg}); err != nil {
		log.Printf("Template rendering error: %v", err)
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
		log.Printf("Template rendering error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func handleEmailTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "email.html", nil); err != nil {
		log.Printf("Template rendering error: %v", err)
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

	if req.SenderKey == "" {
		defaultKey := os.Getenv("DEFAULT_SENDER_KEY")
		if defaultKey == "" {
			defaultKey = "university"
		}
		req.SenderKey = defaultKey
	}

	senderLabel, err := SendEmail(req.Email, req.Name, req.Company, strings.ToLower(strings.TrimSpace(req.SenderKey)))
	if err != nil {
		log.Printf("Error sending email: %v", err)
		respondJSON(w, http.StatusInternalServerError, false, err.Error(), "")
		return
	}

	respondJSON(w, http.StatusOK, true, "", fmt.Sprintf("Email sent successfully via %s!", senderLabel))
}

func handleCoverLetterTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "coverletter.html", nil); err != nil {
		log.Printf("Template rendering error: %v", err)
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
		log.Printf("Error generating cover letter PDF: %v", err)
		respondJSON(w, http.StatusInternalServerError, false, err.Error(), "")
		return
	}

	safeCompanyName := strings.ReplaceAll(companyName, " ", "_")
	safeCompanyName = strings.ReplaceAll(safeCompanyName, "/", "_")
	safeCompanyName = strings.ReplaceAll(safeCompanyName, "\\", "_")

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"ASUTOSH_DALEI_COVERLETTER_%s.pdf\"", safeCompanyName))

	if _, err := w.Write(pdfData); err != nil {
		log.Printf("Error writing PDF to client: %v", err)
	}
}

func respondJSON(w http.ResponseWriter, status int, success bool, errMessage, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := map[string]interface{}{
		"success": success,
	}
	if errMessage != "" {
		resp["error"] = errMessage
	}
	if message != "" {
		resp["message"] = message
	}
	json.NewEncoder(w).Encode(resp)
}
