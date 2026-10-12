package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	App      AppConfig
	Database DatabaseConfig
	JWT      JWTConfig
	CORS     CORSConfig
	Logger   LoggerConfig
	Device   DeviceConfig
	Routing  RoutingConfig
	Geocode  GeocodeConfig
	Report   ReportConfig
	Seed     SeedConfig
}

type AppConfig struct {
	Name        string
	Env         string
	Port        string
	FrontendURL string
	Debug       bool
	DataDir     string
}

type DatabaseConfig struct {
	// URL is the full connection string (DATABASE_URL, shared with Prisma).
	// When set it wins over the individual DB_* fields.
	URL      string
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
	TimeZone string
}

type JWTConfig struct {
	Secret             string
	RefreshSecret      string
	ExpiredHour        int
	RefreshExpiredHour int
}

type CORSConfig struct {
	AllowedOrigins []string
}

type LoggerConfig struct {
	// Format is "console" (human readable, coloured on a terminal), "json"
	// (Loki/Promtail friendly), or empty to auto-detect.
	Format      string
	LogToFile   bool
	LogFilePath string
	MaxSize     int
	MaxBackups  int
	MaxAge      int
	Compress    bool
}

// DeviceConfig is the Teltonika TCP listener (FMC150 trackers connect here).
type DeviceConfig struct {
	Port string
	// Tunnel: auto | ngrok | bore | 0 (see pkg/tunnel).
	TunnelMode     string
	NgrokAuthtoken string
	BoreHost       string
	// TunnelHost / TunnelPort force the public address shown on the dashboard
	// (e.g. a fixed forwarded port). Set = no tunnel is opened.
	TunnelHost string
	TunnelPort int
}

type RoutingConfig struct {
	Enabled     bool
	OSRMURL     string
	TruckFactor float64
	OverpassURL string
}

type GeocodeConfig struct {
	Enabled bool
}

type ReportConfig struct {
	// TimeZone groups reports per day and decides what "today" means.
	TimeZone string
	Location *time.Location
}

type SeedConfig struct {
	// Password for every seeded user (one per role).
	Password string
	// Optional override for the ADMIN account email.
	AdminEmail string
	AdminName  string
}

