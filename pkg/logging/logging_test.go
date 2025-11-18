package logging

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	buf := &bytes.Buffer{}
	orig := log.Writer()
	log.SetOutput(buf)
	defer log.SetOutput(orig)
	fn()
	return buf.String()
}

func TestSetLevelFromString(t *testing.T) {
	SetLevel(LevelInfo)
	level := SetLevelFromString("debug")
	if level != LevelDebug || CurrentLevel() != LevelDebug {
		 t.Fatalf("expected debug level, got %v", level)
	}
	msg := captureLog(t, func() {
		SetLevelFromString("unknown")
	})
	if !strings.Contains(msg, "unknown log level") {
		 t.Fatalf("expected warning log for unknown level, got %s", msg)
	}
}

func TestLogFiltering(t *testing.T) {
	SetLevel(LevelWarn)
	msg := captureLog(t, func() {
		Infof("should not appear")
		Errorf("should appear")
	})
	if strings.Contains(msg, "should not appear") {
		 t.Fatalf("info log should be filtered: %s", msg)
	}
	if !strings.Contains(msg, "should appear") {
		 t.Fatalf("error log missing: %s", msg)
	}
}
