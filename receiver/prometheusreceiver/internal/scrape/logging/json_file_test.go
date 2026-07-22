package logging

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestJSONFileLogger(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "scrape-failures.json")
	logger, err := NewJSONFileLogger(filename)
	require.NoError(t, err)

	record := slog.NewRecord(time.Unix(123, 0), slog.LevelError, "scrape failed", 0)
	record.AddAttrs(slog.String("target", "localhost:9090"))
	require.NoError(t, logger.Handle(context.Background(), record))
	require.NoError(t, logger.Close())

	content, err := os.ReadFile(filename)
	require.NoError(t, err)
	entry := map[string]any{}
	require.NoError(t, json.Unmarshal(content, &entry))
	require.Equal(t, "ERROR", entry["level"])
	require.Equal(t, "scrape failed", entry["msg"])
	require.Equal(t, "localhost:9090", entry["target"])
}
