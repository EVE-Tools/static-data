package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"
	"time"

	"github.com/antihax/goesi"
	"google.golang.org/grpc"

	"github.com/EVE-Tools/element43/go/lib/transport"
	"github.com/EVE-Tools/static-data/lib/locations"
	pb "github.com/EVE-Tools/static-data/lib/staticData"

	"github.com/boltdb/bolt"
	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	"github.com/grpc-ecosystem/go-grpc-middleware/tags"
	"github.com/kelseyhightower/envconfig"
	"github.com/sirupsen/logrus"
)

// Config holds the application's configuration info from the environment.
type Config struct {
	DBPath            string `default:"static-data.db" envconfig:"db_path"`
	LogLevel          string `default:"info" envconfig:"log_level"`
	Port              string `default:"43000" envconfig:"port"`
	ESIHost           string `default:"esi.tech.ccp.is" envconfig:"esi_host"`
	StructureHuntHost string `default:"stop.hammerti.me.uk" envconfig:"structure_hunt_host"`
	DisableTLS        bool   `default:"false" envconfig:"disable_tls"`
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

// getClients generates API clients and base URLs
func getClients(config Config) (*goesi.APIClient, *http.Client, string) {
	const userAgent string = "Element43/static-data (element-43.com)"
	const timeout time.Duration = time.Duration(time.Second * 30)
	var structureHuntURL string

	// Initialize clients
	genericClient := &http.Client{
		Timeout:   time.Minute,
		Transport: transport.NewTransport(userAgent),
	}

	httpClientESI := &http.Client{
		Timeout:   timeout,
		Transport: transport.NewESITransport(userAgent, timeout),
	}

	esiClient := goesi.NewAPIClient(httpClientESI, userAgent)

	if config.DisableTLS {
		esiClient.ChangeBasePath(fmt.Sprintf("http://%s:443", config.ESIHost))
		structureHuntURL = fmt.Sprintf("http://%s:443/api/structure/all", config.StructureHuntHost)
	} else {
		esiClient.ChangeBasePath(fmt.Sprintf("https://%s", config.ESIHost))
		structureHuntURL = fmt.Sprintf("https://%s/api/structure/all", config.StructureHuntHost)
	}

	return esiClient, genericClient, structureHuntURL
}

// Init DB and start gRPC endpoint.
func startEndpoint(config Config) {
	db, err := bolt.Open(config.DBPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		panic(err)
	}

	esiClient, genericClient, url := getClients(config)

	locations.Initialize(esiClient,
		genericClient,
		url,
		db)

	var opts []grpc.ServerOption
	var logOpts []grpc_logrus.Option
	opts = append(opts, grpc_middleware.WithUnaryServerChain(
		grpc_ctxtags.UnaryServerInterceptor(),
		grpc_logrus.UnaryServerInterceptor(logrus.NewEntry(logrus.New()), logOpts...)))

	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%s", config.Port))

	if err != nil {
		log.Fatalf("could not listen: %v", err)
	}

	grpcServer := grpc.NewServer(opts...)
	pb.RegisterStaticDataServer(grpcServer, &locations.Server{})
	grpcServer.Serve(listener)
}
