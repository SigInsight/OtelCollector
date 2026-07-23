package schemamigrator

import (
	"fmt"
	"strings"
)

// TableEngine represents the engine of the table.
type TableEngine interface {
	ToSQL() string
	EngineType() string
}

// TableSetting represents the setting of the table.
type TableSetting struct {
	Name  string
	Value string
}

func (t TableSetting) ToSQL() string {
	return fmt.Sprintf("%s = %s", t.Name, t.Value)
}

type TableSettings []TableSetting

func (t TableSettings) ToSQL() string {
	parts := make([]string, len(t))
	for i, setting := range t {
		parts[i] = setting.ToSQL()
	}
	return strings.Join(parts, ", ")
}

// MergeTree represents the MergeTree engine of the table.
type MergeTree struct {
	OrderBy     string
	PrimaryKey  string
	PartitionBy string
	SampleBy    string
	TTL         string
	Settings    TableSettings
}

func (m MergeTree) EngineParams() string {
	var sql strings.Builder
	if m.PrimaryKey != "" {
		sql.WriteString(" PRIMARY KEY ")
		sql.WriteString(m.PrimaryKey)
	}
	if m.OrderBy != "" {
		sql.WriteString(" ORDER BY ")
		sql.WriteString(m.OrderBy)
	}
	if m.PartitionBy != "" {
		sql.WriteString(" PARTITION BY ")
		sql.WriteString(m.PartitionBy)
	}
	if m.SampleBy != "" {
		sql.WriteString(" SAMPLE BY ")
		sql.WriteString(m.SampleBy)
	}
	if m.TTL != "" {
		sql.WriteString(" TTL ")
		sql.WriteString(m.TTL)
	}
	if len(m.Settings) > 0 {
		sql.WriteString(" SETTINGS ")
		sql.WriteString(m.Settings.ToSQL())
	}
	return sql.String()
}

func (m MergeTree) ToSQL() string {
	var sql strings.Builder
	sql.WriteString(m.EngineType())
	sql.WriteString(m.EngineParams())
	return sql.String()
}

func (m MergeTree) EngineType() string { return "MergeTree" }

// Replacing represents the ReplacingMergeTree engine of the table.
type ReplacingMergeTree struct {
	MergeTree
}

func (r ReplacingMergeTree) EngineType() string { return "ReplacingMergeTree" }

func (r ReplacingMergeTree) ToSQL() string {
	var sql strings.Builder
	sql.WriteString(r.EngineType())
	sql.WriteString(r.EngineParams())
	return sql.String()
}

// AggregatingMergeTree represents the AggregatingMergeTree engine of the table.
type AggregatingMergeTree struct {
	MergeTree
}

func (a AggregatingMergeTree) EngineType() string { return "AggregatingMergeTree" }

func (a AggregatingMergeTree) ToSQL() string {
	var sql strings.Builder
	sql.WriteString(a.EngineType())
	sql.WriteString(a.EngineParams())
	return sql.String()
}

type SummingMergeTree struct {
	MergeTree
}

func (s SummingMergeTree) EngineType() string { return "SummingMergeTree" }

func (s SummingMergeTree) ToSQL() string {
	var sql strings.Builder
	sql.WriteString(s.EngineType())
	sql.WriteString(s.EngineParams())
	return sql.String()
}
