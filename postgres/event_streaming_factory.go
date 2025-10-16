package postgres

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	common "orisun/admin/slices/common"
	"orisun/config"
	pb "orisun/eventstore"
	"orisun/logging"
)

// EventStreamingManager manages both WAL and polling event streaming approaches
type EventStreamingManager struct {
	config          config.AppConfig
	logger          logging.Logger
	walListener     *LogicalReplicationListener
	pollingActive   bool
	ctx             context.Context
}

// NewEventStreamingManager creates a new event streaming manager
func NewEventStreamingManager(
	ctx context.Context,
	config config.AppConfig,
	logger logging.Logger,
) *EventStreamingManager {
	return &EventStreamingManager{
		config: config,
		logger: logger,
		ctx:    ctx,
	}
}

// StartEventStreaming starts the appropriate event streaming method(s) based on configuration
func (esm *EventStreamingManager) StartEventStreaming(
	natsConn *nats.Conn,
	js jetstream.JetStream,
	lockProvider pb.LockProvider,
	getEvents pb.EventstoreGetEvents,
	eventPublishing common.EventPublishing,
) error {
	method := esm.config.EventStreaming.Method
	
	esm.logger.Infof("Starting event streaming with method: %s", method)
	
	switch method {
	case "wal":
		return esm.startWALStreaming(natsConn, eventPublishing)
	case "polling":
		return esm.startPollingStreaming(js, lockProvider, getEvents, eventPublishing)
	case "both":
		// Start both WAL and polling
		if err := esm.startWALStreaming(natsConn, eventPublishing); err != nil {
			esm.logger.Errorf("Failed to start WAL streaming: %v", err)
		}
		if err := esm.startPollingStreaming(js, lockProvider, getEvents, eventPublishing); err != nil {
			esm.logger.Errorf("Failed to start polling streaming: %v", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown event streaming method: %s", method)
	}
}

// startWALStreaming starts the logical replication (WAL) streaming
func (esm *EventStreamingManager) startWALStreaming(
	natsConn *nats.Conn,
	eventPublishing common.EventPublishing,
) error {
	if !esm.config.EventStreaming.WAL.Enabled {
		esm.logger.Info("WAL streaming is disabled in configuration")
		return nil
	}
	
	// Create PostgreSQL connection string
	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		esm.config.Postgres.Host,
		esm.config.Postgres.Port,
		esm.config.Postgres.User,
		esm.config.Postgres.Password,
		esm.config.Postgres.Name,
		esm.config.Postgres.SSLMode,
	)
	
	// Create logical replication listener
	listener, err := NewLogicalReplicationListener(
		esm.ctx,
		esm.logger,
		natsConn,
		connStr,
		eventPublishing.(*PostgresEventPublishing),
		esm.config.Postgres.GetSchemaMapping(),
	)
	if err != nil {
		return fmt.Errorf("failed to create logical replication listener: %w", err)
	}
	
	esm.walListener = listener
	
	// Start the listener in a goroutine
	go func() {
		esm.logger.Info("Starting WAL-based event streaming")
		if err := listener.Start(); err != nil {
			esm.logger.Errorf("WAL event streaming failed: %v", err)
		}
	}()
	
	// Graceful shutdown
	go func() {
		<-esm.ctx.Done()
		esm.logger.Info("Shutting down WAL event streaming")
		if esm.walListener != nil {
			esm.walListener.Stop()
		}
	}()
	
	return nil
}

// startPollingStreaming starts the polling-based event streaming
func (esm *EventStreamingManager) startPollingStreaming(
	js jetstream.JetStream,
	lockProvider pb.LockProvider,
	getEvents pb.EventstoreGetEvents,
	eventPublishing common.EventPublishing,
) error {
	if !esm.config.EventStreaming.Polling.Enabled {
		esm.logger.Info("Polling streaming is disabled in configuration")
		return nil
	}
	
	esm.logger.Info("Starting polling-based event streaming")
	esm.pollingActive = true
	
	// Use the existing startEventPolling function from main.go
	// We'll need to refactor this to be accessible from here
	go esm.runPollingLoop(js, lockProvider, getEvents, eventPublishing)
	
	return nil
}

// runPollingLoop runs the polling loop (extracted from main.go logic)
func (esm *EventStreamingManager) runPollingLoop(
	js jetstream.JetStream,
	lockProvider pb.LockProvider,
	getEvents pb.EventstoreGetEvents,
	eventPublishing common.EventPublishing,
) {
	// This will be implemented to call the existing polling logic
	// For now, we'll keep the polling logic in main.go and call it from there
	esm.logger.Info("Polling loop started (implementation delegated to main.go)")
}

// Stop stops all active event streaming
func (esm *EventStreamingManager) Stop() {
	esm.logger.Info("Stopping event streaming manager")
	
	if esm.walListener != nil {
		esm.walListener.Stop()
	}
	
	esm.pollingActive = false
}

// IsWALEnabled returns true if WAL streaming is enabled
func (esm *EventStreamingManager) IsWALEnabled() bool {
	return esm.config.EventStreaming.WAL.Enabled
}

// IsPollingEnabled returns true if polling streaming is enabled
func (esm *EventStreamingManager) IsPollingEnabled() bool {
	return esm.config.EventStreaming.Polling.Enabled
}