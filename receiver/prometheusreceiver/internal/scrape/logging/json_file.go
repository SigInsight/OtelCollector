package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
)

type JSONFileLogger struct {
	handler slog.Handler
	file    *os.File
}

var _ slog.Handler = (*JSONFileLogger)(nil)
var _ io.Closer = (*JSONFileLogger)(nil)

func NewJSONFileLogger(filename string) (*JSONFileLogger, error) {
	if filename == "" {
		return nil, nil
	}
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return nil, fmt.Errorf("create JSON scrape failure log: %w", err)
	}
	return &JSONFileLogger{
		handler: slog.NewJSONHandler(file, nil),
		file:    file,
	}, nil
}

func (logger *JSONFileLogger) Close() error {
	return logger.file.Close()
}

func (logger *JSONFileLogger) Enabled(ctx context.Context, level slog.Level) bool {
	return logger.handler.Enabled(ctx, level)
}

func (logger *JSONFileLogger) Handle(ctx context.Context, record slog.Record) error {
	return logger.handler.Handle(ctx, record.Clone())
}

func (logger *JSONFileLogger) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return logger
	}
	return &JSONFileLogger{handler: logger.handler.WithAttrs(attrs), file: logger.file}
}

func (logger *JSONFileLogger) WithGroup(name string) slog.Handler {
	if name == "" {
		return logger
	}
	return &JSONFileLogger{handler: logger.handler.WithGroup(name), file: logger.file}
}
