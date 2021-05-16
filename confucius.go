package confucius

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/imdario/mergo"
	"github.com/mitchellh/mapstructure"
	"github.com/pelletier/go-toml"
	"gopkg.in/yaml.v2"
)

const (
	// DefaultFilename is the default filename of the config file that confucius looks for.
	DefaultFilename = "config.yaml"
	// DefaultDir is the default directory that confucius searches in for the config file.
	DefaultDir = "."
	// DefaultTag is the default struct tag key that confucius uses to find the field's alt
	// name.
	DefaultTag = "conf"
	// DefaultTimeLayout is the default time layout that confucius uses to parse times.
	DefaultTimeLayout = time.RFC3339
	// DefaultProfileLayout represents default profile file layout.
	// You should use `config` for filename, `test` for profile, `yaml` for extension.
	// Example; config-test.yaml
	DefaultProfileLayout = "config.test.yaml"
)

// Load reads a configuration file and loads it into the given struct. The
// parameter `cfg` must be a pointer to a struct.
//
// By default confucius looks for a file `config.yaml` in the current directory and
// uses the struct field tag `fig` for matching field names and validation.
// To alter this behaviour pass additional parameters as options.
//
// A field can be marked as required by adding a `required` key in the field's struct tag.
// If a required field is not set by the configuration file an error is returned.
//
//   type Config struct {
//     Env string `conf:"env" validate:"required"` // or just `validate:"required"`
//   }
//
// A field can be configured with a default value by adding a `default` key in the
// field's struct tag.
// If a field is not set by the configuration file then the default value is set.
//
//  type Config struct {
//    Level string `conf:"level" default:"info"` // or just `default:"info"`
//  }
//
// A single field may not be marked as both `required` and `default`.
func Load(cfg interface{}, options ...Option) error {
	confucius := defaultConfucius()

	for _, opt := range options {
		opt(confucius)
	}

	return confucius.Load(cfg)
}

func defaultConfucius() *confucius {
	return &confucius{
		filename:      DefaultFilename,
		dirs:          []string{DefaultDir},
		tag:           DefaultTag,
		timeLayout:    DefaultTimeLayout,
		profileLayout: DefaultProfileLayout,
	}
}

type confucius struct {
	useEnv        bool
	useReader     bool
	dirs          []string
	profiles      []string
	filename      string
	tag           string
	timeLayout    string
	envPrefix     string
	profileLayout string
	readerConfig  io.Reader
	readerDecoder Decoder
}

func (c *confucius) Load(cfg interface{}) (err error) {
	if !isStructPtr(cfg) {
		return fmt.Errorf("cfg must be a pointer to a struct")
	}

	readerVals := make(map[string]interface{})

	file, err := c.findCfgFile()
	if !c.useReader && err != nil {
		return err
	}

	vals, err := c.decodeFile(file)
	if !c.useReader && err != nil {
		return err
	}

	if c.useReader {
		readerVals, err = c.decodeReader(c.readerConfig, c.readerDecoder)
		if err != nil {
			return err
		}
		if err := mergo.Merge(&readerVals, vals, mergo.WithOverride, mergo.WithTypeCheck); err != nil {
			return err
		}
		vals = readerVals
	}

	for _, profile := range c.profiles {
		profileFile, err := c.findProfileCfgFile(profile)
		if err != nil {
			return err
		}

		profileVals, err := c.decodeFile(profileFile)
		if err != nil {
			return fmt.Errorf("%v, filename: %s", err, profileFile)
		}

		if err := mergo.Merge(&vals, profileVals, mergo.WithOverride, mergo.WithTypeCheck); err != nil {
			return err
		}
	}

	if err := c.decodeMap(vals, cfg); err != nil {
		return err
	}

	return c.processCfg(cfg)
}

func (c *confucius) profileFileName(profile string) string {
	filename := c.profileLayout
	parts := strings.Split(c.filename, ".")
	filename = strings.ReplaceAll(filename, "config", parts[0])
	filename = strings.ReplaceAll(filename, "test", profile)
	filename = strings.ReplaceAll(filename, "yaml", parts[1])
	return filename
}

func (c *confucius) findProfileCfgFile(profile string) (path string, err error) {
	file := c.profileFileName(profile)
	for _, dir := range c.dirs {
		path = filepath.Join(dir, file)
		if fileExists(path) {
			return
		}
	}
	return "", fmt.Errorf("%s: %w", file, ErrFileNotFound)
}

func (c *confucius) findCfgFile() (path string, err error) {
	for _, dir := range c.dirs {
		path = filepath.Join(dir, c.filename)
		if fileExists(path) {
			return
		}
	}
	return "", fmt.Errorf("%s: %w", c.filename, ErrFileNotFound)
}

// decodeFile reads the file and unmarshalls it using a decoder based on the file extension.
func (c *confucius) decodeFile(file string) (map[string]interface{}, error) {
	fd, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	return c.decodeReader(fd, Decoder(filepath.Ext(file)))
}

func (c *confucius) decodeReader(reader io.Reader, decoder Decoder) (map[string]interface{}, error) {
	vals := make(map[string]interface{})

	switch decoder {
	case ".yaml", ".yml":
		if err := yaml.NewDecoder(reader).Decode(&vals); err != nil {
			return nil, err
		}
	case ".json":
		if err := json.NewDecoder(reader).Decode(&vals); err != nil {
			return nil, err
		}
	case ".toml":
		tree, err := toml.LoadReader(reader)
		if err != nil {
			return nil, err
		}
		for field, val := range tree.ToMap() {
			vals[field] = val
		}
	default:
		return nil, fmt.Errorf("unsupported file extension %s", filepath.Ext(c.filename))
	}

	return vals, nil
}

