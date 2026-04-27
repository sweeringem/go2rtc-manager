package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/go2rtc-manager/config"
)

func TestNewWritesToConfiguredFile(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "logs", "go2rtc-manager.log")
	logger, err := New(config.LogConfig{
		Level:      "info",
		Format:     "json",
		FilePath:   logPath,
		MaxSizeMB:  100,
		MaxBackups: 10,
		MaxAgeDays: 30,
		Compress:   true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	logger.Info("test log")
	_ = logger.Sync()

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(content), "test log") {
		t.Fatalf("expected log file to contain message, got %q", string(content))
	}
}
