package logging

import (
	"log"
	"strings"
	"sync/atomic"
)

type Level int32

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var current atomic.Int32

func init() {
	SetLevel(LevelInfo)
}

func SetLevel(l Level) {
	current.Store(int32(l))
}

func SetLevelFromString(value string) Level {
	level := LevelInfo
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		level = LevelDebug
	case "info":
		level = LevelInfo
	case "warn", "warning":
		level = LevelWarn
	case "error":
		level = LevelError
	default:
		if value != "" {
			log.Printf("[WARN] unknown log level '%s', defaulting to info", value)
		}
	}
	SetLevel(level)
	return level
}

func effectiveLevel() Level {
	return Level(current.Load())
}

func CurrentLevel() Level {
	return effectiveLevel()
}

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "info"
	}
}

func Debugf(format string, args ...any) {
	logWithLevel(LevelDebug, "DEBUG", format, args...)
}

func Infof(format string, args ...any) {
	logWithLevel(LevelInfo, "INFO", format, args...)
}

func Warnf(format string, args ...any) {
	logWithLevel(LevelWarn, "WARN", format, args...)
}

func Errorf(format string, args ...any) {
	logWithLevel(LevelError, "ERROR", format, args...)
}

func logWithLevel(level Level, label string, format string, args ...any) {
	if level < effectiveLevel() {
		return
	}
	log.Printf("["+label+"] "+format, args...)
}
