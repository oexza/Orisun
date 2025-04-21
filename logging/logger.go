package logging

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// This will be set at build time
var isDevelopment = true

var appLogger Logger

type Logger interface {
	Debug(args ...interface{})
	Debugf(format string, args ...interface{})
	Info(args ...interface{})
	Infof(format string, args ...interface{})
	Warn(args ...interface{})
	Warnf(format string, args ...interface{})
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
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

const (
	LevelDebug = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal // Adding Fatal level
)
