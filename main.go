package main

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"pi_dashboard/internal/auth"
	"pi_dashboard/internal/clipboard"
	"pi_dashboard/internal/config"
	"pi_dashboard/internal/coverletter"
	"pi_dashboard/internal/database"
	"pi_dashboard/internal/email"
	"pi_dashboard/internal/llm"
	"pi_dashboard/internal/middleware"
	"pi_dashboard/internal/router"
	"pi_dashboard/internal/tracker"
	"pi_dashboard/internal/workspace"
	pkgllm "pi_dashboard/pkg/llm"
	"pi_dashboard/pkg/observability"

	"golang.ngrok.com/ngrok/v2"
)

//go:embed templates/*
var templateFiles embed.FS

//go:embed static/*
var staticFiles embed.FS

var (
	templates    = parseTemplates()
	staticFS     = http.FileServer(mustStaticFS())
	dbPool       *database.Pool
	dbPoolReader *database.Pool
	buildTime    string
	commitHash   string
)

func parseTemplates() *template.Template {
	tmpl, err := template.ParseFS(templateFiles, "templates/*.html")
	if err != nil {
		slog.Error("parse templates", "err", err)
		os.Exit(1)
	}
	return tmpl
}

func mustStaticFS() http.FileSystem {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		slog.Error("static fs", "err", err)
		os.Exit(1)
	}
	return http.FS(sub)
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	poolCfg := database.DefaultConfig(cfg.DatabaseURL)
	if cfg.DBMaxConns > 0 {
		poolCfg.MaxConns = int32(cfg.DBMaxConns)
	}

	dbPool, err = database.NewPool(ctx, poolCfg)
	if err != nil {
		slog.Error("database pool", "err", err)
		os.Exit(1)
	}
	defer dbPool.Close()
	slog.Info("database connected")

	if err := database.RunMigrations(ctx, dbPool.Pool); err != nil {
		slog.Error("migrations", "err", err)
		os.Exit(1)
	}

	if u := strings.TrimSpace(os.Getenv("DATABASE_URL_READER")); u != "" {
		rcfg := database.DefaultConfig(u)
		rcfg.MaxConns = 4
		dbPoolReader, err = database.NewPool(ctx, rcfg)
		if err != nil {
			slog.Error("reader pool", "err", err)
			os.Exit(1)
		}
		defer dbPoolReader.Close()
		slog.Info("reader pool enabled")
	}

	statsCollector := observability.NewStatsCollector()

	sessionStore := auth.NewSessionStore()
	go sessionStore.RunJanitor(ctx)

	llmCfg := llm.Config{
		ProviderType:     cfg.LLMProvider,
		OllamaHost:       cfg.OllamaHost,
		OllamaModel:      cfg.OllamaModel,
		OpenRouterAPIKey: cfg.OpenRouterAPIKey,
		OpenRouterModel:  cfg.OpenRouterModel,
	}
	var llmProvider pkgllm.Provider
	llmProvider, err = llm.NewProvider(llmCfg)
	if err != nil {
		slog.Warn("llm provider", "err", err)
	}

	authHandler := auth.NewHandler(sessionStore, cfg.AccessPasskey, templates)

	emailSvc := email.NewService(cfg.UniversityEmail, cfg.PersonalEmail, cfg.DefaultSenderKey)
	gmailConfig := &email.GmailConfig{
		AccessToken:  cfg.GmailAccessToken,
		RefreshToken: cfg.GmailRefreshToken,
		ClientID:     cfg.GmailClientID,
		ClientSecret: cfg.GmailClientSecret,
		TokenURI:     cfg.GmailTokenURI,
		Expiry:       cfg.GmailExpiry,
	}
	emailHandler := email.NewHandler(emailSvc, templates, gmailConfig)

	coverLetterSvc := coverletter.NewService()
	coverLetterHandler := coverletter.NewHandler(coverLetterSvc, templates)

	clipboardRepo := clipboard.NewRepository(dbPool.Pool)
	clipboardSvc := clipboard.NewService(clipboardRepo)
	clipboardHandler := clipboard.NewHandler(clipboardSvc, templates)

	var prompts *llm.Prompts
	if path := cfg.SystemPromptsPath; path != "" {
		prompts, _ = llm.LoadPrompts(path)
	}
	if prompts == nil {
		slog.Error("failed to load system prompts", "path", cfg.SystemPromptsPath)
		os.Exit(1)
	}

	trackerRepo := tracker.NewRepository(dbPool.Pool)
	trackerSvc := tracker.NewService(trackerRepo, llmProvider, cfg.ActivityStatsTimezone, prompts)
	trackerHandler := tracker.NewHandler(trackerSvc, templates)

	resumeText, _ := os.ReadFile("resume.tex")
	resumeMarkdown, _ := os.ReadFile("resume.md")

	workspaceSvc := workspace.NewService(llmProvider, string(resumeText), string(resumeMarkdown), prompts)
	workspaceHandler := workspace.NewHandler(workspaceSvc, templates, prompts)

	deps := router.Dependencies{
		Auth:        authHandler,
		AuthStore:   sessionStore,
		Templates:   templates,
		DB:          dbPool,
		Stats:       statsCollector,
		Email:       emailHandler,
		CoverLetter: coverLetterHandler,
		Clipboard:   clipboardHandler,
		Tracker:     trackerHandler,
		Workspace:   workspaceHandler,
	}

	r := router.New(deps)
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", staticFS))
	mux.Handle("/", r)

	handler := middleware.SecurityHeaders(middleware.RequestID(middleware.Logging(mux)))

	port := cfg.Port
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	switch {
	case cfg.EnableNgrok:
		token := cfg.NgrokAuthtoken
		if token == "" {
			slog.Error("ENABLE_NGROK set but NGROK_AUTHTOKEN empty")
			os.Exit(1)
		}
		agent, err := ngrok.NewAgent(ngrok.WithAuthtoken(token))
		if err != nil {
			slog.Error("ngrok agent", "err", err)
			os.Exit(1)
		}
		listenOpts := []ngrok.EndpointOption{ngrok.WithPoolingEnabled(true)}
		if u := cfg.NgrokInternalURL; u != "" {
			listenOpts = append(listenOpts, ngrok.WithURL(u))
		}
		ln, err := agent.Listen(ctx, listenOpts...)
		if err != nil {
			slog.Error("ngrok listen", "err", err)
			os.Exit(1)
		}
		tunnelURL := ln.URL()
		envPublic := cfg.NgrokPublicURL
		isInternal := tunnelURL != nil && strings.HasSuffix(strings.ToLower(tunnelURL.Hostname()), "internal")

		slog.Info("ready: ngrok")
		switch {
		case envPublic != "":
			slog.Info("public url", "url", envPublic)
		case isInternal:
			slog.Info("internal tunnel", "url", tunnelURL.String(), "hint", "set NGROK_PUBLIC_URL")
		default:
			slog.Info("url", "url", tunnelURL.String())
		}
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("server", "err", err)
			os.Exit(1)
		}
	default:
		lan := firstNonLoopbackIPv4()
		slog.Info("ready", "local", "http://127.0.0.1:"+port)
		if lan != "" {
			slog.Info("lan", "url", "http://"+lan+":"+port)
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server", "err", err)
			os.Exit(1)
		}
	}
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