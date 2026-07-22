package migrate

import (
	"context"
	"errors"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// ErrLogsJSONUnsupported indicates that ClickHouse lacks the required JSON serialization settings.
var ErrLogsJSONUnsupported = errors.New("logs JSON schema is not supported by ClickHouse")

const logsJSONSettingsQuery = `SELECT count()
FROM system.merge_tree_settings
WHERE name IN ('object_serialization_version', 'object_shared_data_serialization_version')`

func ensureLogsJSONSupport(ctx context.Context, conn driver.Conn) error {
	var count uint64
	if err := conn.QueryRow(ctx, logsJSONSettingsQuery).Scan(&count); err != nil {
		return fmt.Errorf("check ClickHouse logs JSON capabilities: %w", err)
	}

	if count != 2 {
		return fmt.Errorf(
			"%w: required MergeTree settings object_serialization_version and object_shared_data_serialization_version are unavailable",
			ErrLogsJSONUnsupported,
		)
	}

	return nil
}
