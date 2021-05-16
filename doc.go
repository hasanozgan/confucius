/*
package confucius loads configuration files into Go structs with extra juice for validating fields and setting defaults.

Config files may be defined in in yaml, json or toml format.

When you call `Load()`, confucius takes the following steps:

  1. Finds config file
  2. Loads file into config struct
  3. Fills config struct from the environment (if enabled)
  4. Sets defaults (where applicable)
  5. Validates required fields (where applicable)

Example

Define your configuration file in the root of your project:

  # config.yaml

  build: "2020-01-09T12:30:00Z"

  server:
    ports:
      - 8080
    cleanup: 1h

  logger:
    level: "warn"
    trace: true

Define your struct and load it:

 package main

 import (
   "fmt"

   "github.com/hasanozgan/confucius"
 )


  type Config struct {
    Build  time.Time `conf:"build" validate:"required"`
    Server struct {
      Host    string        `conf:"host" default:"127.0.0.1"`
      Ports   []int         `conf:"ports" default:"[80,443]"`
      Cleanup time.Duration `conf:"cleanup" default:"30m"`
    }
    Logger struct {
      Level string `conf:"level" default:"info"`
      Trace bool   `conf:"trace"`
    }
  }

 func main() {
   var cfg Config
   _ = confucius.Load(&cfg)

   fmt.Printf("%+v\n", cfg)
   // Output: {Build:2019-12-25 00:00:00 +0000 UTC Server:{Host:127.0.0.1 Ports:[8080] Cleanup:1h0m0s} Logger:{Level:warn Trace:true}}
 }

By default confucius searches for a file named `config.yaml` in the directory it is run from.
It can be configured to look elsewhere.

Configuration

Pass options as additional parameters to `Load()` to configure fig's behaviour.

File

Change the file and directories confucius searches in with `File()`.

  confucius.Load(&cfg,
    confucius.File("settings.json"),
    confucius.Dirs(".", "home/user/myapp", "/opt/myapp"),
  )

Fig searches for the file in dirs sequentially and uses the first matching file.

The decoder (yaml/json/toml) used is picked based on the file's extension.

Tag

The struct tag key tag confucius looks for to find the field's alt name can be changed using `Tag()`.

  type Config struct {
    Host  string `yaml:"host" validate:"required"`
    Level string `yaml:"level" default:"info"`
  }

  var cfg Config
  confucius.Load(&cfg, confucius.Tag("yaml"))

By default confucius uses the tag key `fig`.

Environment

Fig can be configured to additionally set fields using the environment. This will happen after the struct is loaded from a config file and thus any values found in the environment will overwrite existing values in the struct.

This is meant to be used in conjunction with loading from a file. There is no support to ONLY load from the environment. You could, but you'd still have to provide an (empty) config file.

This behaviour is disabled by default and can be enabled using the option `UseEnv(prefix)`. Prefix is a string that will be prepended to the keys that are searched in the environment. Although discouraged, prefix may be left empty.

Fig searches for keys in the form PREFIX_FIELD_PATH, or if prefix is left empty then FIELD_PATH.

A field's path is formed by prepending its name with the names of all the surrounding structs up to the root struct, upper-cased and separated by an underscore.

If a field has an alt name defined in its struct tag then that name is preferred over its struct name.

  type Config struct {
    Build    time.Time
    LogLevel string `conf:"log_level"`
    Server   struct {
      Host string
    }
  }

With the struct above and `UseEnv("myapp")` confucius would search for the following
environment variables:

  MYAPP_BUILD
  MYAPP_LOG_LEVEL
  MYAPP_SERVER_HOST

Fields contained in struct slices whose elements already exists can be also be set via the environment in the form PARENT_IDX_FIELD, where idx is the index of the field in the slice.

  type Config struct {
    Server []struct {
      Host string
    }
  }

With the config above individual servers may be configured with the following environment variable:

  MYAPP_SERVER_0_HOST
  MYAPP_SERVER_1_HOST
  ...

Note: the Server slice must already have members inside it (i.e. from loading of the configuration file) for the containing fields to be altered via the environment. Fig will not instantiate and insert elements into the slice.

Time

Change the layout confucius uses to parse times using `TimeLayout()`.

  type Config struct {
    Date time.Time `conf:"date" default:"12-25-2019"`
  }

  var cfg Config
  confucius.Load(&cfg, confucius.TimeLayout("01-02-2006"))

  fmt.Printf("%+v", cfg)
  // Output: {Date:2019-12-25 00:00:00 +0000 UTC}

By default confucius parses time using the `RFC.3339` layout (`2006-01-02T15:04:05Z07:00`).

Required

A validate key with a required value in the field's struct tag makes confucius check if the field has been set after it's been loaded. Required fields that are not set are returned as an error.

  type Config struct {
    Host string `conf:"host" validate:"required"` // or simply `validate:"required"`
  }

Fig uses the following properties to check if a field is set:

  basic types:           != to its zero value ("" for str, 0 for int, etc.)
  slices, arrays:        len() > 0
  pointers*, interfaces: != nil
  structs:               always true (use a struct pointer to check for struct presence)
  time.Time:             !time.IsZero()
  time.Duration:         != 0

  *pointers to non-struct types (with the exception of time.Time) are de-referenced if they are non-nil and then checked

See example below to help understand:

  type Config struct {
    A string    `validate:"required"`
    B *string   `validate:"required"`
    C int       `validate:"required"`
    D *int      `validate:"required"`
    E []float32 `validate:"required"`
    F struct{}  `validate:"required"`
    G *struct{} `validate:"required"`
    H struct {
      I interface{} `validate:"required"`
      J interface{} `validate:"required"`
    } `validate:"required"`
    K *[]bool    `validate:"required"`
    L []uint     `validate:"required"`
    M *time.Time `validate:"required"`
  }

  var cfg Config

  // simulate loading of config file
  b := ""
  cfg.B = &b
  cfg.H.I = 5.5
  cfg.K = &[]bool{}
  cfg.L = []uint{5}
  m := time.Time{}
  cfg.M = &m

  err := confucius.Load(&cfg)
  fmt.Print(err)
  // A: required, B: required, C: required, D: required, E: required, G: required, H.J: required, K: required, M: required

Default

A default key in the field tag makes confucius fill the field with the value specified when the field is not otherwise set.

Fig attempts to parse the value based on the field's type. If parsing fails then an error is returned.

  type Config struct {
    Port int `conf:"port" default:"8000"` // or simply `default:"8000"`
  }


A default value can be set for the following types:

  all basic types except bool and complex
  time.Time
  time.Duration
  slices (of above types)

Successive elements of slice defaults should be separated by a comma. The entire slice can optionally be enclosed in square brackets:

  type Config struct {
    Durations []time.Duration `default:"[30m,1h,90m,2h]"` // or `default:"30m,1h,90m,2h"`
  }

Note: the default setter knows if it should fill a field or not by comparing if the current value of the field is equal to the corresponding zero value for that field's type. This happens after the configuration is loaded and has the implication that the zero value set explicitly by the user will get overwritten by any default value registered for that field. It's for this reason that defaults on booleans are not permitted, as a boolean field with a default value of `true` would always be true (since if it were set to false it'd be overwritten).

Mutual exclusion

The required validation and the default field tags are mutually exclusive as they are contradictory.

This is not allowed:

  type Config struct {
    Level string `validate:"required" default:"warn"` // will result in an error
  }

Errors

A wrapped error `ErrFileNotFound` is returned when confucius is not able to find a config file to load. This can be useful for instance to fallback to a different configuration loading mechanism.

  var cfg Config
  err := confucius.Load(&cfg)
  if errors.Is(err, confucius.ErrFileNotFound) {
    // load config from elsewhere
  }
*/
package confucius