func LoadConfig() (*Config, error) {
	viper.SetConfigFile(".env")
	viper.AutomaticEnv()

	if _, err := os.Stat(".env"); err == nil {
		if err := viper.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to access config file: %w", err)
	}

	viper.SetDefault("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:3001")
	viper.SetDefault("APP_PORT", "3100")
	viper.SetDefault("DEVICE_PORT", "3101")
	viper.SetDefault("TUNNEL", "auto")
	viper.SetDefault("BORE_HOST", "bore.pub")
	viper.SetDefault("APP_DATA_DIR", "data")
	viper.SetDefault("JWT_EXPIRED_HOUR", 1)
	viper.SetDefault("JWT_REFRESH_EXPIRED_HOUR", 168)
	viper.SetDefault("ROUTING", true)
	viper.SetDefault("TRUCK_FACTOR", 1.3)
	viper.SetDefault("OSRM_URL", "https://router.project-osrm.org")
	viper.SetDefault("GEOCODE", true)
	viper.SetDefault("TZ_REPORT", "Asia/Jakarta")
	viper.SetDefault("DB_SSL_MODE", "disable")
	viper.SetDefault("DB_TIMEZONE", "Asia/Jakarta")

	tz := strings.TrimSpace(viper.GetString("TZ_REPORT"))
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid TZ_REPORT %q: %w", tz, err)
	}

	jwtSecret := viper.GetString("JWT_SECRET")
	refreshSecret := viper.GetString("JWT_REFRESH_SECRET")
	if refreshSecret == "" {
		refreshSecret = jwtSecret + ":refresh"
	}

	config := &Config{
		App: AppConfig{
			Name:        viper.GetString("APP_NAME"),
			Env:         viper.GetString("APP_ENV"),
			Port:        viper.GetString("APP_PORT"),
			FrontendURL: viper.GetString("APP_FRONTEND_URL"),
			Debug:       viper.GetBool("APP_DEBUG"),
			DataDir:     viper.GetString("APP_DATA_DIR"),
		},
		Database: DatabaseConfig{
			URL:      strings.TrimSpace(viper.GetString("DATABASE_URL")),
			Host:     viper.GetString("DB_HOST"),
			Port:     viper.GetString("DB_PORT"),
			User:     viper.GetString("DB_USER"),
			Password: viper.GetString("DB_PASSWORD"),
			Name:     viper.GetString("DB_NAME"),
			SSLMode:  viper.GetString("DB_SSL_MODE"),
			TimeZone: viper.GetString("DB_TIMEZONE"),
		},
		JWT: JWTConfig{
			Secret:             jwtSecret,
			RefreshSecret:      refreshSecret,
			ExpiredHour:        viper.GetInt("JWT_EXPIRED_HOUR"),
			RefreshExpiredHour: viper.GetInt("JWT_REFRESH_EXPIRED_HOUR"),
		},
		CORS: CORSConfig{
			AllowedOrigins: origins(viper.GetString("CORS_ALLOWED_ORIGINS")),
		},
		Logger: LoggerConfig{
			Format:   strings.TrimSpace(viper.GetString("LOG_FORMAT")),
			MaxAge:   viper.GetInt("LOG_MAX_AGE"),
			Compress: viper.GetBool("LOG_COMPRESS"),
		},
		Device: DeviceConfig{
			Port:           viper.GetString("DEVICE_PORT"),
			TunnelMode:     strings.TrimSpace(viper.GetString("TUNNEL")),
			NgrokAuthtoken: strings.TrimSpace(viper.GetString("NGROK_AUTHTOKEN")),
			BoreHost:       strings.TrimSpace(viper.GetString("BORE_HOST")),
			TunnelHost: strings.TrimSpace(viper.GetString("TUNNEL_HOST")),
			TunnelPort: viper.GetInt("TUNNEL_PORT"),
		},
		Routing: RoutingConfig{
			Enabled:     viper.GetBool("ROUTING"),
			OSRMURL:     strings.TrimRight(viper.GetString("OSRM_URL"), "/"),
			TruckFactor: viper.GetFloat64("TRUCK_FACTOR"),
			OverpassURL: strings.TrimSpace(viper.GetString("OVERPASS_URL")),
		},
		Geocode: GeocodeConfig{
			Enabled: viper.GetBool("GEOCODE"),
		},
		Report: ReportConfig{
			TimeZone: tz,
			Location: loc,
		},
		Seed: SeedConfig{
			Password:   viper.GetString("SEED_PASSWORD"),
			AdminEmail: viper.GetString("SEED_ADMIN_EMAIL"),
			AdminName:  viper.GetString("SEED_ADMIN_NAME"),
		},
	}

	if config.JWT.Secret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	if config.Routing.TruckFactor <= 0 {
		config.Routing.TruckFactor = 1.3
	}

	return config, nil
}

func (c *DatabaseConfig) DSN() string {
	if c.URL != "" {
		return c.URL
	}
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=%s",
		c.Host, c.User, c.Password, c.Name, c.Port, c.SSLMode, c.TimeZone,
	)
}

// Label is the DSN without credentials, safe for logs.
func (c *DatabaseConfig) Label() string {
	if c.URL != "" {
		u := c.URL
		if at := strings.LastIndex(u, "@"); at >= 0 {
			u = u[at+1:]
		}
		if q := strings.Index(u, "?"); q >= 0 {
			u = u[:q]
		}
		return "postgres://" + u
	}
	return fmt.Sprintf("postgres://%s:%s/%s", c.Host, c.Port, c.Name)
}

// origins splits a comma list and drops blanks (an empty entry would make the CORS middleware panic).
func origins(list string) []string {
	out := []string{}
	for _, o := range strings.Split(list, ",") {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	if len(out) == 0 {
		return []string{"http://localhost:3000", "http://localhost:3001"}
	}
	return out
}
