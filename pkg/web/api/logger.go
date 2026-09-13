package api

import "log/slog"

func logger() *slog.Logger { return slog.Default() }
