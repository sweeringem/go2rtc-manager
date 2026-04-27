package logging

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/example/go2rtc-manager/config"
)

func New(cfg config.LogConfig) (*zap.Logger, error) {
	var logLevel zapcore.Level
	if err := logLevel.UnmarshalText([]byte(cfg.Level)); err != nil {
		logLevel = zapcore.InfoLevel
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	newEncoder := func() zapcore.Encoder {
		if cfg.Format == "text" {
			return zapcore.NewConsoleEncoder(encoderConfig)
		}
		return zapcore.NewJSONEncoder(encoderConfig)
	}

	cores := []zapcore.Core{
		zapcore.NewCore(newEncoder(), zapcore.AddSync(os.Stdout), logLevel),
	}

	if cfg.FilePath != "" {
		dir := filepath.Dir(cfg.FilePath)
		if dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create log dir: %w", err)
			}
		}
		cores = append(cores, zapcore.NewCore(newEncoder(), zapcore.AddSync(&lumberjack.Logger{
			Filename:   cfg.FilePath,
			MaxSize:    cfg.MaxSizeMB,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAgeDays,
			Compress:   cfg.Compress,
		}), logLevel))
	}

	return zap.New(zapcore.NewTee(cores...)), nil
}
