package schemamigrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCombineBaselineInspections(t *testing.T) {
	tests := []struct {
		name        string
		inspections []BaselineInspection
		want        BaselineState
	}{
		{
			name: "all empty",
			inspections: []BaselineInspection{
				{Database: SigInsightLogsDB, State: BaselineStateEmpty},
				{Database: SigInsightMetricsDB, State: BaselineStateEmpty},
			},
			want: BaselineStateEmpty,
		},
		{
			name: "all fresh",
			inspections: []BaselineInspection{
				{Database: SigInsightLogsDB, State: BaselineStateCompleteFresh},
				{Database: SigInsightMetricsDB, State: BaselineStateCompleteFresh},
			},
			want: BaselineStateCompleteFresh,
		},
		{
			name: "complete with legacy history",
			inspections: []BaselineInspection{
				{Database: SigInsightLogsDB, State: BaselineStateCompleteLegacy},
				{Database: SigInsightMetricsDB, State: BaselineStateCompleteFresh},
			},
			want: BaselineStateCompleteLegacy,
		},
		{
			name: "mixed empty and complete",
			inspections: []BaselineInspection{
				{Database: SigInsightLogsDB, State: BaselineStateEmpty},
				{Database: SigInsightMetricsDB, State: BaselineStateCompleteFresh},
			},
			want: BaselineStatePartial,
		},
		{
			name: "partial",
			inspections: []BaselineInspection{
				{Database: SigInsightLogsDB, State: BaselineStateCompleteFresh},
				{Database: SigInsightMetricsDB, State: BaselineStatePartial},
			},
			want: BaselineStatePartial,
		},
		{
			name: "ahead takes precedence",
			inspections: []BaselineInspection{
				{Database: SigInsightLogsDB, State: BaselineStatePartial},
				{Database: SigInsightMetricsDB, State: BaselineStateAhead},
			},
			want: BaselineStateAhead,
		},
		{
			name: "no inspections",
			want: BaselineStatePartial,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CombineBaselineInspections(tc.inspections)
			assert.Equal(t, tc.want, got.State)
			assert.Equal(t, tc.inspections, got.Databases)
		})
	}
}
