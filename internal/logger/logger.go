package logger

import (
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/spf13/viper"
)

type Logger interface {
	Info(msg string, args ...any)
	Debug(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	Fatal(msg string, args ...any)
}

type SlogLogger struct {
	*slog.Logger
}

// We need to first initialize the logger with a default log level, then reinitalize once viper has it's configuration value
var RawLogger = InitLogger("info")

func ReinitLogger() {
	RawLogger = InitLogger(getLogLevel())
}

func InitLogger(logLevelString string) *slog.Logger {
	var logLevel slog.Level
	var addSource bool
	switch logLevelString {
	case "debug":
		logLevel = slog.LevelDebug
		addSource = true
	case "info":
		logLevel = slog.LevelInfo
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		log.Fatalln("Invalid log level:", logLevelString)
	}

	// Create JSON handler with options
	opts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: addSource,
	}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	logger := slog.New(handler)

	return logger
}

// Info wraps slog.Logger.Info()
func Info(args ...interface{}) {
	RawLogger.Info(fmt.Sprint(args...))
}

// Debug wraps slog.Logger.Debug()
func Debug(args ...interface{}) {
	RawLogger.Debug(fmt.Sprint(args...))
}

// Warn wraps slog.Logger.Warn()
func Warn(args ...interface{}) {
	RawLogger.Warn(fmt.Sprint(args...))
}

// Error wraps slog.Logger.Error()
func Error(args ...interface{}) {
	RawLogger.Error(fmt.Sprint(args...))
}

// Fatal wraps slog.Logger.Error() and calls log.Fatal()
func Fatal(args ...interface{}) {
	msg := fmt.Sprint(args...)
	RawLogger.Error(msg)
	log.Fatal(msg)
}

// Infof wraps slog.Logger.Info() with formatted message
func Infof(template string, args ...interface{}) {
	RawLogger.Info(fmt.Sprintf(template, args...))
}

// Debugf wraps slog.Logger.Debug() with formatted message
func Debugf(template string, args ...interface{}) {
	RawLogger.Debug(fmt.Sprintf(template, args...))
}

// Warnf wraps slog.Logger.Warn() with formatted message
func Warnf(template string, args ...interface{}) {
	RawLogger.Warn(fmt.Sprintf(template, args...))
}

// Errorf wraps slog.Logger.Error() with formatted message
func Errorf(template string, args ...interface{}) {
	RawLogger.Error(fmt.Sprintf(template, args...))
}

// Fatalf wraps slog.Logger.Error() with formatted message and calls log.Fatal()
func Fatalf(template string, args ...interface{}) {
	msg := fmt.Sprintf(template, args...)
	RawLogger.Error(msg)
	log.Fatal(msg)
}

// getLogLevel returns the log level from viper
func getLogLevel() string {
	if viperLogLevel := viper.GetString("log_level"); viperLogLevel != "" {
		return viperLogLevel
	}
	return "info"
}
