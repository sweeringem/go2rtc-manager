package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	App      AppConfig      `mapstructure:"app"`
	Log      LogConfig      `mapstructure:"log"`
	Go2RTC   Go2RTCConfig   `mapstructure:"go2rtc"`
	Schedule ScheduleConfig `mapstructure:"schedule"`
	Action   ActionConfig   `mapstructure:"action"`
	Redis    RedisConfig    `mapstructure:"redis"`
}

type AppConfig struct {
	Name string `mapstructure:"name"`
	Env  string `mapstructure:"env"`
}

type LogConfig struct {
	Level string `mapstructure:"level"`
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
	Interval          time.Duration `mapstructure:"interval"`
	ConfirmationDelay time.Duration `mapstructure:"confirmation_delay"`
}

type ActionConfig struct {
	DryRun bool `mapstructure:"dry_run"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	RouterIP string `mapstructure:"router_ip"`
}

func Load(path string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetEnvPrefix("GO2RTC_CLEANER")
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

	if cfg.Go2RTC.BaseURL == "" {
		return Config{}, fmt.Errorf("go2rtc.base_url is required")
	}
	if cfg.Go2RTC.ConfigPath == "" {
		return Config{}, fmt.Errorf("go2rtc.config_path is required")
	}
	if cfg.Schedule.Interval <= 0 {
		return Config{}, fmt.Errorf("schedule.interval must be greater than zero")
	}
	if cfg.Schedule.ConfirmationDelay <= 0 {
		return Config{}, fmt.Errorf("schedule.confirmation_delay must be greater than zero")
	}
	if cfg.Redis.Addr != "" && cfg.Redis.RouterIP == "" {
		return Config{}, fmt.Errorf("redis.router_ip is required when redis.addr is set")
	}

	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.name", "go2rtc-stream-cleaner")
	v.SetDefault("app.env", "local")
	v.SetDefault("log.level", "info")
	v.SetDefault("go2rtc.base_url", "http://127.0.0.1:1984")
	v.SetDefault("go2rtc.username", "")
	v.SetDefault("go2rtc.password", "")
	v.SetDefault("go2rtc.config_path", "/config/go2rtc.yaml")
	v.SetDefault("go2rtc.request_timeout", "10s")
	v.SetDefault("go2rtc.backup_before_change", true)
	v.SetDefault("schedule.run_on_start", true)
	v.SetDefault("schedule.interval", "3h")
	v.SetDefault("schedule.confirmation_delay", "3m")
	v.SetDefault("action.dry_run", false)
	v.SetDefault("redis.addr", "")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.router_ip", "")
}
