package resourcedetectionprocessor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	backoff "github.com/cenkalti/backoff/v5"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/processor"
	"go.uber.org/zap"
)

type detector interface {
	Detect(context.Context) (pcommon.Resource, string, error)
}

type detectorFactory func(processor.Settings, *Config) (detector, error)

type providerFactory struct {
	detectors map[string]detectorFactory
}

func (f *providerFactory) create(settings processor.Settings, timeout time.Duration, cfg *Config) (*resourceProvider, error) {
	detectors := make([]detector, 0, len(cfg.Detectors))
	for _, configured := range cfg.Detectors {
		detectorType := strings.TrimSpace(configured)
		factory, ok := f.detectors[detectorType]
		if !ok {
			return nil, fmt.Errorf("invalid detector key: %v", detectorType)
		}
		created, err := factory(settings, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed creating detector type %q: %w", detectorType, err)
		}
		detectors = append(detectors, created)
	}
	return &resourceProvider{logger: settings.Logger, timeout: timeout, detectors: detectors}, nil
}

type resourceResult struct {
	resource  pcommon.Resource
	schemaURL string
	err       error
}

type resourceProvider struct {
	logger    *zap.Logger
	timeout   time.Duration
	detectors []detector

	detectedResource atomic.Pointer[resourceResult]
	stopCh           chan struct{}
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	startOnce        sync.Once
	stopOnce         sync.Once
}

func (p *resourceProvider) get() (pcommon.Resource, string, error) {
	if result := p.detectedResource.Load(); result != nil {
		return result.resource, result.schemaURL, result.err
	}
	return pcommon.NewResource(), "", nil
}

func (p *resourceProvider) refresh(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	res, schemaURL, err := p.detect(ctx)
	previous := p.detectedResource.Load()
	if previous != nil && previous.err == nil && !isEmptyResource(previous.resource) && err != nil {
		p.logger.Warn("resource refresh failed; keeping previous snapshot", zap.Error(err))
		return nil
	}
	p.detectedResource.Store(&resourceResult{resource: res, schemaURL: schemaURL, err: err})
	return err
}

func (p *resourceProvider) detect(ctx context.Context) (pcommon.Resource, string, error) {
	res := pcommon.NewResource()
	mergedSchemaURL := ""
	var joinedErr error
	successes := 0
	results := make([]chan resourceResult, len(p.detectors))

	for index, configuredDetector := range p.detectors {
		resultCh := make(chan resourceResult, 1)
		results[index] = resultCh
		go detectWithRetry(ctx, configuredDetector, resultCh)
	}

	for _, resultCh := range results {
		result := <-resultCh
		if result.err != nil {
			joinedErr = errors.Join(joinedErr, result.err)
			continue
		}
		successes++
		mergedSchemaURL = mergeSchemaURL(mergedSchemaURL, result.schemaURL)
		mergeResource(res, result.resource, false)
	}

	if successes == 0 {
		if joinedErr == nil {
			joinedErr = errors.New("resource detection failed: no detectors succeeded")
		}
		return pcommon.NewResource(), "", joinedErr
	}
	return res, mergedSchemaURL, joinedErr
}

func detectWithRetry(ctx context.Context, configuredDetector detector, resultCh chan<- resourceResult) {
	retry := backoff.ExponentialBackOff{
		InitialInterval:     time.Second,
		RandomizationFactor: 1.5,
		Multiplier:          2,
	}
	retry.Reset()

	for {
		res, schemaURL, err := configuredDetector.Detect(ctx)
		if err == nil {
			resultCh <- resourceResult{resource: res, schemaURL: schemaURL}
			return
		}
		next := retry.NextBackOff()
		if next == backoff.Stop {
			resultCh <- resourceResult{err: err}
			return
		}
		timer := time.NewTimer(next)
		select {
		case <-ctx.Done():
			timer.Stop()
			resultCh <- resourceResult{err: err}
			return
		case <-timer.C:
		}
	}
}

func (p *resourceProvider) startRefreshing(interval time.Duration) {
	p.startOnce.Do(func() {
		if interval <= 0 {
			return
		}
		p.stopCh = make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		p.cancel = cancel
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := p.refresh(ctx); err != nil {
						p.logger.Warn("resource refresh failed", zap.Error(err))
					}
				case <-p.stopCh:
					return
				}
			}
		}()
	})
}

func (p *resourceProvider) stopRefreshing() {
	p.stopOnce.Do(func() {
		if p.cancel != nil {
			p.cancel()
		}
		if p.stopCh != nil {
			close(p.stopCh)
			p.wg.Wait()
		}
	})
}

func mergeSchemaURL(current, next string) string {
	if current == "" {
		return next
	}
	if next == "" || current == next {
		return current
	}
	return current
}

func mergeResource(to, from pcommon.Resource, override bool) {
	if isEmptyResource(from) {
		return
	}
	for key, value := range from.Attributes().All() {
		if override {
			value.CopyTo(to.Attributes().PutEmpty(key))
		} else if target, found := to.Attributes().GetOrPutEmpty(key); !found {
			value.CopyTo(target)
		}
	}
}

func isEmptyResource(res pcommon.Resource) bool {
	return res == (pcommon.Resource{}) || res.Attributes().Len() == 0
}
