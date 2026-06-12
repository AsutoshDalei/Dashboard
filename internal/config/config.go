package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	// Database
	DatabaseURL       string
	DatabaseURLReader string
	DBMaxConns        int
	DBReaderMaxConns  int

	// Auth
	AccessPasskey string

	// Server
	Port             string
	EnableNgrok      bool
	NgrokAuthtoken   string
	NgrokInternalURL string
	NgrokPublicURL   string

	// LLM
	LLMProvider         string
	OllamaHost          string
	OllamaModel         string
	OpenRouterAPIKey    string
	OpenRouterModel     string
	OpenRouterModels    string
	OpenRouterChatModel string

	// Email
	Email            string
	Password         string
	UniversityEmail  string
	PersonalEmail    string
	GmailAccessToken  string
	GmailRefreshToken string
	GmailClientID     string
	GmailClientSecret string
	GmailTokenURI     string
	GmailExpiry       string
	DefaultSenderKey string

	// Resume
	ResumePath       string
	ResumeFilename   string
	ResumeTailorPath string

	// Templates
	EmailTemplatePath       string
	CoverLetterTemplatePath string
	SystemPromptsPath       string

	// Backup
	DBDumpDir            string
	DBBackupInterval     time.Duration
	DBBackupMaxRetries   int
	DBBackupRowTolerance int
	DBBackupSchemas      string
	DisableDBBackup      bool

	// Tracker
	EnableSuggestTrgm     bool
	ActivityStatsTimezone string

	// Observability
	LogLevel string
}

func Load() (*Config, error) {
	if err := godotenv.Load(".env"); err != nil {
		if err = godotenv.Load("../.env"); err != nil {
			// no .env file, use system env
		}
	}

	cfg := &Config{
		Port:                    getEnv("PORT", "5001"),
		DatabaseURL:             getEnv("DATABASE_URL", ""),
		DatabaseURLReader:       getEnv("DATABASE_URL_READER", ""),
		DBMaxConns:              getEnvInt("DB_MAX_CONNS", 4),
		DBReaderMaxConns:        getEnvInt("DB_READER_MAX_CONNS", 4),
		AccessPasskey:           getEnv("ACCESS_PASSKEY", ""),
		EnableNgrok:             getEnvBool("ENABLE_NGROK"),
		NgrokAuthtoken:          getEnv("NGROK_AUTHTOKEN", ""),
		NgrokInternalURL:        getEnv("NGROK_INTERNAL_ENDPOINT_URL", ""),
		NgrokPublicURL:          getEnv("NGROK_PUBLIC_URL", ""),
		LLMProvider:             getEnv("LLM_PROVIDER", "ollama"),
		OllamaHost:              getEnv("OLLAMA_HOST", "172.16.7.115"),
		OllamaModel:             getEnv("OLLAMA_MODEL", "gemma4"),
		OpenRouterAPIKey:        getEnv("OPENROUTER_API_KEY", ""),
		OpenRouterModel:         getEnv("OPENROUTER_MODEL", ""),
		OpenRouterModels:        getEnv("OPENROUTER_MODELS", ""),
		OpenRouterChatModel:     getEnv("OPENROUTER_CHAT_MODEL", ""),
		Email:                   getEnv("EMAIL", ""),
		Password:                getEnv("PASSWORD", ""),
		UniversityEmail:         getEnv("UNIVERSITY_EMAIL", ""),
		PersonalEmail:           getEnv("PERSONAL_EMAIL", ""),
		GmailAccessToken:        getEnv("GMAIL_ACCESS_TOKEN", ""),
		GmailRefreshToken:       getEnv("GMAIL_REFRESH_TOKEN", ""),
		GmailClientID:           getEnv("GMAIL_CLIENT_ID", ""),
		GmailClientSecret:       getEnv("GMAIL_CLIENT_SECRET", ""),
		GmailTokenURI:           getEnv("GMAIL_TOKEN_URI", "https://oauth2.googleapis.com/token"),
		GmailExpiry:             getEnv("GMAIL_EXPIRY", ""),
		DefaultSenderKey:        getEnv("DEFAULT_SENDER_KEY", "university"),
		ResumePath:              getEnv("RESUME_PATH", ""),
		ResumeFilename:          getEnv("RESUME_FILENAME", "ASUTOSH_DALEI_RESUME.pdf"),
		ResumeTailorPath:        getEnv("RESUME_TAILOR_PATH", "pi_bundle/resume_tailor"),
		EmailTemplatePath:       getEnv("EMAIL_TEMPLATE_PATH", "templates/email_body.tmpl"),
		CoverLetterTemplatePath: getEnv("COVERLETTER_TEMPLATE_PATH", "templates/coverletter.tex.tmpl"),
		SystemPromptsPath:       getEnv("SYSTEM_PROMPTS_PATH", "system_prompts.json"),
		DBDumpDir:               getEnv("DB_DUMP_DIR", "db_dumps"),
		DBBackupInterval:        getEnvDuration("DB_BACKUP_INTERVAL", 24*time.Hour),
		DBBackupMaxRetries:      getEnvInt("DB_BACKUP_MAX_RETRIES", 2),
		DBBackupRowTolerance:    getEnvInt("DB_BACKUP_ROW_TOLERANCE", 0),
		DBBackupSchemas:         getEnv("DB_BACKUP_SCHEMAS", "public"),
		DisableDBBackup:         getEnvBool("DISABLE_DB_BACKUP"),
		EnableSuggestTrgm:       getEnvBool("ENABLE_SUGGEST_TRGM"),
		ActivityStatsTimezone:   getEnv("ACTIVITY_STATS_TIMEZONE", ""),
		LogLevel:                getEnv("LOG_LEVEL", "info"),
	}

	if cfg.UniversityEmail == "" {
		cfg.UniversityEmail = cfg.Email
	}
	if cfg.PersonalEmail == "" {
		cfg.PersonalEmail = cfg.Email
	}

	return cfg, cfg.Validate()
}

func (c *Config) Validate() error {
	var missing []string

	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if c.AccessPasskey == "" {
		missing = append(missing, "ACCESS_PASSKEY")
	}

	if len(missing) > 0 {
		return fmt.Errorf("required environment variables missing: %s", strings.Join(missing, ", "))
	}

	return nil
}

func getEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}

func getEnvBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}
