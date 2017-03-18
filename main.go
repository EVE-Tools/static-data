package main

import (
	"runtime"
	"time"

	"github.com/EVE-Tools/static-data/lib/locations"

	"github.com/Sirupsen/logrus"
	"github.com/boltdb/bolt"
	"github.com/gin-gonic/contrib/cors"
	"github.com/gin-gonic/contrib/ginrus"
	"github.com/gin-gonic/gin"
	"github.com/kelseyhightower/envconfig"
)

// Config holds the application's configuration info from the environment.
type Config struct {
	DBPath   string `default:"static-data.db" envconfig:"db_path"`
	LogLevel string `default:"info" envconfig:"log_level"`
	Port     string `default:"8000" envconfig:"port"`
}

func main() {
	config := loadConfig()
	startEndpoint(config)

	// Terminate this goroutine, crash if all other goroutines exited
	runtime.Goexit()
}

// Load configuration from environment
func loadConfig() Config {
	config := Config{}
	envconfig.MustProcess("STATIC_DATA", &config)

	logLevel, err := logrus.ParseLevel(config.LogLevel)
	if err != nil {
		panic(err)
	}

	logrus.SetLevel(logLevel)
	logrus.Debugf("Config: %q", config)
	return config
}

// Init DB and start endpoint.
func startEndpoint(config Config) {
	db, err := bolt.Open(config.DBPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		panic(err)
	}

	locations.Inititalize(db)

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(cors.Default())
	router.Use(ginrus.Ginrus(logrus.StandardLogger(), time.RFC3339, true))

	v1 := router.Group("/api/static-data/v1")
	v1.POST("/location/", locations.GetLocationsEndpoint)

	router.Run(":" + config.Port)
}