// decodeMap decodes a map of values into result using the mapstructure library.
func (c *confucius) decodeMap(m map[string]interface{}, result interface{}) error {
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		WeaklyTypedInput: true,
		Result:           result,
		TagName:          c.tag,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			fromEnvironmentHookFunc(),
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToTimeHookFunc(c.timeLayout),
		),
	})
	if err != nil {
		return err
	}
	return dec.Decode(m)
}

func replaceEnvironments(str string) (result string, err error) {
	re := regexp.MustCompile(`\$\{(.*?|)\}`)
	result = str
	for _, match := range re.FindAllStringSubmatch(str, -1) {
		whole, value := match[0], match[1]
		if value == "" {
			return result, fmt.Errorf("environment name is missing")
		}

		s := strings.Split(value, ":")
		envName := s[0]
		if envValue, ok := os.LookupEnv(envName); ok {
			result = strings.ReplaceAll(result, whole, envValue)
		} else {
			defaultVal := ""
			if len(s) > 1 {
				defaultVal = s[1]
			}
			result = strings.ReplaceAll(result, whole, defaultVal)
		}
	}
	return result, err
}

func fromEnvironmentHookFunc() mapstructure.DecodeHookFunc {
	return func(
		f reflect.Type,
		t reflect.Type,
		data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String {
			return data, nil
		}

		return replaceEnvironments(data.(string))
	}
}

// processCfg processes a cfg struct after it has been loaded from
// the config file, by validating required fields and setting defaults
// where applicable.
func (c *confucius) processCfg(cfg interface{}) error {
	fields := flattenCfg(cfg, c.tag)
	errs := make(fieldErrors)

	for _, field := range fields {
		if err := c.processField(field); err != nil {
			errs[field.path()] = err
		}
	}

	if len(errs) > 0 {
		return errs
	}

	return nil
}

// processField processes a single field and is called by processCfg
// for each field in cfg.
func (c *confucius) processField(field *field) error {
	if field.required && field.setDefault {
		return fmt.Errorf("field cannot have both a required validation and a default value")
	}

	if c.useEnv {
		if err := c.setFromEnv(field.v, field.path()); err != nil {
			return fmt.Errorf("unable to set from env: %v", err)
		}
	}

	if field.required && isZero(field.v) {
		return fmt.Errorf("required validation failed")
	}

	if field.setDefault && isZero(field.v) {
		if err := c.setDefaultValue(field.v, field.defaultVal); err != nil {
			return fmt.Errorf("unable to set default: %v", err)
		}
	}

	return nil
}

func (c *confucius) setFromEnv(fv reflect.Value, key string) error {
	key = c.formatEnvKey(key)
	if val, ok := os.LookupEnv(key); ok {
		return c.setValue(fv, val)
	}
	return nil
}

func (c *confucius) formatEnvKey(key string) string {
	// loggers[0].level --> loggers_0_level
	key = strings.NewReplacer(".", "_", "[", "_", "]", "").Replace(key)
	if c.envPrefix != "" {
		key = fmt.Sprintf("%s_%s", c.envPrefix, key)
	}
	return strings.ToUpper(key)
}

// setDefaultValue calls setValue but disallows booleans from
// being set.
func (c *confucius) setDefaultValue(fv reflect.Value, val string) error {
	if fv.Kind() == reflect.Bool {
		return fmt.Errorf("unsupported type: %v", fv.Kind())
	}
	return c.setValue(fv, val)
}

// setValue sets fv to val. it attempts to convert val to the correct
// type based on the field's kind. if conversion fails an error is
// returned.
// fv must be settable else this panics.
func (c *confucius) setValue(fv reflect.Value, val string) error {
	switch fv.Kind() {
	case reflect.Ptr:
		if fv.IsNil() {
			fv.Set(reflect.New(fv.Type().Elem()))
		}
		return c.setValue(fv.Elem(), val)
	case reflect.Slice:
		if err := c.setSlice(fv, val); err != nil {
			return err
		}
	case reflect.Bool:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if _, ok := fv.Interface().(time.Duration); ok {
			d, err := time.ParseDuration(val)
			if err != nil {
				return err
			}
			fv.Set(reflect.ValueOf(d))
		} else {
			i, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return err
			}
			fv.SetInt(i)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		i, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			return err
		}
		fv.SetUint(i)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	case reflect.String:
		fv.SetString(val)
	case reflect.Struct: // struct is only allowed a default in the special case where it's a time.Time
		if _, ok := fv.Interface().(time.Time); ok {
			t, err := time.Parse(c.timeLayout, val)
			if err != nil {
				return err
			}
			fv.Set(reflect.ValueOf(t))
		} else {
			return fmt.Errorf("unsupported type %s", fv.Kind())
		}
	default:
		return fmt.Errorf("unsupported type %s", fv.Kind())
	}
	return nil
}

// setSlice val to sv. val should be a Go slice formatted as a string
// (e.g. "[1,2]") and sv must be a slice value. if conversion of val
// to a slice fails then an error is returned.
// sv must be settable else this panics.
func (c *confucius) setSlice(sv reflect.Value, val string) error {
	ss := stringSlice(val)
	slice := reflect.MakeSlice(sv.Type(), len(ss), cap(ss))
	for i, s := range ss {
		if err := c.setValue(slice.Index(i), s); err != nil {
			return err
		}
	}
	sv.Set(slice)
	return nil
}