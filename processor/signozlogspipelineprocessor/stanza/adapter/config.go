package signozlogspipelinestanzaadapter

import (
	signozlogspipelinestanzaoperator "github.com/SigInsight/OtelCollector/processor/signozlogspipelineprocessor/stanza/operator"
)

type BaseConfig struct {
	// Using our own version of Config allows using a dedicated registry of stanza ops for logs pipelines.
	Operators []signozlogspipelinestanzaoperator.Config `mapstructure:"operators"`
}
