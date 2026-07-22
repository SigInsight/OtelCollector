package scrapediscovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	commonconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"go.yaml.in/yaml/v2"
)

type FileConfig struct {
	Files           []string       `yaml:"files"`
	RefreshInterval model.Duration `yaml:"refresh_interval,omitempty"`
}

var filePathPattern = regexp.MustCompile(`^[^*]*(\*[^/]*)?\.(json|yml|yaml|JSON|YML|YAML)$`)

var httpContentTypePattern = regexp.MustCompile(`^(?i:application/json(;\s*charset=("utf-8"|utf-8))?)$`)

func init() {
	RegisterConfig(&FileConfig{})
	RegisterConfig(&HTTPConfig{})
}

func (*FileConfig) Name() string { return "file" }

func (config *FileConfig) SetDirectory(dir string) {
	for index, file := range config.Files {
		config.Files[index] = commonconfig.JoinDir(dir, file)
	}
}

func (config *FileConfig) UnmarshalYAML(unmarshal func(any) error) error {
	*config = FileConfig{RefreshInterval: model.Duration(5 * time.Minute)}
	type plain FileConfig
	if err := unmarshal((*plain)(config)); err != nil {
		return err
	}
	if len(config.Files) == 0 {
		return errors.New("file service discovery config must contain at least one path name")
	}
	for _, file := range config.Files {
		if !filePathPattern.MatchString(file) {
			return fmt.Errorf("path name %q is not valid for file discovery", file)
		}
	}
	return nil
}

func (config *FileConfig) NewDiscoverer(options Options) (Discoverer, error) {
	if len(config.Files) == 0 {
		return nil, errors.New("file service discovery config must contain at least one path name")
	}
	interval := time.Duration(config.RefreshInterval)
	if interval == 0 {
		interval = 5 * time.Minute
	}
	return pollingDiscoverer{
		interval: interval,
		load: func(context.Context) ([]*targetgroup.Group, error) {
			return config.load()
		},
		logger: options.Logger,
	}, nil
}

func (config *FileConfig) load() ([]*targetgroup.Group, error) {
	files := []string{}
	for _, pattern := range config.Files {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		files = append(files, matches...)
	}
	sort.Strings(files)
	groups := []*targetgroup.Group{}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		loaded := []*targetgroup.Group{}
		if filepath.Ext(file) == ".json" || filepath.Ext(file) == ".JSON" {
			err = json.Unmarshal(content, &loaded)
		} else {
			err = yaml.UnmarshalStrict(content, &loaded)
		}
		if err != nil {
			return nil, err
		}
		for index, group := range loaded {
			if group == nil {
				return nil, errors.New("nil target group item found")
			}
			group.Source = file + ":" + strconv.Itoa(index)
			if group.Labels == nil {
				group.Labels = model.LabelSet{}
			}
			group.Labels[model.MetaLabelPrefix+"filepath"] = model.LabelValue(file)
		}
		groups = append(groups, loaded...)
	}
	return groups, nil
}

type HTTPConfig struct {
	HTTPClientConfig commonconfig.HTTPClientConfig `yaml:",inline"`
	RefreshInterval  model.Duration                `yaml:"refresh_interval,omitempty"`
	URL              string                        `yaml:"url"`
}

func (*HTTPConfig) Name() string { return "http" }

func (config *HTTPConfig) SetDirectory(dir string) {
	config.HTTPClientConfig.SetDirectory(dir)
}

func (config *HTTPConfig) UnmarshalYAML(unmarshal func(any) error) error {
	*config = HTTPConfig{
		HTTPClientConfig: commonconfig.DefaultHTTPClientConfig,
		RefreshInterval:  model.Duration(time.Minute),
	}
	type plain HTTPConfig
	if err := unmarshal((*plain)(config)); err != nil {
		return err
	}
	if config.URL == "" {
		return errors.New("URL is missing")
	}
	parsedURL, err := url.Parse(config.URL)
	if err != nil {
		return err
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return errors.New("URL scheme must be 'http' or 'https'")
	}
	if parsedURL.Host == "" {
		return errors.New("host is missing in URL")
	}
	return config.HTTPClientConfig.Validate()
}

func (config *HTTPConfig) NewDiscoverer(options Options) (Discoverer, error) {
	if config.URL == "" {
		return nil, errors.New("URL is missing")
	}
	parsedURL, err := url.Parse(config.URL)
	if err != nil {
		return nil, err
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, errors.New("URL scheme must be 'http' or 'https'")
	}
	if parsedURL.Host == "" {
		return nil, errors.New("host is missing in URL")
	}
	if err := config.HTTPClientConfig.Validate(); err != nil {
		return nil, err
	}
	client, err := commonconfig.NewClientFromConfig(config.HTTPClientConfig, "http")
	if err != nil {
		return nil, err
	}
	interval := time.Duration(config.RefreshInterval)
	if interval == 0 {
		interval = time.Minute
	}
	return pollingDiscoverer{interval: interval, load: func(ctx context.Context) ([]*targetgroup.Group, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, config.URL, http.NoBody)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, errors.New(response.Status)
		}
		if !httpContentTypePattern.MatchString(strings.TrimSpace(response.Header.Get("Content-Type"))) {
			return nil, fmt.Errorf("unsupported content type %q", response.Header.Get("Content-Type"))
		}
		groups := []*targetgroup.Group{}
		if err := json.NewDecoder(response.Body).Decode(&groups); err != nil {
			return nil, err
		}
		for index, group := range groups {
			if group == nil {
				return nil, errors.New("nil target group item found")
			}
			group.Source = config.URL + ":" + strconv.Itoa(index)
			if group.Labels == nil {
				group.Labels = model.LabelSet{}
			}
			group.Labels[model.MetaLabelPrefix+"url"] = model.LabelValue(config.URL)
		}
		return groups, nil
	}, logger: options.Logger}, nil
}

type pollingDiscoverer struct {
	interval time.Duration
	load     func(context.Context) ([]*targetgroup.Group, error)
	logger   *slog.Logger
}

func (discovery pollingDiscoverer) Run(ctx context.Context, updates chan<- []*targetgroup.Group) {
	publish := func() {
		groups, err := discovery.load(ctx)
		if err != nil {
			if discovery.logger != nil {
				discovery.logger.Warn("service discovery refresh failed", "err", err)
			}
			return
		}
		select {
		case <-ctx.Done():
		case updates <- groups:
		}
	}
	publish()
	ticker := time.NewTicker(discovery.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			publish()
		}
	}
}
