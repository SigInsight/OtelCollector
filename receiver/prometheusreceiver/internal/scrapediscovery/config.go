package scrapediscovery

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"

	"github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"go.yaml.in/yaml/v2"
)

const (
	configFieldPrefix = "AUTO_DISCOVERY_"
	staticConfigsKey  = "static_configs"
)

type Discoverer interface {
	Run(context.Context, chan<- []*targetgroup.Group)
}

type Options struct {
	Logger *slog.Logger
}

type Config interface {
	Name() string
	NewDiscoverer(Options) (Discoverer, error)
}

type Configs []Config

func (configs *Configs) SetDirectory(dir string) {
	for _, discoveryConfig := range *configs {
		if setter, ok := discoveryConfig.(config.DirectorySetter); ok {
			setter.SetDirectory(dir)
		}
	}
}

var (
	configNames      = map[string]Config{}
	configFieldNames = map[reflect.Type]string{}
	configFields     []reflect.StructField
	configsType      = reflect.TypeFor[Configs]()
)

func init() {
	registerConfig(staticConfigsKey, reflect.TypeFor[*targetgroup.Group](), StaticConfig{})
}

func RegisterConfig(discoveryConfig Config) {
	registerConfig(discoveryConfig.Name()+"_sd_configs", reflect.TypeOf(discoveryConfig), discoveryConfig)
}

func registerConfig(yamlKey string, elementType reflect.Type, discoveryConfig Config) {
	if _, exists := configNames[discoveryConfig.Name()]; exists {
		panic(fmt.Sprintf("scrapediscovery: config %q is already registered", discoveryConfig.Name()))
	}
	configNames[discoveryConfig.Name()] = discoveryConfig
	fieldName := configFieldPrefix + yamlKey
	configFieldNames[elementType] = fieldName
	configFields = append(configFields, reflect.StructField{
		Name: fieldName,
		Type: reflect.SliceOf(elementType),
		Tag:  reflect.StructTag(`yaml:"` + yamlKey + `,omitempty"`),
	})
	sort.Slice(configFields, func(index, next int) bool { return configFields[index].Name < configFields[next].Name })
}

func UnmarshalYAMLWithInlineConfigs(out any, unmarshal func(any) error) error {
	outValue := reflect.ValueOf(out)
	if outValue.Kind() != reflect.Pointer || outValue.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("scrapediscovery: expected a pointer to struct, got %T", out)
	}
	outValue = outValue.Elem()
	outType := outValue.Type()
	fields := make([]reflect.StructField, 0, outType.NumField()+len(configFields))
	configsIndex := -1
	for index := 0; index < outType.NumField(); index++ {
		field := outType.Field(index)
		if field.Type == configsType {
			configsIndex = index
			continue
		}
		if field.PkgPath != "" {
			continue
		}
		fields = append(fields, field)
	}
	if configsIndex == -1 {
		return fmt.Errorf("scrapediscovery: Configs field not found in %T", out)
	}
	configStartIndex := len(fields)
	fields = append(fields, configFields...)
	dynamicType := reflect.StructOf(fields)
	dynamic := reflect.New(dynamicType)
	for index := 0; index < outType.NumField(); index++ {
		field := outType.Field(index)
		if field.Type == configsType || field.PkgPath != "" {
			continue
		}
		dynamic.Elem().FieldByName(field.Name).Set(outValue.Field(index))
	}
	if err := unmarshal(dynamic.Interface()); err != nil {
		return err
	}
	for index := 0; index < outType.NumField(); index++ {
		field := outType.Field(index)
		if field.Type == configsType || field.PkgPath != "" {
			continue
		}
		outValue.Field(index).Set(dynamic.Elem().FieldByName(field.Name))
	}
	configs, err := readConfigs(dynamic.Elem(), configStartIndex)
	if err != nil {
		return err
	}
	outValue.Field(configsIndex).Set(reflect.ValueOf(configs))
	return nil
}

func MarshalYAMLWithInlineConfigs(in any) (any, error) {
	inValue := reflect.ValueOf(in)
	for inValue.Kind() == reflect.Pointer {
		inValue = inValue.Elem()
	}
	inType := inValue.Type()
	fields := make([]reflect.StructField, 0, inType.NumField()+len(configFields))
	configsIndex := -1
	for index := 0; index < inType.NumField(); index++ {
		field := inType.Field(index)
		if field.Type == configsType {
			configsIndex = index
			continue
		}
		if field.PkgPath == "" {
			fields = append(fields, field)
		}
	}
	if configsIndex == -1 {
		return nil, fmt.Errorf("scrapediscovery: Configs field not found in %T", in)
	}
	fields = append(fields, configFields...)
	dynamicType := reflect.StructOf(fields)
	dynamic := reflect.New(dynamicType).Elem()
	for index := 0; index < inType.NumField(); index++ {
		field := inType.Field(index)
		if field.Type == configsType || field.PkgPath != "" {
			continue
		}
		dynamic.FieldByName(field.Name).Set(inValue.Field(index))
	}
	configs := inValue.Field(configsIndex).Interface().(Configs)
	for _, discoveryConfig := range configs {
		if staticConfig, ok := discoveryConfig.(StaticConfig); ok {
			field := dynamic.FieldByName(configFieldPrefix + staticConfigsKey)
			for _, group := range staticConfig {
				field.Set(reflect.Append(field, reflect.ValueOf(group)))
			}
			continue
		}
		fieldName, exists := configFieldNames[reflect.TypeOf(discoveryConfig)]
		if !exists {
			return nil, fmt.Errorf("scrapediscovery: cannot marshal unregistered config type %T", discoveryConfig)
		}
		field := dynamic.FieldByName(fieldName)
		field.Set(reflect.Append(field, reflect.ValueOf(discoveryConfig)))
	}
	return dynamic.Addr().Interface(), nil
}

func readConfigs(value reflect.Value, startIndex int) (Configs, error) {
	configs := Configs{}
	staticGroups := []*targetgroup.Group{}
	for index := startIndex; index < value.NumField(); index++ {
		field := value.Field(index)
		for itemIndex := 0; itemIndex < field.Len(); itemIndex++ {
			item := field.Index(itemIndex)
			if item.Kind() == reflect.Pointer && item.IsNil() {
				return nil, fmt.Errorf("empty or null discovery config")
			}
			switch item := item.Interface().(type) {
			case *targetgroup.Group:
				item.Source = fmt.Sprintf("%d", len(staticGroups))
				staticGroups = append(staticGroups, item)
			case Config:
				configs = append(configs, item)
			default:
				return nil, fmt.Errorf("invalid discovery config type %T", item)
			}
		}
	}
	if len(staticGroups) > 0 {
		configs = append(configs, StaticConfig(staticGroups))
	}
	return configs, nil
}

type StaticConfig []*targetgroup.Group

func (StaticConfig) Name() string { return "static" }

func (configs StaticConfig) NewDiscoverer(Options) (Discoverer, error) {
	return staticDiscoverer(configs), nil
}

type staticDiscoverer []*targetgroup.Group

func (discovery staticDiscoverer) Run(ctx context.Context, updates chan<- []*targetgroup.Group) {
	select {
	case <-ctx.Done():
	case updates <- discovery:
	}
}

func RegisteredConfigNames() []string {
	names := make([]string, 0, len(configNames))
	for name := range configNames {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func IsSupportedField(field string) bool {
	return strings.HasPrefix(field, configFieldPrefix)
}

var _ yaml.Unmarshaler
