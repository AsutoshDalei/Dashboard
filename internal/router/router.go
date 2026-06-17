package router

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"

	"pi_dashboard/internal/auth"
	"pi_dashboard/internal/clipboard"
	"pi_dashboard/internal/coverletter"
	"pi_dashboard/internal/database"
	"pi_dashboard/internal/email"
	"pi_dashboard/internal/jobposted"
	"pi_dashboard/internal/middleware"
	"pi_dashboard/internal/tracker"
	"pi_dashboard/internal/workspace"
	"pi_dashboard/pkg/observability"
)

type Dependencies struct {
	Auth        *auth.Handler
	AuthStore   *auth.SessionStore
	Templates   *template.Template
	DB          *database.Pool
	Stats       *observability.StatsCollector
	Email       *email.Handler
	CoverLetter *coverletter.Handler
	Clipboard   *clipboard.Handler
	Tracker     *tracker.Handler
	JobPosted   *jobposted.Handler
	Workspace   *workspace.Handler
}

type Router struct {
	mux       *http.ServeMux
	templates *template.Template
	deps      Dependencies
	authH     *auth.Handler
}

func New(deps Dependencies) *Router {
	r := &Router{
		mux:       http.NewServeMux(),
		templates: deps.Templates,
		deps:      deps,
		authH:     deps.Auth,
	}
	r.registerRoutes()
	return r
}

func (r *Router) Handler() http.Handler {
	var h http.Handler = r.mux
	h = middleware.SecurityHeaders(h)
	h = middleware.RequestID(h)
	h = middleware.Logging(h)
	return h
}

func (r *Router) registerRoutes() {
	r.mux.HandleFunc("/", r.handleRoot)
	r.mux.HandleFunc("/login", r.authH.HandleLogin)
	r.mux.HandleFunc("/auth", r.authH.HandleAuth)
	r.mux.HandleFunc("/logout", r.authH.HandleLogout)

	r.mux.HandleFunc("/healthz", r.handleHealthz)
	r.mux.HandleFunc("/health/live", r.handleHealthLive)
	r.mux.HandleFunc("/health/ready", r.handleHealthReady)
	r.mux.HandleFunc("/metrics", r.handleMetrics)

	r.mux.HandleFunc("/dashboard", middleware.RequireAuth(r.deps.AuthStore)(r.handleDashboard))

	auth := middleware.RequireAuth(r.deps.AuthStore)

	if r.deps.Email != nil {
		r.mux.HandleFunc("/tools/email", auth(r.deps.Email.HandleTool))
		r.mux.HandleFunc("/send-email", auth(r.deps.Email.HandleSend))
		r.mux.HandleFunc("/email-templates", auth(r.deps.Email.HandleTemplates))
	}

	if r.deps.CoverLetter != nil {
		r.mux.HandleFunc("/tools/cover-letter", auth(r.deps.CoverLetter.HandleTool))
		r.mux.HandleFunc("/generate-cover-letter", auth(r.deps.CoverLetter.HandleGenerate))
	}

	if r.deps.Clipboard != nil {
		r.mux.HandleFunc("/tools/clipboard", auth(r.deps.Clipboard.HandleTool))
		r.mux.HandleFunc("/api/clipboard", auth(r.deps.Clipboard.HandleAPI))
		r.mux.HandleFunc("/api/clipboard/reorder", auth(r.deps.Clipboard.HandleReorderAPI))
		r.mux.HandleFunc("/api/clipboard/", auth(r.deps.Clipboard.HandleItemAPI))
	}

	if r.deps.Tracker != nil {
		r.mux.HandleFunc("/tools/tracker", auth(r.deps.Tracker.HandleTool))
		r.mux.HandleFunc("/api/applications", auth(r.deps.Tracker.HandleUpsert))
		r.mux.HandleFunc("/api/applications/check", auth(r.deps.Tracker.HandleCheck))
		r.mux.HandleFunc("/api/applications/suggest", auth(r.deps.Tracker.HandleSuggest))
		r.mux.HandleFunc("/api/applications/stats", auth(r.deps.Tracker.HandleStats))
		r.mux.HandleFunc("/api/applications/timeline", auth(r.deps.Tracker.HandleTimeline))
		r.mux.HandleFunc("/api/applications/contribution", auth(r.deps.Tracker.HandleContribution))
		r.mux.HandleFunc("/api/applications/contribution-range", auth(r.deps.Tracker.HandleContributionRange))
		r.mux.HandleFunc("/api/applications/query", auth(r.deps.Tracker.HandleQuery))
	}

	if r.deps.JobPosted != nil {
		r.mux.HandleFunc("/tools/jobposted", auth(r.deps.JobPosted.HandleTool))
		r.mux.HandleFunc("/api/job-posted/check", auth(r.deps.JobPosted.HandleCheck))
	}

	if r.deps.Workspace != nil {
		r.mux.HandleFunc("/tools/resume", auth(r.deps.Workspace.HandleResumeTool))
		r.mux.HandleFunc("/api/resume/analyze", auth(r.deps.Workspace.HandleResumeAnalyze))
		r.mux.HandleFunc("/api/resume/analyze-async", auth(r.deps.Workspace.HandleResumeAnalyzeAsync))
		r.mux.HandleFunc("/api/resume/generate", auth(r.deps.Workspace.HandleResumeGenerate))
		r.mux.HandleFunc("/api/resume/generate-async", auth(r.deps.Workspace.HandleResumeGenerateAsync))
		r.mux.HandleFunc("/api/resume/compile", auth(r.deps.Workspace.HandleResumeCompile))
		r.mux.HandleFunc("/api/resume/reanalyze", auth(r.deps.Workspace.HandleResumeReanalyze))
		r.mux.HandleFunc("/api/resume/chat", auth(r.deps.Workspace.HandleResumeChat))
		r.mux.HandleFunc("/api/resume/job-match", auth(r.deps.Workspace.HandleJobMatch))
		r.mux.HandleFunc("/api/job/status", auth(r.deps.Workspace.HandleJobStatus))
		r.mux.HandleFunc("/tools/chat", auth(r.deps.Workspace.HandleChatTool))
		r.mux.HandleFunc("/api/chat/send", auth(r.deps.Workspace.HandleChatSend))
		r.mux.HandleFunc("/api/chat/send-async", auth(r.deps.Workspace.HandleChatSendAsync))
		r.mux.HandleFunc("/api/chat/clear", auth(r.deps.Workspace.HandleChatClear))
	}
}

func (r *Router) handleHealthz(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (r *Router) handleHealthLive(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "alive"})
}

func (r *Router) handleHealthReady(w http.ResponseWriter, req *http.Request) {
	status := "ready"
	code := http.StatusOK

	if r.deps.DB != nil {
		if err := r.deps.DB.Health(req.Context()); err != nil {
			status = "not ready"
			code = http.StatusServiceUnavailable
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}

func (r *Router) handleMetrics(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.deps.Stats != nil {
		json.NewEncoder(w).Encode(r.deps.Stats.Snapshot())
	} else {
		json.NewEncoder(w).Encode(map[string]string{"message": "metrics not enabled"})
	}
}

func (r *Router) handleRoot(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/" {
		http.NotFound(w, req)
		return
	}
	http.Redirect(w, req, "/login", http.StatusSeeOther)
}

func (r *Router) handleDashboard(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := r.templates.ExecuteTemplate(w, "dashboard.html", nil); err != nil {
		slog.Error("template", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}
