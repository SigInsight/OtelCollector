package migrate

import (
	"context"
	"testing"

	mockhouse "github.com/srikanthccv/ClickHouse-go-mock"
	"github.com/stretchr/testify/require"
)

func TestEnsureLogsJSONSupport(t *testing.T) {
	tests := []struct {
		name      string
		count     uint64
		wantError bool
	}{
		{
			name:  "supported",
			count: 2,
		},
		{
			name:      "unsupported",
			count:     0,
			wantError: true,
		},
		{
			name:      "partially supported",
			count:     1,
			wantError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := mockhouse.NewClickHouseNative(nil)
			require.NoError(t, err)

			row := mockhouse.NewRow(
				[]mockhouse.ColumnType{{Name: "count()", Type: "UInt64"}},
				[]any{tc.count},
			)
			conn.ExpectQueryRow(logsJSONSettingsQuery).WillReturnRow(row)

			err = ensureLogsJSONSupport(context.Background(), conn)
			if tc.wantError {
				require.ErrorIs(t, err, ErrLogsJSONUnsupported)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, conn.ExpectationsWereMet())
		})
	}
}
