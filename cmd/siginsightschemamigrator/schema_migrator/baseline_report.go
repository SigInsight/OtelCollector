package schemamigrator

import "context"

// BaselineReport combines the v1 baseline history state for all telemetry databases.
type BaselineReport struct {
	State     BaselineState
	Databases []BaselineInspection
}

// InspectV1BaselineState inspects and combines all v1 telemetry database histories.
func (m *MigrationManager) InspectV1BaselineState(ctx context.Context) (BaselineReport, error) {
	inspections := make([]BaselineInspection, 0, len(Databases))
	for _, spec := range V1BaselineSpecs() {
		inspection, err := m.InspectBaselineState(ctx, spec)
		if err != nil {
			return BaselineReport{}, err
		}
		inspections = append(inspections, inspection)
	}
	return CombineBaselineInspections(inspections), nil
}

// CombineBaselineInspections computes the overall state without querying ClickHouse.
func CombineBaselineInspections(inspections []BaselineInspection) BaselineReport {
	report := BaselineReport{
		State:     BaselineStatePartial,
		Databases: inspections,
	}
	if len(inspections) == 0 {
		return report
	}

	allEmpty := true
	allComplete := true
	anyLegacy := false
	for _, inspection := range inspections {
		if inspection.State == BaselineStateAhead {
			report.State = BaselineStateAhead
			return report
		}
		if inspection.State != BaselineStateEmpty {
			allEmpty = false
		}
		if inspection.State != BaselineStateCompleteFresh &&
			inspection.State != BaselineStateCompleteLegacy {
			allComplete = false
		}
		if inspection.State == BaselineStateCompleteLegacy {
			anyLegacy = true
		}
	}

	if allEmpty {
		report.State = BaselineStateEmpty
		return report
	}
	if !allComplete {
		return report
	}
	if anyLegacy {
		report.State = BaselineStateCompleteLegacy
	} else {
		report.State = BaselineStateCompleteFresh
	}
	return report
}
