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
		{name: "all empty", inspections: []BaselineInspection{{State: BaselineStateEmpty}, {State: BaselineStateEmpty}}, want: BaselineStateEmpty},
		{name: "all complete", inspections: []BaselineInspection{{State: BaselineStateComplete}, {State: BaselineStateComplete}}, want: BaselineStateComplete},
		{name: "mixed", inspections: []BaselineInspection{{State: BaselineStateEmpty}, {State: BaselineStateComplete}}, want: BaselineStateIncompatible},
		{name: "one incompatible", inspections: []BaselineInspection{{State: BaselineStateComplete}, {State: BaselineStateIncompatible}}, want: BaselineStateIncompatible},
		{name: "none", want: BaselineStateIncompatible},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CombineBaselineInspections(tc.inspections)
			assert.Equal(t, tc.want, got.State)
			assert.Equal(t, tc.inspections, got.Databases)
		})
	}
}
