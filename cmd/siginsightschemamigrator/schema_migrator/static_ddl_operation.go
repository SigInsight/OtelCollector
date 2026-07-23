package schemamigrator

import "strings"

const (
	staticDDLOnClusterToken = "{{ON_CLUSTER}}"
	staticDDLClusterToken   = "{{CLUSTER}}"
)

// StaticDDLOperation creates one object from the consolidated v1 schema.
// Query contains explicit tokens for the ON CLUSTER clause and Distributed
// engine cluster so the exported DDL remains independent of deployment config.
type StaticDDLOperation struct {
	Database string
	Table    string
	Query    string

	cluster    string
	replicated bool
}

func (s StaticDDLOperation) OnCluster(cluster string) Operation {
	s.cluster = cluster
	return &s
}

func (s StaticDDLOperation) WithReplication() Operation {
	s.replicated = true
	return &s
}

func (s StaticDDLOperation) ShouldWaitForDistributionQueue() (bool, string, string) {
	return false, s.Database, s.Table
}

func (s StaticDDLOperation) IsMutation() bool    { return false }
func (s StaticDDLOperation) IsIdempotent() bool  { return true }
func (s StaticDDLOperation) IsLightweight() bool { return true }
func (s StaticDDLOperation) ForceMigrate() bool  { return false }

func (s StaticDDLOperation) ToSQL() string {
	onCluster := ""
	if s.cluster != "" {
		onCluster = " ON CLUSTER " + s.cluster
	}

	query := strings.ReplaceAll(s.Query, staticDDLOnClusterToken, onCluster)
	query = strings.ReplaceAll(query, staticDDLClusterToken, s.cluster)
	if !s.replicated {
		return query
	}

	replacements := []struct {
		from string
		to   string
	}{
		{"ENGINE = ReplacingMergeTree", "ENGINE = ReplicatedReplacingMergeTree"},
		{"ENGINE = AggregatingMergeTree", "ENGINE = ReplicatedAggregatingMergeTree"},
		{"ENGINE = SummingMergeTree", "ENGINE = ReplicatedSummingMergeTree"},
		{"ENGINE = MergeTree", "ENGINE = ReplicatedMergeTree"},
	}
	for _, replacement := range replacements {
		query = strings.Replace(query, replacement.from, replacement.to, 1)
	}
	return query
}
