package scrapediscovery

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/discovery/targetgroup"
)

type SDMetrics struct {
	MechanismMetrics map[string]struct{}
	RefreshManager   refreshManager
}

type refreshManager struct{}

func (refreshManager) Unregister() {}

func CreateAndRegisterSDMetrics(prometheus.Registerer) (*SDMetrics, error) {
	return &SDMetrics{MechanismMetrics: map[string]struct{}{}}, nil
}

type Manager struct {
	ctx           context.Context
	logger        *slog.Logger
	syncCh        chan map[string][]*targetgroup.Group
	mu            sync.Mutex
	applyMu       sync.Mutex
	sendMu        sync.Mutex
	providers     map[string]context.CancelFunc
	targets       map[string]map[string][]*targetgroup.Group
	providerOrder map[string][]string
	generation    uint64
}

type providerSpec struct {
	jobName      string
	providerName string
	discoverer   Discoverer
	ctx          context.Context
	cancel       context.CancelFunc
}

type ManagerOption func(*Manager)

func Updatert(time.Duration) ManagerOption { return func(*Manager) {} }

func NewManager(ctx context.Context, configuredLogger *slog.Logger, _ prometheus.Registerer, _ *SDMetrics, options ...ManagerOption) *Manager {
	logger := slog.Default()
	if configuredLogger != nil {
		logger = configuredLogger
	}
	manager := &Manager{
		ctx:           ctx,
		logger:        logger,
		syncCh:        make(chan map[string][]*targetgroup.Group, 1),
		providers:     map[string]context.CancelFunc{},
		targets:       map[string]map[string][]*targetgroup.Group{},
		providerOrder: map[string][]string{},
	}
	for _, option := range options {
		option(manager)
	}
	return manager
}

func (manager *Manager) Run() error {
	<-manager.ctx.Done()
	manager.sendMu.Lock()
	manager.mu.Lock()
	for _, cancel := range manager.providers {
		cancel()
	}
	manager.providers = map[string]context.CancelFunc{}
	manager.targets = map[string]map[string][]*targetgroup.Group{}
	manager.providerOrder = map[string][]string{}
	manager.mu.Unlock()
	manager.sendMu.Unlock()
	return manager.ctx.Err()
}

func (manager *Manager) SyncCh() <-chan map[string][]*targetgroup.Group { return manager.syncCh }

func (manager *Manager) UnregisterMetrics() {}

func (manager *Manager) ApplyConfig(configs map[string]Configs) error {
	manager.applyMu.Lock()
	defer manager.applyMu.Unlock()

	jobNames := make([]string, 0, len(configs))
	for jobName := range configs {
		jobNames = append(jobNames, jobName)
	}
	sort.Strings(jobNames)

	providerSpecs := make([]providerSpec, 0)
	for _, jobName := range jobNames {
		for index, discoveryConfig := range configs[jobName] {
			discoverer, err := discoveryConfig.NewDiscoverer(Options{Logger: manager.logger})
			if err != nil {
				return err
			}
			providerSpecs = append(providerSpecs, providerSpec{
				jobName:      jobName,
				providerName: jobName + "/" + discoveryConfig.Name() + "/" + strconv.Itoa(index),
				discoverer:   discoverer,
			})
		}
	}

	manager.sendMu.Lock()
	manager.mu.Lock()
	for _, cancel := range manager.providers {
		cancel()
	}
	manager.providers = map[string]context.CancelFunc{}
	manager.targets = map[string]map[string][]*targetgroup.Group{}
	manager.providerOrder = map[string][]string{}
	manager.generation++
	generation := manager.generation
	for index := range providerSpecs {
		providerSpecs[index].ctx, providerSpecs[index].cancel = context.WithCancel(manager.ctx)
		manager.providers[providerSpecs[index].providerName] = providerSpecs[index].cancel
		manager.providerOrder[providerSpecs[index].jobName] = append(
			manager.providerOrder[providerSpecs[index].jobName],
			providerSpecs[index].providerName,
		)
	}
	targets := manager.allTargets()
	manager.mu.Unlock()
	manager.sendLatestUpdate(manager.ctx, targets)
	manager.sendMu.Unlock()

	for _, providerSpec := range providerSpecs {
		go manager.runDiscoverer(
			providerSpec.ctx,
			providerSpec.jobName,
			providerSpec.providerName,
			generation,
			providerSpec.discoverer,
		)
	}
	return nil
}

func (manager *Manager) runDiscoverer(ctx context.Context, jobName, providerName string, generation uint64, discoverer Discoverer) {
	updates := make(chan []*targetgroup.Group)
	go discoverer.Run(ctx, updates)
	for {
		select {
		case <-ctx.Done():
			return
		case groups, ok := <-updates:
			if !ok {
				return
			}
			manager.sendMu.Lock()
			manager.mu.Lock()
			if manager.generation != generation {
				manager.mu.Unlock()
				manager.sendMu.Unlock()
				return
			}
			if manager.targets[jobName] == nil {
				manager.targets[jobName] = map[string][]*targetgroup.Group{}
			}
			manager.targets[jobName][providerName] = groups
			targets := manager.allTargets()
			manager.mu.Unlock()
			manager.sendLatestUpdate(ctx, targets)
			manager.sendMu.Unlock()
		}
	}
}

func (manager *Manager) allTargets() map[string][]*targetgroup.Group {
	targets := make(map[string][]*targetgroup.Group, len(manager.providerOrder))
	for jobName, providerNames := range manager.providerOrder {
		for _, providerName := range providerNames {
			targets[jobName] = append(targets[jobName], manager.targets[jobName][providerName]...)
		}
	}
	return targets
}

func (manager *Manager) clearPendingUpdate() {
	select {
	case <-manager.syncCh:
	default:
	}
}

func (manager *Manager) sendLatestUpdate(ctx context.Context, targets map[string][]*targetgroup.Group) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	manager.clearPendingUpdate()
	select {
	case <-ctx.Done():
	case manager.syncCh <- targets:
	}
}
