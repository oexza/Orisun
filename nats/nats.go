package nats

import (
	"context"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	c "github.com/oexza/Orisun/config"
	l "github.com/oexza/Orisun/logging"
	"net/url"
	"time"
)

func InitializeNATS(ctx context.Context, config c.NatsConfig, logger l.Logger) (jetstream.JetStream, *nats.Conn, *server.Server) {
	natsOptions := createNATSOptions(config, logger)
	natsServer := startNATSServer(natsOptions, config.Cluster.Timeout, logger)

	// Connect to NATS
	nc, err := nats.Connect("", nats.InProcessServer(natsServer))
	if err != nil {
		logger.Fatalf("Failed to connect to NATS: %v", err)
	}

	// Create JetStream context
	js, err := jetstream.New(nc)
	if err != nil {
		logger.Fatalf("Failed to create JetStream context: %v", err)
	}
	// time.Sleep(30 * time.Second)
	waitForJetStream(ctx, js, logger)
	return js, nc, natsServer
}

func createNATSOptions(config c.NatsConfig, logger l.Logger) server.Options {
	options := server.Options{
		ServerName: config.ServerName,
		Port:       config.Port,
		MaxPayload: config.MaxPayload,
		JetStream:  true,
		StoreDir:   config.StoreDir,
	}

	if config.Cluster.Enabled {
		options.Cluster = server.ClusterOpts{
			Name: config.Cluster.Name,
			Host: config.Cluster.Host,
			Port: config.Cluster.Port,
		}
		options.Routes = convertToURLSlice(config.Cluster.GetRoutes(), logger)
		logger.Info("Nats cluster is enabled, running in clustered mode")
		logger.Info(
			"Cluster configuration: Name=%v, Host=%v, Port=%v, Routes=%v",
			config.Cluster.Name,
			config.Cluster.Host,
			config.Cluster.Port,
			config.Cluster.Routes,
		)
	} else {
		logger.Info("Nats cluster is disabled, running in standalone mode")
	}

	return options
}

func startNATSServer(options server.Options, timeout time.Duration, logger l.Logger) *server.Server {
	natsServer, err := server.NewServer(&options)
	if err != nil {
		logger.Fatalf("Failed to create NATS server: %v", err)
	}

	natsServer.ConfigureLogger()
	go natsServer.Start()
	if !natsServer.ReadyForConnections(timeout) {
		logger.Fatal("NATS server failed to start")
	}
	logger.Info("NATS server started on ", natsServer.ClientURL())

	return natsServer
}

func waitForJetStream(ctx context.Context, js jetstream.JetStream, logger l.Logger) {
	jetStreamTestDone := make(chan struct{})

	go func() {
		defer close(jetStreamTestDone)

		for {
			streamName := "test_jetstream"
			_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
				Name: streamName,
				Subjects: []string{
					streamName + ".test",
				},
				MaxMsgs: 1,
			})

			if err != nil {
				logger.Warnf("failed to add stream: %v %v", streamName, err)
				time.Sleep(5 * time.Second)
				continue
			}

			r, err := js.Publish(ctx, streamName+".test", []byte("test"))
			if err != nil {
				logger.Warnf("Failed to publish to JetStream, retrying in 5 second: %v", err)
				time.Sleep(5 * time.Second)
			} else {
				logger.Infof("Published to JetStream: %v", r)
				break
			}
		}
	}()

	select {
	case <-jetStreamTestDone:
		logger.Info("JetStream system is available")
	}
}

func convertToURLSlice(routes []string, logger l.Logger) []*url.URL {
	var urls []*url.URL
	for _, route := range routes {
		u, err := url.Parse(route)
		if err != nil {
			logger.Fatalf("Warning: invalid route URL %q: %v", route, err)
			continue
		}
		urls = append(urls, u)
	}
	return urls
}
