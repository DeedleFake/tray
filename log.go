package tray

import (
	"log/slog"
	"os"
)

var (
	logEnabled = os.Getenv("TRAY_DEBUG") == "1"
	logger     = slog.With("source", "tray debug")
)

func log(msg string, args ...any) {
	if logEnabled {
		logger.Info(msg, args...)
	}
}

func logErr(msg string, args ...any) {
	if logEnabled {
		logger.Error(msg, args...)
	}
}
