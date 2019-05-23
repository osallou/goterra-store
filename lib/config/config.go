package terraconfig

import (
	"io/ioutil"

	yaml "gopkg.in/yaml.v2"
)

// RedisConfig contains redis server connection info
type RedisConfig struct {
	Host   string
	Port   uint
	Prefix string
}

// WebConfig defines web server
type WebConfig struct {
	Listen string
	Port   uint
}

// Config contains goterra configuration
type Config struct {
	loaded bool
	Redis  RedisConfig
	URL    string `json:"url"`
	Secret string
	Web    WebConfig
}

// Singleton config
var cfg Config

// LoadConfig returns the singleton config object
func LoadConfig() Config {
	if cfg.loaded {
		return cfg
	}
	cfgfile, _ := ioutil.ReadFile("goterra.yml")
	config := Config{loaded: true}
	yaml.Unmarshal([]byte(cfgfile), &config)
	// fmt.Printf("Config: %+v\n", config)
	cfg = config
	return config
}
