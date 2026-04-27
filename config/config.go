package config

import (
	"fmt"
	"net"
	"strings"
	"time"

	cron "github.com/robfig/cron/v3"
	"github.com/spf13/viper"
)

var cleanupCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

type Config struct {
	App      AppConfig      `mapstructure:"app"`
	Log      LogConfig      `mapstructure:"log"`
	HTTP     HTTPConfig     `mapstructure:"http"`
	Go2RTC   Go2RTCConfig   `mapstructure:"go2rtc"`
	Schedule ScheduleConfig `mapstructure:"schedule"`
	Action   ActionConfig   `mapstructure:"action"`
	Snapshot SnapshotConfig `mapstructure:"snapshot"`
	Record   RecordConfig   `mapstructure:"record"`
	MinIO    MinIOConfig    `mapstructure:"minio"`
	MongoDB  MongoDBConfig  `mapstructure:"mongodb"`
	Redis    RedisConfig    `mapstructure:"redis"`
}

type AppConfig struct {
	Name  string `mapstructure:"name"`
	Env   string `mapstructure:"env"`
	BoxIP string `mapstructure:"box_ip"`
}

type LogConfig struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"`
	FilePath   string `mapstructure:"file_path"`
	MaxSizeMB  int    `mapstructure:"max_size_mb"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAgeDays int    `mapstructure:"max_age_days"`
	Compress   bool   `mapstructure:"compress"`
}

type HTTPConfig struct {
	Addr         string        `mapstructure:"addr"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	IdleTimeout  time.Duration `mapstructure:"idle_timeout"`
}

type Go2RTCConfig struct {
	BaseURL            string        `mapstructure:"base_url"`
	Username           string        `mapstructure:"username"`
	Password           string        `mapstructure:"password"`
	ConfigPath         string        `mapstructure:"config_path"`
	RequestTimeout     time.Duration `mapstructure:"request_timeout"`
	BackupBeforeChange bool          `mapstructure:"backup_before_change"`
}

type ScheduleConfig struct {
	RunOnStart        bool          `mapstructure:"run_on_start"`
	Crons             []string      `mapstructure:"crons"`
	ConfirmationDelay time.Duration `mapstructure:"confirmation_delay"`
}

type ActionConfig struct {
	DryRun bool `mapstructure:"dry_run"`
}

type SnapshotConfig struct {
	StorageDir string `mapstructure:"storage_dir"`
}

type RecordConfig struct {
	MaxDuration       time.Duration `mapstructure:"max_duration"`
	JobRetention      time.Duration `mapstructure:"job_retention"`
	MaxConcurrentJobs int           `mapstructure:"max_concurrent_jobs"`
	AllowedIPs        []string      `mapstructure:"allowed_ips"`
}

type MinIOConfig struct {
	Endpoint  string `mapstructure:"endpoint"`
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
	UseSSL    bool   `mapstructure:"use_ssl"`
}

type MongoDBConfig struct {
	URI        string `mapstructure:"uri"`
	Database   string `mapstructure:"database"`
	Collection string `mapstructure:"collection"`
}

type RedisConfig struct {
	Addr            string        `mapstructure:"addr"`
	Password        string        `mapstructure:"password"`
	DB              int           `mapstructure:"db"`
	PublishInterval time.Duration `mapstructure:"publish_interval"`
}

