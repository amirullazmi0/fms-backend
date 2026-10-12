package logger

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *zap.Logger

type LoggerConfig struct {
	Debug       bool
	Format      string
	LogToFile   bool
	LogFilePath string
	MaxSize     int // megabytes
	MaxBackups  int
	MaxAge      int // days
	Compress    bool
}

const (
	colorReset   = "\x1b[0m"
	colorRed     = "\x1b[31m"
	colorGreen   = "\x1b[32m"
	colorYellow  = "\x1b[33m"
	colorMagenta = "\x1b[35m"
	colorGray    = "\x1b[90m"
)

func levelColor(level zapcore.Level) string {
	switch level {
	case zapcore.DebugLevel:
		return colorGray
	case zapcore.InfoLevel:
		return colorGreen
	case zapcore.WarnLevel:
		return colorYellow
	case zapcore.ErrorLevel:
		return colorRed
	case zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		return colorMagenta
	default:
		return colorReset
	}
}

func colorLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(levelColor(level) + level.CapitalString() + colorReset)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}

// InitLogger initializes the global logger with Grafana Loki compatible format
func InitLogger(cfg LoggerConfig) error {
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Set log level
	var level zapcore.Level
	if cfg.Debug {
		level = zapcore.DebugLevel
	} else {
		level = zapcore.InfoLevel
	}

	// Create cores for different outputs
	var cores []zapcore.Core

	// Console output. Auto-detect unless LOG_FORMAT forces one, and only emit
	// colour on a real terminal.
	useConsole := isTerminal(os.Stdout)
	switch strings.ToLower(strings.TrimSpace(cfg.Format)) {
	case "console", "text", "pretty":
		useConsole = true
	case "json":
		useConsole = false
	}

	var consoleEncoder zapcore.Encoder
	if useConsole {
		consoleConfig := encoderConfig
		if isTerminal(os.Stdout) {
			consoleConfig.EncodeLevel = colorLevelEncoder
		} else {
			consoleConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		}
		consoleEncoder = zapcore.NewConsoleEncoder(consoleConfig)
	} else {
		// Use JSON encoder so Promtail/Loki can parse it
		consoleConfig := encoderConfig
		consoleConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
		consoleEncoder = zapcore.NewJSONEncoder(consoleConfig)
	}
	consoleCore := zapcore.NewCore(
		consoleEncoder,
		zapcore.AddSync(os.Stdout),
		level,
	)
	cores = append(cores, consoleCore)

	// File output (always JSON for Loki/Promtail)
	if cfg.LogToFile {
		fileEncoder := zapcore.NewJSONEncoder(encoderConfig)

		// Create logs directory if not exists
		if err := os.MkdirAll("logs", 0755); err != nil {
			return err
		}

		logFile, err := os.OpenFile(cfg.LogFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}

		fileCore := zapcore.NewCore(
			fileEncoder,
			zapcore.AddSync(logFile),
			level,
		)
		cores = append(cores, fileCore)
	}

	// Combine all cores
	core := zapcore.NewTee(cores...)

	// Create logger with caller info
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	Log = logger
	return nil
}

// Info logs an info message
func Info(msg string, fields ...zap.Field) {
	if Log != nil {
		Log.Info(msg, fields...)
	}
}

// Error logs an error message
func Error(msg string, fields ...zap.Field) {
	if Log != nil {
		Log.Error(msg, fields...)
	}
}

// Debug logs a debug message
func Debug(msg string, fields ...zap.Field) {
	if Log != nil {
		Log.Debug(msg, fields...)
	}
}

// Warn logs a warning message
func Warn(msg string, fields ...zap.Field) {
	if Log != nil {
		Log.Warn(msg, fields...)
	}
}

// Fatal logs a fatal message and exits
func Fatal(msg string, fields ...zap.Field) {
	if Log != nil {
		Log.Fatal(msg, fields...)
	} else {
		os.Exit(1)
	}
}

// Sync flushes any buffered log entries
func Sync() {
	if Log != nil {
		_ = Log.Sync()
	}
}
