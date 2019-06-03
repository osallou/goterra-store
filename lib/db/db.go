package goterradb

import (
	"fmt"

	"github.com/go-redis/redis"

	terraConfig "github.com/osallou/goterra-lib/lib/config"
)

var client DbHandler

// DbHandler is used to get redis client and info
type DbHandler struct {
	Client *redis.Client
	Prefix string
	init   bool
}

// NewClient returns a redis client
func NewClient(cfg terraConfig.Config) DbHandler {
	if client.init {
		return client
	}
	c := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port), // use default Addr
		Password: "",                                                   // no password set
		DB:       0,                                                    // use default DB
	})
	// fmt.Printf("Redis info: %s : %d\n", cfg.Redis.Host, cfg.Redis.Port)
	handler := DbHandler{Client: c, Prefix: cfg.Redis.Prefix, init: true}
	return handler
}

// Deployment is used to get/set a a deployment data
type Deployment struct {
	id string
}
