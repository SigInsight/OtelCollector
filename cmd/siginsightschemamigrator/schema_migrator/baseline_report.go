package schemamigrator

import "context"

type BaselineReport struct {
	State     BaselineState
	Databases []BaselineInspection
}

func (m *MigrationManager) InspectCanonicalBaselineState(ctx context.Context) (BaselineReport, error) {
	inspections := make([]BaselineInspection, 0, len(Databases))
	for _, spec := range CanonicalBaselineSpecs() {
		inspection, err := m.InspectBaselineState(ctx, spec)
		if err != nil {
			return BaselineReport{}, err
		}
		inspections = append(inspections, inspection)
	}
	return CombineBaselineInspections(inspections), nil
}

func CombineBaselineInspections(inspections []BaselineInspection) BaselineReport {
	report := BaselineReport{State: BaselineStateIncompatible, Databases: inspections}
	if len(inspections) == 0 {
		return report
	}

	allEmpty := true
	allComplete := true
	for _, inspection := range inspections {
		allEmpty = allEmpty && inspection.State == BaselineStateEmpty
		allComplete = allComplete && inspection.State == BaselineStateComplete
	}
	if allEmpty {
		report.State = BaselineStateEmpty
	} else if allComplete {
		report.State = BaselineStateComplete
	}
	return report
}
