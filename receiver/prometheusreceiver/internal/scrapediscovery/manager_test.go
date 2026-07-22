package scrapediscovery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/stretchr/testify/require"
)

type testConfig struct {
	name       string
	discoverer Discoverer
}

func (config testConfig) Name() string { return config.name }

func (config testConfig) NewDiscoverer(Options) (Discoverer, error) {
	return config.discoverer, nil
}

type testDiscoverer []*targetgroup.Group

func (discoverer testDiscoverer) Run(ctx context.Context, updates chan<- []*targetgroup.Group) {
	select {
	case <-ctx.Done():
	case updates <- discoverer:
	}
}

type failingConfig struct{}

func (failingConfig) Name() string { return "failing" }

func (failingConfig) NewDiscoverer(Options) (Discoverer, error) {
	return nil, errors.New("cannot create discoverer")
}

func TestManagerCombinesProvidersForJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	manager := NewManager(ctx, nil, nil, nil)
	go func() { _ = manager.Run() }()

	configs := map[string]Configs{
		"job": {
			testConfig{name: "first", discoverer: testDiscoverer{targetGroup("one")}},
			testConfig{name: "second", discoverer: testDiscoverer{targetGroup("two")}},
		},
	}
	require.NoError(t, manager.ApplyConfig(configs))

	deadline := time.After(time.Second)
	for {
		select {
		case targets := <-manager.SyncCh():
			if len(targets["job"]) == 2 {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for combined discovery targets")
		}
	}
}

func TestManagerSendsTargetsForAllJobs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	manager := NewManager(ctx, nil, nil, nil)
	go func() { _ = manager.Run() }()

	require.NoError(t, manager.ApplyConfig(map[string]Configs{
		"first":  {testConfig{name: "first", discoverer: testDiscoverer{targetGroup("one")}}},
		"second": {testConfig{name: "second", discoverer: testDiscoverer{targetGroup("two")}}},
	}))

	deadline := time.After(time.Second)
	for {
		select {
		case targets := <-manager.SyncCh():
			if len(targets["first"]) == 1 && len(targets["second"]) == 1 {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for all discovery targets")
		}
	}
}

func TestManagerClearsTargetsWhenApplyingNewConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	manager := NewManager(ctx, nil, nil, nil)
	go func() { _ = manager.Run() }()

	require.NoError(t, manager.ApplyConfig(map[string]Configs{
		"removed": {testConfig{name: "removed", discoverer: testDiscoverer{targetGroup("old")}}},
	}))
	require.NoError(t, manager.ApplyConfig(map[string]Configs{
		"current": {testConfig{name: "current", discoverer: testDiscoverer{targetGroup("new")}}},
	}))

	deadline := time.After(time.Second)
	for {
		select {
		case targets := <-manager.SyncCh():
			if _, removed := targets["removed"]; !removed {
				if groups, current := targets["current"]; current && len(groups) == 0 {
					return
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for cleared target snapshot")
		}
	}
}

func TestManagerKeepsCurrentProvidersWhenConfigCreationFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	manager := NewManager(ctx, nil, nil, nil)

	require.NoError(t, manager.ApplyConfig(map[string]Configs{
		"job": {testConfig{name: "current", discoverer: testDiscoverer{targetGroup("current")}}},
	}))
	require.Error(t, manager.ApplyConfig(map[string]Configs{
		"job": {failingConfig{}},
	}))

	manager.mu.Lock()
	defer manager.mu.Unlock()
	require.Len(t, manager.providers, 1)
	require.Contains(t, manager.providers, "job/current/0")
}

func TestFileConfigLoad(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "targets.json")
	require.NoError(t, os.WriteFile(filename, []byte(`[{"targets":["localhost:8080"]}]`), 0o600))

	config := &FileConfig{Files: []string{filename}}
	discoverer, err := config.NewDiscoverer(Options{})
	require.NoError(t, err)
	groups := receiveInitialGroups(t, discoverer)
	require.Len(t, groups, 1)
	require.Equal(t, filename+":0", groups[0].Source)
	require.Equal(t, model.LabelValue(filename), groups[0].Labels[model.MetaLabelPrefix+"filepath"])
}

func TestHTTPConfigLoad(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", " application/json; charset=utf-8 ")
		_, err := fmt.Fprint(writer, `[{"targets":["localhost:8081"]}]`)
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	config := &HTTPConfig{URL: server.URL}
	discoverer, err := config.NewDiscoverer(Options{})
	require.NoError(t, err)
	groups := receiveInitialGroups(t, discoverer)
	require.Len(t, groups, 1)
	require.Equal(t, server.URL+":0", groups[0].Source)
	require.Equal(t, model.LabelValue(server.URL), groups[0].Labels[model.MetaLabelPrefix+"url"])
}

func TestHTTPConfigRejectsNonJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		_, err := fmt.Fprint(writer, "<html>not a target file</html>")
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	config := &HTTPConfig{URL: server.URL}
	discoverer, err := config.NewDiscoverer(Options{})
	require.NoError(t, err)
	groups, err := discoverer.(pollingDiscoverer).load(context.Background())
	require.Nil(t, groups)
	require.ErrorContains(t, err, "unsupported content type")
}

func TestHTTPConfigRefreshCancelsWithContext(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)

	config := &HTTPConfig{URL: server.URL}
	discoverer, err := config.NewDiscoverer(Options{})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := discoverer.(pollingDiscoverer).load(ctx)
		result <- err
	}()
	<-requestStarted
	cancel()

	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for canceled HTTP discovery request")
	}
}

func receiveInitialGroups(t *testing.T, discoverer Discoverer) []*targetgroup.Group {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	updates := make(chan []*targetgroup.Group)
	go discoverer.Run(ctx, updates)
	select {
	case groups := <-updates:
		return groups
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for discovery targets")
		return nil
	}
}

func targetGroup(address string) *targetgroup.Group {
	return &targetgroup.Group{Targets: []model.LabelSet{{model.AddressLabel: model.LabelValue(address)}}}
}