func Load(path string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetEnvPrefix("GO2RTC_MANAGER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.App.BoxIP == "" {
		return Config{}, fmt.Errorf("app.box_ip is required")
	}
	cfg.Log.Format = strings.ToLower(strings.TrimSpace(cfg.Log.Format))
	if cfg.Log.Format != "text" && cfg.Log.Format != "json" {
		return Config{}, fmt.Errorf("log.format must be text or json")
	}
	cfg.Log.FilePath = strings.TrimSpace(cfg.Log.FilePath)
	if cfg.Log.FilePath != "" {
		if cfg.Log.MaxSizeMB <= 0 {
			return Config{}, fmt.Errorf("log.max_size_mb must be greater than zero when log.file_path is set")
		}
		if cfg.Log.MaxBackups <= 0 {
			return Config{}, fmt.Errorf("log.max_backups must be greater than zero when log.file_path is set")
		}
		if cfg.Log.MaxAgeDays <= 0 {
			return Config{}, fmt.Errorf("log.max_age_days must be greater than zero when log.file_path is set")
		}
	}
	if cfg.HTTP.Addr == "" {
		return Config{}, fmt.Errorf("http.addr is required")
	}
	if cfg.HTTP.ReadTimeout <= 0 {
		return Config{}, fmt.Errorf("http.read_timeout must be greater than zero")
	}
	if cfg.HTTP.WriteTimeout <= 0 {
		return Config{}, fmt.Errorf("http.write_timeout must be greater than zero")
	}
	if cfg.HTTP.IdleTimeout <= 0 {
		return Config{}, fmt.Errorf("http.idle_timeout must be greater than zero")
	}
	if cfg.Go2RTC.BaseURL == "" {
		return Config{}, fmt.Errorf("go2rtc.base_url is required")
	}
	if cfg.Go2RTC.ConfigPath == "" {
		return Config{}, fmt.Errorf("go2rtc.config_path is required")
	}
	if len(cfg.Schedule.Crons) == 0 {
		return Config{}, fmt.Errorf("schedule.crons must contain at least one cron expression")
	}
	for i, spec := range cfg.Schedule.Crons {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			return Config{}, fmt.Errorf("schedule.crons[%d] is empty", i)
		}
		if _, err := cleanupCronParser.Parse(spec); err != nil {
			return Config{}, fmt.Errorf("schedule.crons[%d] is invalid: %w", i, err)
		}
		cfg.Schedule.Crons[i] = spec
	}
	if cfg.Schedule.ConfirmationDelay <= 0 {
		return Config{}, fmt.Errorf("schedule.confirmation_delay must be greater than zero")
	}
	if cfg.Snapshot.StorageDir == "" {
		return Config{}, fmt.Errorf("snapshot.storage_dir is required")
	}
	if cfg.Record.MaxDuration <= 0 {
		return Config{}, fmt.Errorf("record.max_duration must be greater than zero")
	}
	if cfg.Record.JobRetention <= 0 {
		return Config{}, fmt.Errorf("record.job_retention must be greater than zero")
	}
	if cfg.Record.MaxConcurrentJobs <= 0 {
		return Config{}, fmt.Errorf("record.max_concurrent_jobs must be greater than zero")
	}
	for i, value := range cfg.Record.AllowedIPs {
		allowedIP := strings.TrimSpace(value)
		if allowedIP == "" {
			return Config{}, fmt.Errorf("record.allowed_ips[%d] is empty", i)
		}
		if net.ParseIP(allowedIP) == nil {
			if _, _, err := net.ParseCIDR(allowedIP); err != nil {
				return Config{}, fmt.Errorf("record.allowed_ips[%d] must be a valid IP or CIDR", i)
			}
		}
		cfg.Record.AllowedIPs[i] = allowedIP
	}
	if cfg.MinIO.Endpoint == "" {
		return Config{}, fmt.Errorf("minio.endpoint is required")
	}
	if cfg.MinIO.AccessKey == "" {
		return Config{}, fmt.Errorf("minio.access_key is required")
	}
	if cfg.MinIO.SecretKey == "" {
		return Config{}, fmt.Errorf("minio.secret_key is required")
	}
	if cfg.MongoDB.URI == "" {
		return Config{}, fmt.Errorf("mongodb.uri is required")
	}
	if cfg.MongoDB.Database == "" {
		return Config{}, fmt.Errorf("mongodb.database is required")
	}
	if cfg.MongoDB.Collection == "" {
		return Config{}, fmt.Errorf("mongodb.collection is required")
	}
	if cfg.Redis.Addr != "" && cfg.Redis.PublishInterval <= 0 {
		return Config{}, fmt.Errorf("redis.publish_interval must be greater than zero when redis.addr is set")
	}

	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.name", "go2rtc-manager")
	v.SetDefault("app.env", "local")
	v.SetDefault("app.box_ip", "")
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("log.file_path", "")
	v.SetDefault("log.max_size_mb", 100)
	v.SetDefault("log.max_backups", 10)
	v.SetDefault("log.max_age_days", 30)
	v.SetDefault("log.compress", true)
	v.SetDefault("http.addr", ":7181")
	v.SetDefault("http.read_timeout", "5s")
	v.SetDefault("http.write_timeout", "15s")
	v.SetDefault("http.idle_timeout", "60s")
	v.SetDefault("go2rtc.base_url", "http://127.0.0.1:1984")
	v.SetDefault("go2rtc.username", "")
	v.SetDefault("go2rtc.password", "")
	v.SetDefault("go2rtc.config_path", "/config/go2rtc.yaml")
	v.SetDefault("go2rtc.request_timeout", "10s")
	v.SetDefault("go2rtc.backup_before_change", true)
	v.SetDefault("schedule.run_on_start", true)
	v.SetDefault("schedule.crons", []string{})
	v.SetDefault("schedule.confirmation_delay", "3m")
	v.SetDefault("action.dry_run", false)
	v.SetDefault("snapshot.storage_dir", "storage")
	v.SetDefault("record.max_duration", "1h")
	v.SetDefault("record.job_retention", "24h")
	v.SetDefault("record.max_concurrent_jobs", 3)
	v.SetDefault("record.allowed_ips", []string{})
	v.SetDefault("minio.endpoint", "")
	v.SetDefault("minio.access_key", "")
	v.SetDefault("minio.secret_key", "")
	v.SetDefault("minio.use_ssl", false)
	v.SetDefault("mongodb.uri", "")
	v.SetDefault("mongodb.database", "")
	v.SetDefault("mongodb.collection", "BODYCAM_INFO")
	v.SetDefault("redis.addr", "")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.publish_interval", "0s")
}
