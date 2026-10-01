package config

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

// Reader owns one Viper instance per invocation. CLI flags are bound to
// this same instance, so explicit flags override environment and file values.
type Reader struct {
	v     *viper.Viper
	codec *feedIDCodec
}

func NewReader() *Reader {
	codec := &feedIDCodec{}
	registry := yamlRegistry{codec}
	v := viper.NewWithOptions(viper.WithDecoderRegistry(registry))
	v.SetConfigType("yaml")
	v.SetEnvPrefix("PODSYNC")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AllowEmptyEnv(true)
	v.SetDefault("config", DefaultConfigPath)
	v.SetDefault("server.port", DefaultServerPort)
	v.SetDefault("storage.type", "local")
	v.SetDefault("database.type", "sqlite")

	// Named API/R2 variables remain the documented deployment interface.
	for key, name := range map[string]string{
		"config":                       "PODSYNC_CONFIG_PATH",
		"no-banner":                    "PODSYNC_NO_BANNER",
		"tokens.youtube":               "PODSYNC_YOUTUBE_API_KEY",
		"tokens.vimeo":                 "PODSYNC_VIMEO_API_KEY",
		"tokens.soundcloud":            "PODSYNC_SOUNDCLOUD_API_KEY",
		"tokens.twitch":                "PODSYNC_TWITCH_API_KEY",
		"storage.r2.endpoint_url":      "PODSYNC_R2_ENDPOINT_URL",
		"storage.r2.access_key_id":     "PODSYNC_R2_ACCESS_KEY_ID",
		"storage.r2.secret_access_key": "PODSYNC_R2_SECRET_ACCESS_KEY",
		"storage.r2.bucket":            "PODSYNC_R2_BUCKET",
		"storage.r2.public_url":        "PODSYNC_R2_PUBLIC_URL",
	} {
		_ = v.BindEnv(key, name)
	}
	// Bind known struct fields explicitly so environment-only values are also
	// included by Viper.Unmarshal. Dynamic feed maps keep their file settings.
	bindStructEnv(v, "", reflect.TypeOf(Config{}))
	return &Reader{v: v, codec: codec}
}

func bindStructEnv(v *viper.Viper, prefix string, typ reflect.Type) {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := field.Tag.Get("mapstructure")
		if name == "" || name == "-" {
			continue
		}
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		child := field.Type
		if child.Kind() == reflect.Pointer {
			child = child.Elem()
		}
		switch child.Kind() {
		case reflect.Struct:
			bindStructEnv(v, key, child)
		case reflect.Map:
			// Tokens have explicit bindings above; feeds are configuration data.
		default:
			_ = v.BindEnv(key)
		}
	}
}

func (r *Reader) read(path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".yaml" && ext != ".yml" {
		return fmt.Errorf("configuration file must use .yaml or .yml: %s", path)
	}
	r.v.SetConfigFile(path)
	if err := r.v.ReadInConfig(); err != nil {
		return fmt.Errorf("read YAML configuration %s: %w", path, err)
	}
	return nil
}

func (r *Reader) Load(path string) (*Config, error) {
	if err := r.read(path); err != nil {
		return nil, err
	}
	var config Config
	if err := r.v.Unmarshal(&config, configDecodeHook()); err != nil {
		return nil, fmt.Errorf("decode configuration: %w", err)
	}
	feeds := make(map[string]*Feed, len(config.Feeds))
	for key, f := range config.Feeds {
		id := r.codec.feedIDs[key]
		if id == "" || f == nil {
			return nil, fmt.Errorf("invalid feed configuration: %s", key)
		}
		f.ID = id
		feeds[id] = f
	}
	config.Feeds = feeds
	config.applyDefaults(path)
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

func (r *Reader) LoadDatabase(path string) (*Database, error) {
	if err := r.read(path); err != nil {
		return nil, err
	}
	return r.DatabaseConfig(path)
}

func (r *Reader) DatabaseConfig(path string) (*Database, error) {
	// Unmarshal the envelope so Viper resolves each bound child key. Reading
	// the database subtree alone would miss environment-only and flag values.
	var config struct {
		Database Database `mapstructure:"database"`
	}
	if err := r.v.Unmarshal(&config, configDecodeHook()); err != nil {
		return nil, fmt.Errorf("decode database configuration: %w", err)
	}
	applyDatabaseDefaults(&config.Database, path)
	return &config.Database, nil
}

func configDecodeHook() viper.DecoderConfigOption {
	return viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		stringSliceHook,
	))
}

// Token strings from files or environment may contain space-separated keys.
// Arrays preserve each string as written, including arguments containing spaces.
func stringSliceHook(from, to reflect.Type, value any) (any, error) {
	if to != reflect.TypeOf([]string{}) {
		return value, nil
	}
	if from.Kind() == reflect.String {
		return strings.Fields(value.(string)), nil
	}
	if from.Kind() != reflect.Slice {
		return nil, fmt.Errorf("expected a string or an array of strings")
	}
	items := reflect.ValueOf(value)
	for i := 0; i < items.Len(); i++ {
		if _, ok := items.Index(i).Interface().(string); !ok {
			return nil, fmt.Errorf("array item %d must be a string", i)
		}
	}
	return value, nil
}

// Viper normalizes keys, but feed identifiers are case-sensitive data used in
// database rows and RSS URLs. Replace only feed map keys during native YAML
// decoding, then restore the original identifiers after unmarshaling. Safe
// internal keys also preserve identifiers containing dots or differing by case.
type feedIDCodec struct {
	feedIDs map[string]string
}

func (c *feedIDCodec) Decode(data []byte, values map[string]any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(values); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("configuration must contain a single YAML mapping document")
	}
	if len(values) == 0 {
		return fmt.Errorf("configuration must contain a nonempty YAML mapping")
	}
	c.feedIDs = make(map[string]string)
	for key, value := range values {
		if !strings.EqualFold(key, "feeds") {
			continue
		}
		feeds, ok := value.(map[string]any)
		if !ok {
			continue
		}
		ids := make([]string, 0, len(feeds))
		for id := range feeds {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		aliased := make(map[string]any, len(feeds))
		for i, id := range ids {
			alias := fmt.Sprintf("feed_%d", i)
			aliased[alias] = feeds[id]
			c.feedIDs[alias] = id
		}
		values[key] = aliased
	}
	return nil
}

// BindFlag attaches command flags to this invocation's configuration precedence.
func (r *Reader) BindFlag(key string, flag *pflag.Flag) error { return r.v.BindPFlag(key, flag) }
func (r *Reader) Path() string                                { return r.v.GetString("config") }
func (r *Reader) Debug() bool                                 { return r.v.GetBool("log.debug") }
func (r *Reader) NoBanner() bool                              { return r.v.GetBool("no-banner") }
func (r *Reader) OverrideDatabase(driver, dsn string) {
	r.v.Set("database.type", driver)
	r.v.Set("database.dsn", dsn)
}

// yamlRegistry deliberately exposes only YAML decoding to Viper.
type yamlRegistry struct{ codec *feedIDCodec }

func (r yamlRegistry) Decoder(format string) (viper.Decoder, error) {
	if format != "yaml" && format != "yml" {
		return nil, fmt.Errorf("unsupported configuration format: %s", format)
	}
	return r.codec, nil
}
