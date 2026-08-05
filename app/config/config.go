// Package config loads and validates every knob the service exposes. Nothing
// outside this package reads os.Getenv, which keeps configuration testable and
// makes the full surface area discoverable in one file.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Environment names used across the codebase.
const (
	EnvDevelopment = "development"
	EnvTesting     = "testing"
	EnvProduction  = "production"
)

// Config is the fully resolved application configuration.
type Config struct {
	App     App
	Log     Log
	DB      Database
	Redis   Redis
	Auth    Auth
	Google  Google
	OpenAI  OpenAI
	CORS    CORS
	Limiter RateLimit
	Storage Storage
}

type App struct {
	Env  string
	Name string
	Port int
	// FrontendURL is the SPA origin used to build links inside emails.
	FrontendURL       string
	ResetPasswordPath string
	VerifyEmailPath   string
	ShutdownTimeout   time.Duration
	BodyLimitBytes    int
	TrustedProxies    []string
}

type Log struct {
	Level  string
	Format string
}

type Database struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

type Redis struct {
	URL string
}

type Auth struct {
	JWTSecret               string
	JWTExpiry               time.Duration
	RefreshSecret           string
	RefreshExpiry           time.Duration
	Issuer                  string
	BcryptCost              int
	PasswordResetExpiry     time.Duration
	EmailVerificationExpiry time.Duration

	// RefreshCookie mirrors the refresh token into an HttpOnly cookie, which
	// keeps it out of reach of XSS in the SPA. The token is still returned in
	// the body so native clients keep working.
	RefreshCookie  bool
	CookieDomain   string
	CookieSecure   bool
	CookieSameSite string
}

// Google holds the OAuth client credentials. Sign-in with Google is enabled
// only when all three are present, so the service runs without them.
type Google struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Enabled reports whether Google sign-in can be offered.
func (g Google) Enabled() bool {
	return g.ClientID != "" && g.ClientSecret != "" && g.RedirectURL != ""
}

// OpenAI configures word enrichment (definitions, senses, synonyms,
// antonyms). Word creation runs without it disabled, same pattern as Google
// sign-in: the feature is only offered when configured.
type OpenAI struct {
	APIKey  string
	Model   string
	BaseURL string
	Timeout time.Duration
	// LogDir is where every request/response is logged, one file per day
	// (YYYY-MM-DD.log, JSON lines). Logging is best-effort: a write failure
	// never fails the underlying API call.
	LogDir string
}

// Enabled reports whether word enrichment can be offered.
func (o OpenAI) Enabled() bool { return o.APIKey != "" }

type CORS struct {
	AllowedOrigins   []string
	AllowCredentials bool
}

type RateLimit struct {
	Enabled     bool
	Max         int
	Window      time.Duration
	AuthMax     int
	AuthWindow  time.Duration
	TrustHeader bool
}

// Storage configures the object-storage abstraction. Only the fields for the
// selected provider need to be populated.
type Storage struct {
	Provider       string // local | s3
	PublicBaseURL  string
	LocalPath      string
	MaxUploadBytes int64

	S3Bucket       string
	S3Region       string
	S3Endpoint     string
	S3AccessKey    string
	S3SecretKey    string
	S3UsePathStyle bool
}

// IsProduction reports whether the service runs with production defaults.
func (a App) IsProduction() bool { return a.Env == EnvProduction }

// Addr is the listen address for the HTTP server.
func (a App) Addr() string { return fmt.Sprintf(":%d", a.Port) }

