// Register copies of stanza operators dedicated to signoz logs pipelines
package signozlogspipelinestanzaadapter

import (
	_ "github.com/SigInsight/OtelCollector/pkg/parser/grok"
	_ "github.com/SigInsight/OtelCollector/processor/signozlogspipelineprocessor/stanza/operator/operators/add"
	_ "github.com/SigInsight/OtelCollector/processor/signozlogspipelineprocessor/stanza/operator/operators/copy"
	_ "github.com/SigInsight/OtelCollector/processor/signozlogspipelineprocessor/stanza/operator/operators/json"
	_ "github.com/SigInsight/OtelCollector/processor/signozlogspipelineprocessor/stanza/operator/operators/move"
	_ "github.com/SigInsight/OtelCollector/processor/signozlogspipelineprocessor/stanza/operator/operators/noop"
	_ "github.com/SigInsight/OtelCollector/processor/signozlogspipelineprocessor/stanza/operator/operators/normalize"
	_ "github.com/SigInsight/OtelCollector/processor/signozlogspipelineprocessor/stanza/operator/operators/regex"
	_ "github.com/SigInsight/OtelCollector/processor/signozlogspipelineprocessor/stanza/operator/operators/remove"
	_ "github.com/SigInsight/OtelCollector/processor/signozlogspipelineprocessor/stanza/operator/operators/router"
	_ "github.com/SigInsight/OtelCollector/processor/signozlogspipelineprocessor/stanza/operator/operators/severity"
	_ "github.com/SigInsight/OtelCollector/processor/signozlogspipelineprocessor/stanza/operator/operators/time"
	_ "github.com/SigInsight/OtelCollector/processor/signozlogspipelineprocessor/stanza/operator/operators/trace"
)
