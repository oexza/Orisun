package logging

import (
	"fmt"
	c "github.com/oexza/Orisun/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
)

// This will be set at build time
var isDevelopment = true

var appLogger Logger

type Logger interface {
	Debug(args ...any)
	Debugf(format string, args ...any)
	Info(args ...any)
	Infof(format string, args ...any)
	Warn(args ...any)
	Warnf(format string, args ...any)
	Error(args ...any)
	Errorf(format string, args ...any)
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

func ZapLogger(level string) (Logger, error) {
	if appLogger != nil {
		return appLogger, nil
	}

	var cfg = zap.NewProductionConfig()

	// Configure for colored console output
	cfg.Encoding = "console"
	cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	cfg.EncoderConfig.TimeKey = "time"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// Disable caller info in production builds
	if !isDevelopment {
		cfg.DisableCaller = true
	}

	lvl, err := zapcore.ParseLevel(level)
	if err != nil {
		return nil, err
	}
	cfg.Level.SetLevel(lvl)

	logger, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	appLogger = logger.Sugar()
	return appLogger, nil
}

func InitializeDefaultLogger(config c.LoggingConfig) Logger {
	// Initialize logger
	logr, err := ZapLogger(config.Level)
	if err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	return logr
}

const (
	LevelDebug = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal // Adding Fatal level
)
