package migrate

import "testing"

func TestRewriteLegacyStorageName(t *testing.T) {
	t.Parallel()

	legacyDDL := `CREATE MATERIALIZED VIEW signoz_metrics.metric_mv TO signoz_metrics.samples_v4 AS SELECT * FROM signoz_traces.signoz_index_v3 JOIN signoz_logs.logs_v2 ON 1`
	want := `CREATE MATERIALIZED VIEW siginsight_metrics.metric_mv TO siginsight_metrics.samples_v4 AS SELECT * FROM siginsight_traces.span_index_v3 JOIN siginsight_logs.logs_v2 ON 1`
	if got := rewriteLegacyStorageName(legacyDDL); got != want {
		t.Fatalf("rewriteLegacyStorageName() = %q, want %q", got, want)
	}
}

func TestRewriteLegacyStorageNameIsIdempotent(t *testing.T) {
	t.Parallel()

	currentDDL := `CREATE VIEW siginsight_traces.trace_view AS SELECT * FROM siginsight_traces.span_index_v3`
	if got := rewriteLegacyStorageName(currentDDL); got != currentDDL {
		t.Fatalf("rewriteLegacyStorageName() changed current DDL: %q", got)
	}
}
