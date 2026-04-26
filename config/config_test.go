package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAllowsRedisDisabledWithoutPublishInterval(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := strings.TrimSpace(`
app:
  box_ip: 192.168.0.10
http:
  addr: ":7181"
  read_timeout: 5s
  write_timeout: 5s
  idle_timeout: 30s
go2rtc:
  base_url: http://127.0.0.1:1984
  config_path: /config/go2rtc.yaml
schedule:
  crons:
    - "0 2 * * *"
    - "0 14 * * *"
  confirmation_delay: 1m
snapshot:
  storage_dir: storage
record:
  max_duration: 1h
  job_retention: 24h
  max_concurrent_jobs: 3
  allowed_ips:
    - 127.0.0.1
    - 10.0.0.0/24
minio:
  endpoint: localhost:9000
  access_key: minioadmin
  secret_key: minioadmin
mongodb:
  uri: mongodb://localhost:27017
  database: go2rtc_manager
  collection: BODYCAM_INFO
redis:
  addr: ""
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.App.BoxIP != "192.168.0.10" {
		t.Fatalf("unexpected app.box_ip: got %q want %q", cfg.App.BoxIP, "192.168.0.10")
	}
	if len(cfg.Schedule.Crons) != 2 {
		t.Fatalf("unexpected cron count: got %d want %d", len(cfg.Schedule.Crons), 2)
	}
	if len(cfg.Record.AllowedIPs) != 2 {
		t.Fatalf("unexpected allowed ip count: got %d want %d", len(cfg.Record.AllowedIPs), 2)
	}
}

func TestLoadRequiresAppBoxIP(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := strings.TrimSpace(`
http:
  addr: ":7181"
  read_timeout: 5s
  write_timeout: 5s
  idle_timeout: 30s
go2rtc:
  base_url: http://127.0.0.1:1984
  config_path: /config/go2rtc.yaml
schedule:
  crons:
    - "0 2 * * *"
  confirmation_delay: 1m
snapshot:
  storage_dir: storage
redis:
  addr: ""
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got, want := err.Error(), "app.box_ip is required"; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestLoadRequiresCleanupCrons(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := strings.TrimSpace(`
app:
  box_ip: 192.168.0.10
http:
  addr: ":7181"
  read_timeout: 5s
  write_timeout: 5s
  idle_timeout: 30s
go2rtc:
  base_url: http://127.0.0.1:1984
  config_path: /config/go2rtc.yaml
schedule:
  confirmation_delay: 1m
snapshot:
  storage_dir: storage
redis:
  addr: ""
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got, want := err.Error(), "schedule.crons must contain at least one cron expression"; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestLoadRejectsInvalidCleanupCron(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := strings.TrimSpace(`
app:
  box_ip: 192.168.0.10
http:
  addr: ":7181"
  read_timeout: 5s
  write_timeout: 5s
  idle_timeout: 30s
go2rtc:
  base_url: http://127.0.0.1:1984
  config_path: /config/go2rtc.yaml
schedule:
  crons:
    - "not a cron"
  confirmation_delay: 1m
snapshot:
  storage_dir: storage
redis:
  addr: ""
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "schedule.crons[0] is invalid") {
		t.Fatalf("unexpected error: got %q", err.Error())
	}
}

func TestLoadRequiresPublishIntervalWhenRedisEnabled(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := strings.TrimSpace(`
app:
  box_ip: 192.168.0.10
http:
  addr: ":7181"
  read_timeout: 5s
  write_timeout: 5s
  idle_timeout: 30s
go2rtc:
  base_url: http://127.0.0.1:1984
  config_path: /config/go2rtc.yaml
schedule:
  crons:
    - "0 2 * * *"
  confirmation_delay: 1m
snapshot:
  storage_dir: storage
record:
  max_duration: 1h
  job_retention: 24h
  max_concurrent_jobs: 3
minio:
  endpoint: localhost:9000
  access_key: minioadmin
  secret_key: minioadmin
mongodb:
  uri: mongodb://localhost:27017
  database: go2rtc_manager
  collection: BODYCAM_INFO
redis:
  addr: 127.0.0.1:6379
  publish_interval: 0s
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got, want := err.Error(), "redis.publish_interval must be greater than zero when redis.addr is set"; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestLoadRequiresRecordMaxDuration(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := strings.TrimSpace(`
app:
  box_ip: 192.168.0.10
http:
  addr: ":7181"
  read_timeout: 5s
  write_timeout: 5s
  idle_timeout: 30s
go2rtc:
  base_url: http://127.0.0.1:1984
  config_path: /config/go2rtc.yaml
schedule:
  crons:
    - "0 2 * * *"
  confirmation_delay: 1m
snapshot:
  storage_dir: storage
record:
  max_duration: 0s
  job_retention: 24h
  max_concurrent_jobs: 3
minio:
  endpoint: localhost:9000
  access_key: minioadmin
  secret_key: minioadmin
mongodb:
  uri: mongodb://localhost:27017
  database: go2rtc_manager
  collection: BODYCAM_INFO
redis:
  addr: ""
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got, want := err.Error(), "record.max_duration must be greater than zero"; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestLoadRequiresMinIOAndMongoDBConfig(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := strings.TrimSpace(`
app:
  box_ip: 192.168.0.10
http:
  addr: ":7181"
  read_timeout: 5s
  write_timeout: 5s
  idle_timeout: 30s
go2rtc:
  base_url: http://127.0.0.1:1984
  config_path: /config/go2rtc.yaml
schedule:
  crons:
    - "0 2 * * *"
  confirmation_delay: 1m
snapshot:
  storage_dir: storage
record:
  max_duration: 1h
  job_retention: 24h
  max_concurrent_jobs: 3
redis:
  addr: ""
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got, want := err.Error(), "minio.endpoint is required"; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestLoadRequiresRecordMaxConcurrentJobs(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := strings.TrimSpace(`
app:
  box_ip: 192.168.0.10
http:
  addr: ":7181"
  read_timeout: 5s
  write_timeout: 5s
  idle_timeout: 30s
go2rtc:
  base_url: http://127.0.0.1:1984
  config_path: /config/go2rtc.yaml
schedule:
  crons:
    - "0 2 * * *"
  confirmation_delay: 1m
snapshot:
  storage_dir: storage
record:
  max_duration: 1h
  job_retention: 24h
  max_concurrent_jobs: 0
minio:
  endpoint: localhost:9000
  access_key: minioadmin
  secret_key: minioadmin
mongodb:
  uri: mongodb://localhost:27017
  database: go2rtc_manager
  collection: BODYCAM_INFO
redis:
  addr: ""
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got, want := err.Error(), "record.max_concurrent_jobs must be greater than zero"; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestLoadRejectsInvalidRecordAllowedIP(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := strings.TrimSpace(`
app:
  box_ip: 192.168.0.10
http:
  addr: ":7181"
  read_timeout: 5s
  write_timeout: 5s
  idle_timeout: 30s
go2rtc:
  base_url: http://127.0.0.1:1984
  config_path: /config/go2rtc.yaml
schedule:
  crons:
    - "0 2 * * *"
  confirmation_delay: 1m
snapshot:
  storage_dir: storage
record:
  max_duration: 1h
  job_retention: 24h
  max_concurrent_jobs: 3
  allowed_ips:
    - not-an-ip
minio:
  endpoint: localhost:9000
  access_key: minioadmin
  secret_key: minioadmin
mongodb:
  uri: mongodb://localhost:27017
  database: go2rtc_manager
  collection: BODYCAM_INFO
redis:
  addr: ""
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got, want := err.Error(), "record.allowed_ips[0] must be a valid IP or CIDR"; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestLoadAllowsEmptyRecordAllowedIPs(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := strings.TrimSpace(`
app:
  box_ip: 192.168.0.10
http:
  addr: ":7181"
  read_timeout: 5s
  write_timeout: 5s
  idle_timeout: 30s
go2rtc:
  base_url: http://127.0.0.1:1984
  config_path: /config/go2rtc.yaml
schedule:
  crons:
    - "0 2 * * *"
  confirmation_delay: 1m
snapshot:
  storage_dir: storage
record:
  max_duration: 1h
  job_retention: 24h
  max_concurrent_jobs: 3
  allowed_ips: []
minio:
  endpoint: localhost:9000
  access_key: minioadmin
  secret_key: minioadmin
mongodb:
  uri: mongodb://localhost:27017
  database: go2rtc_manager
  collection: BODYCAM_INFO
redis:
  addr: ""
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(cfg.Record.AllowedIPs) != 0 {
		t.Fatalf("unexpected allowed IPs: got %v", cfg.Record.AllowedIPs)
	}
}