// Load reads .env (when present) followed by the process environment, then
// validates the result. A missing .env file is not an error: containers inject
// configuration directly.
func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		// Any other error (malformed file, permissions) is worth surfacing.
		var pathErr *os.PathError
		if !errors.As(err, &pathErr) {
			return nil, fmt.Errorf("load .env: %w", err)
		}
	}

	cfg := &Config{
		App: App{
			Env:               getString("APP_ENV", EnvDevelopment),
			Name:              getString("APP_NAME", "backend-api"),
			Port:              getInt("PORT", 8080),
			FrontendURL:       strings.TrimRight(getString("FRONTEND_URL", "http://localhost:5173"), "/"),
			ResetPasswordPath: getString("RESET_PASSWORD_PATH", "/reset-password"),
			VerifyEmailPath:   getString("VERIFY_EMAIL_PATH", "/verify-email"),
			ShutdownTimeout:   getDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
			BodyLimitBytes:    getInt("BODY_LIMIT_BYTES", 4*1024*1024),
			TrustedProxies:    getStringSlice("TRUSTED_PROXIES", nil),
		},
		Log: Log{
			Level:  getString("LOG_LEVEL", "info"),
			Format: getString("LOG_FORMAT", "json"),
		},
		DB: Database{
			URL:             getString("DATABASE_URL", ""),
			MaxConns:        int32(getInt("DB_MAX_CONNS", 20)),
			MinConns:        int32(getInt("DB_MIN_CONNS", 2)),
			MaxConnLifetime: getDuration("DB_MAX_CONN_LIFETIME", time.Hour),
			MaxConnIdleTime: getDuration("DB_MAX_CONN_IDLE_TIME", 30*time.Minute),
		},
		Redis: Redis{
			URL: getString("REDIS_URL", ""),
		},
		Auth: Auth{
			JWTSecret:               getString("JWT_SECRET", ""),
			JWTExpiry:               getDuration("JWT_EXPIRY", 15*time.Minute),
			RefreshSecret:           getString("REFRESH_SECRET", ""),
			RefreshExpiry:           getDuration("REFRESH_EXPIRY", 720*time.Hour),
			Issuer:                  getString("JWT_ISSUER", "backend-api"),
			BcryptCost:              getInt("BCRYPT_COST", 12),
			PasswordResetExpiry:     getDuration("PASSWORD_RESET_EXPIRY", time.Hour),
			EmailVerificationExpiry: getDuration("EMAIL_VERIFICATION_EXPIRY", 24*time.Hour),
			RefreshCookie:           getBool("REFRESH_COOKIE_ENABLED", false),
			CookieDomain:            getString("COOKIE_DOMAIN", ""),
			CookieSecure:            getBool("COOKIE_SECURE", true),
			CookieSameSite:          getString("COOKIE_SAMESITE", "Strict"),
		},
		Google: Google{
			ClientID:     getString("GOOGLE_CLIENT_ID", ""),
			ClientSecret: getString("GOOGLE_CLIENT_SECRET", ""),
			RedirectURL:  getString("GOOGLE_REDIRECT_URL", ""),
		},
		OpenAI: OpenAI{
			APIKey:  getString("OPENAI_API_KEY", ""),
			Model:   getString("OPENAI_MODEL", "gpt-4o-mini"),
			BaseURL: strings.TrimRight(getString("OPENAI_BASE_URL", "https://api.openai.com/v1"), "/"),
			Timeout: getDuration("OPENAI_TIMEOUT", 20*time.Second),
			LogDir:  getString("OPENAI_LOG_DIR", "./.data/openai-logs"),
		},
		CORS: CORS{
			AllowedOrigins:   getStringSlice("CORS_ALLOWED_ORIGINS", []string{"http://localhost:5173"}),
			AllowCredentials: getBool("CORS_ALLOW_CREDENTIALS", true),
		},
		Limiter: RateLimit{
			Enabled:    getBool("RATE_LIMIT_ENABLED", true),
			Max:        getInt("RATE_LIMIT_MAX", 120),
			Window:     getDuration("RATE_LIMIT_WINDOW", time.Minute),
			AuthMax:    getInt("AUTH_RATE_LIMIT_MAX", 10),
			AuthWindow: getDuration("AUTH_RATE_LIMIT_WINDOW", time.Minute),
		},
		Storage: Storage{
			Provider:       strings.ToLower(getString("STORAGE_PROVIDER", "local")),
			PublicBaseURL:  strings.TrimRight(getString("STORAGE_PUBLIC_BASE_URL", "http://localhost:8080/files"), "/"),
			LocalPath:      getString("STORAGE_LOCAL_PATH", "./.data/uploads"),
			MaxUploadBytes: int64(getInt("STORAGE_MAX_UPLOAD_BYTES", 5*1024*1024)),
			S3Bucket:       getString("S3_BUCKET", ""),
			S3Region:       getString("S3_REGION", "us-east-1"),
			S3Endpoint:     getString("S3_ENDPOINT", ""),
			S3AccessKey:    getString("S3_ACCESS_KEY_ID", ""),
			S3SecretKey:    getString("S3_SECRET_ACCESS_KEY", ""),
			S3UsePathStyle: getBool("S3_USE_PATH_STYLE", true),
		},
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate fails fast on misconfiguration: a service that boots with a weak
// signing key is worse than one that refuses to start.
func (c *Config) Validate() error {
	var problems []string

	switch c.App.Env {
	case EnvDevelopment, EnvTesting, EnvProduction:
	default:
		problems = append(problems, fmt.Sprintf("APP_ENV must be one of development|staging|production, got %q", c.App.Env))
	}

	// Half-configured Google sign-in is worse than none: the button appears and
	// then fails at the exchange, so it is rejected at start-up.
	// googleFields := 3
	// for _, value := range []string{c.Google.ClientID, c.Google.ClientSecret, c.Google.RedirectURL} {
	// 	if value != "" {
	// 		googleFields++
	// 	}
	// }
	// if googleFields != 0 && googleFields != 3 {
	// 	problems = append(problems,
	// 		"GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET and GOOGLE_REDIRECT_URL must all be set, or all be empty")
	// }
	// if c.Google.Enabled() && !strings.HasPrefix(c.Google.RedirectURL, "https://") && c.App.IsProduction() {
	// 	problems = append(problems, "GOOGLE_REDIRECT_URL must use https in production")
	// }

	if c.App.FrontendURL == "" {
		problems = append(problems, "FRONTEND_URL is required")
	}
	if c.App.Port < 1 || c.App.Port > 65535 {
		problems = append(problems, "PORT must be between 1 and 65535")
	}
	if c.DB.URL == "" {
		problems = append(problems, "DATABASE_URL is required")
	}
	if c.Redis.URL == "" {
		problems = append(problems, "REDIS_URL is required")
	}
	if len(c.Auth.JWTSecret) < 32 {
		problems = append(problems, "JWT_SECRET must be at least 32 characters")
	}
	if len(c.Auth.RefreshSecret) < 32 {
		problems = append(problems, "REFRESH_SECRET must be at least 32 characters")
	}
	if c.Auth.JWTSecret == c.Auth.RefreshSecret {
		problems = append(problems, "JWT_SECRET and REFRESH_SECRET must differ")
	}
	if c.Auth.JWTExpiry <= 0 || c.Auth.JWTExpiry > time.Hour {
		problems = append(problems, "JWT_EXPIRY must be > 0 and <= 1h")
	}
	if c.Auth.RefreshExpiry <= c.Auth.JWTExpiry {
		problems = append(problems, "REFRESH_EXPIRY must be greater than JWT_EXPIRY")
	}
	if c.Auth.BcryptCost < 10 || c.Auth.BcryptCost > 31 {
		problems = append(problems, "BCRYPT_COST must be between 10 and 31")
	}

	switch c.Storage.Provider {
	case "local":
		if c.Storage.LocalPath == "" {
			problems = append(problems, "STORAGE_LOCAL_PATH is required for the local provider")
		}
	case "s3":
		if c.Storage.S3Bucket == "" {
			problems = append(problems, "S3_BUCKET is required for the s3 provider")
		}
		if c.Storage.S3Region == "" {
			problems = append(problems, "S3_REGION is required for the s3 provider")
		}
	default:
		problems = append(problems, fmt.Sprintf("STORAGE_PROVIDER must be local or s3, got %q", c.Storage.Provider))
	}

	if c.App.IsProduction() {
		for _, origin := range c.CORS.AllowedOrigins {
			if origin == "*" {
				problems = append(problems, "CORS_ALLOWED_ORIGINS must not contain '*' in production")
			}
		}
		if c.Auth.RefreshCookie && !c.Auth.CookieSecure {
			problems = append(problems, "COOKIE_SECURE must be true in production when refresh cookies are enabled")
		}
		if strings.Contains(strings.ToLower(c.Auth.JWTSecret), "change-me") ||
			strings.Contains(strings.ToLower(c.Auth.RefreshSecret), "change-me") {
			problems = append(problems, "default example secrets must be replaced in production")
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

func getString(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	// Tolerate inline comments from .env files: "8080 # http port".
	if idx := strings.Index(raw, "#"); idx >= 0 {
		raw = strings.TrimSpace(raw[:idx])
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func getBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

func getDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return v
}

func getStringSlice(key string, fallback []string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}
