package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nats-io/nats.go"

	"orisun/config"
	"orisun/logging"
)

// Event represents an event extracted from logical replication
type Event struct {
	EventType      string      `json:"event_type"`
	Data           interface{} `json:"data"`
	TransactionID  int64       `json:"transaction_id"`
	GlobalId       int64       `json:"global_id"`
	StreamID       string      `json:"stream_id"`
	StreamName     string      `json:"stream_name"`
	StreamVersion  int64       `json:"stream_version"`
	EventData      string      `json:"event_data"`
	Metadata       string      `json:"metadata"`
	CreatedAt      time.Time   `json:"created_at"`
	Boundary       string      `json:"boundary"`
}

// LogicalReplicationListener streams events using PostgreSQL logical replication
type LogicalReplicationListener struct {
	ctx                    context.Context
	logger                 logging.Logger
	natsConn               *nats.Conn
	pgConn                 *pgconn.PgConn
	eventPublishing        *PostgresEventPublishing
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping
	slotName               string
	publicationName        string
	relations              map[uint32]*pglogrepl.RelationMessage
	typeMap                *pgtype.Map
}

// NewLogicalReplicationListener creates a new logical replication-based event listener
func NewLogicalReplicationListener(
	ctx context.Context,
	logger logging.Logger,
	natsConn *nats.Conn,
	pgConnString string,
	eventPublishing *PostgresEventPublishing,
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping,
) (*LogicalReplicationListener, error) {
	// Connect with replication=database parameter
	pgConn, err := pgconn.Connect(ctx, pgConnString+"&replication=database")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL for replication: %w", err)
	}

	slotName := getEnvOrDefault("LOGICAL_REPLICATION_SLOT", "orisun_events_slot")
	publicationName := getEnvOrDefault("LOGICAL_REPLICATION_PUBLICATION", "orisun_events_pub")

	return &LogicalReplicationListener{
		ctx:                    ctx,
		logger:                 logger,
		natsConn:               natsConn,
		pgConn:                 pgConn,
		eventPublishing:        eventPublishing,
		boundarySchemaMappings: boundarySchemaMappings,
		slotName:               slotName,
		publicationName:        publicationName,
		relations:              make(map[uint32]*pglogrepl.RelationMessage),
		typeMap:                pgtype.NewMap(),
	}, nil
}

// Start begins the logical replication stream
func (l *LogicalReplicationListener) Start() error {
	l.logger.Info("Starting logical replication listener")

	// Setup replication slot and publication
	if err := l.setupReplication(); err != nil {
		return fmt.Errorf("failed to setup replication: %w", err)
	}

	// Start replication stream
	if err := l.startReplicationStream(); err != nil {
		return fmt.Errorf("failed to start replication stream: %w", err)
	}

	// Process messages
	return l.processMessages()
}

// setupReplication creates the publication and replication slot
func (l *LogicalReplicationListener) setupReplication() error {
	// Drop and recreate publication to ensure it includes all current event tables
	if _, err := l.pgConn.Exec(l.ctx, fmt.Sprintf("DROP PUBLICATION IF EXISTS %s;", l.publicationName)).ReadAll(); err != nil {
		return fmt.Errorf("failed to drop existing publication: %w", err)
	}

	// Create publication for all event tables across schemas
	var tableList []string
	for _, mapping := range l.boundarySchemaMappings {
		tableList = append(tableList, fmt.Sprintf("%s.events", mapping.Schema))
	}

	publicationSQL := fmt.Sprintf("CREATE PUBLICATION %s FOR TABLE %s;", 
		l.publicationName, 
		joinStrings(tableList, ", "))
	
	if _, err := l.pgConn.Exec(l.ctx, publicationSQL).ReadAll(); err != nil {
		return fmt.Errorf("failed to create publication: %w", err)
	}

	l.logger.Infof("Created publication %s for tables: %v", l.publicationName, tableList)

	// Create temporary replication slot
	if _, err := pglogrepl.CreateReplicationSlot(l.ctx, l.pgConn, l.slotName, "pgoutput", pglogrepl.CreateReplicationSlotOptions{Temporary: true}); err != nil {
		return fmt.Errorf("failed to create replication slot: %w", err)
	}

	l.logger.Infof("Created replication slot: %s", l.slotName)
	return nil
}

// startReplicationStream begins the logical replication stream
func (l *LogicalReplicationListener) startReplicationStream() error {
	pluginArguments := []string{
		"proto_version '1'",
		fmt.Sprintf("publication_names '%s'", l.publicationName),
		"messages 'true'",
	}

	startLSN, err := pglogrepl.ParseLSN("0/0")
	if err != nil {
		return fmt.Errorf("failed to parse start LSN: %w", err)
	}

	return pglogrepl.StartReplication(l.ctx, l.pgConn, l.slotName, startLSN, pglogrepl.StartReplicationOptions{PluginArgs: pluginArguments})
}

// processMessages handles incoming replication messages
func (l *LogicalReplicationListener) processMessages() error {
	for {
		select {
		case <-l.ctx.Done():
			l.logger.Info("Logical replication listener stopped")
			return l.ctx.Err()
		default:
			msg, err := l.pgConn.ReceiveMessage(l.ctx)
			if err != nil {
				return fmt.Errorf("failed to receive message: %w", err)
			}

			if err := l.handleMessage(msg); err != nil {
				l.logger.Errorf("Failed to handle message: %v", err)
				// Continue processing other messages
			}
		}
	}
}

// handleMessage processes different types of replication messages
func (l *LogicalReplicationListener) handleMessage(msg pgproto3.BackendMessage) error {
	switch msg := msg.(type) {
	case *pgproto3.CopyData:
		switch msg.Data[0] {
		case pglogrepl.PrimaryKeepaliveMessageByteID:
			// Handle keepalive message
			pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
			if err != nil {
				return fmt.Errorf("failed to parse keepalive message: %w", err)
			}
			
			if pkm.ReplyRequested {
				// Send standby status if requested
				if err := l.sendStandbyStatus(pkm.ServerWALEnd); err != nil {
					return fmt.Errorf("failed to send standby status: %w", err)
				}
			}

		case pglogrepl.XLogDataByteID:
			// Handle WAL data
			walData, err := pglogrepl.ParseXLogData(msg.Data[1:])
			if err != nil {
				return fmt.Errorf("failed to parse WAL data: %w", err)
			}
			
			if err := l.processWALData(&walData); err != nil {
				return fmt.Errorf("failed to process WAL data: %w", err)
			}
		}
	default:
		l.logger.Debugf("Received unexpected message: %T", msg)
	}
	return nil
}

// processWALData processes logical replication WAL data
func (l *LogicalReplicationListener) processWALData(walData *pglogrepl.XLogData) error {
	logicalMsg, err := pglogrepl.Parse(walData.WALData)
	if err != nil {
		return fmt.Errorf("failed to parse logical message: %w", err)
	}

	switch msg := logicalMsg.(type) {
	case *pglogrepl.RelationMessage:
		// Store relation information for later use
		l.relations[msg.RelationID] = msg
		l.logger.Debugf("Stored relation: %s.%s (ID: %d)", msg.Namespace, msg.RelationName, msg.RelationID)

	case *pglogrepl.InsertMessage:
		// Process new event insertion
		return l.processInsertMessage(msg)

	case *pglogrepl.UpdateMessage:
		// Events are typically immutable, but handle updates if needed
		l.logger.Debugf("Received update message for relation ID: %d", msg.RelationID)

	case *pglogrepl.DeleteMessage:
		// Events are typically immutable, but handle deletes if needed
		l.logger.Debugf("Received delete message for relation ID: %d", msg.RelationID)
	}

	return nil
}

// processInsertMessage handles new event insertions
func (l *LogicalReplicationListener) processInsertMessage(msg *pglogrepl.InsertMessage) error {
	relation, ok := l.relations[msg.RelationID]
	if !ok {
		return fmt.Errorf("relation %d not found", msg.RelationID)
	}

	// Only process events tables
	if relation.RelationName != "events" {
		return nil
	}

	// Extract event data from the insert message
	event, err := l.extractEventFromInsert(msg, relation)
	if err != nil {
		return fmt.Errorf("failed to extract event: %w", err)
	}

	// Determine boundary from schema name
	boundary := l.getBoundaryFromSchema(relation.Namespace)
	if boundary == "" {
		l.logger.Warnf("No boundary found for schema: %s", relation.Namespace)
		return nil
	}

	// Publish event to NATS
	if err := l.publishEventToNATS(event, boundary); err != nil {
		return fmt.Errorf("failed to publish event to NATS: %w", err)
	}

	// Update last published position
	if err := l.eventPublishing.InsertLastPublishedEvent(l.ctx, boundary, event.TransactionID, event.GlobalId); err != nil {
		l.logger.Errorf("Failed to update last published position: %v", err)
		// Don't return error as the event was already published
	}

	l.logger.Debugf("Published event %s from boundary %s", event.EventType, boundary)
	return nil
}

// extractEventFromInsert extracts event data from a logical replication insert message
func (l *LogicalReplicationListener) extractEventFromInsert(msg *pglogrepl.InsertMessage, relation *pglogrepl.RelationMessage) (*Event, error) {
	event := &Event{}
	
	for idx, col := range msg.Tuple.Columns {
		if idx >= len(relation.Columns) {
			continue
		}
		
		colName := relation.Columns[idx].Name
		
		switch col.DataType {
		case 'n': // null
			continue
		case 't': // text
			val, err := l.decodeTextColumnData(col.Data, relation.Columns[idx].DataType)
			if err != nil {
				return nil, fmt.Errorf("failed to decode column %s: %w", colName, err)
			}
			
			if err := l.setEventField(event, colName, val); err != nil {
				return nil, fmt.Errorf("failed to set event field %s: %w", colName, err)
			}
		}
	}
	
	return event, nil
}

// decodeTextColumnData decodes column data based on its type
func (l *LogicalReplicationListener) decodeTextColumnData(data []byte, dataType uint32) (interface{}, error) {
	if dt, ok := l.typeMap.TypeForOID(dataType); ok {
		return dt.Codec.DecodeValue(l.typeMap, dataType, pgtype.TextFormatCode, data)
	}
	return string(data), nil
}

// setEventField sets the appropriate field on the Event struct
func (l *LogicalReplicationListener) setEventField(event *Event, fieldName string, value interface{}) error {
	switch fieldName {
	case "global_id":
		if v, ok := value.(int64); ok {
			event.GlobalId = v
		}
	case "transaction_id":
		if v, ok := value.(int64); ok {
			event.TransactionID = v
		}
	case "stream_name":
		if v, ok := value.(string); ok {
			event.StreamName = v
		}
	case "stream_version":
		if v, ok := value.(int64); ok {
			event.StreamVersion = v
		}
	case "event_type":
		if v, ok := value.(string); ok {
			event.EventType = v
		}
	case "data":
		if v, ok := value.(string); ok {
			event.EventData = v
		}
	case "metadata":
		if v, ok := value.(string); ok {
			event.Metadata = v
		}
	case "date_created":
		if v, ok := value.(time.Time); ok {
			event.CreatedAt = v
		}
	}
	return nil
}

// getBoundaryFromSchema maps schema name to boundary
func (l *LogicalReplicationListener) getBoundaryFromSchema(schema string) string {
	for boundary, mapping := range l.boundarySchemaMappings {
		if mapping.Schema == schema {
			return boundary
		}
	}
	return ""
}

// publishEventToNATS publishes a single event to NATS
func (l *LogicalReplicationListener) publishEventToNATS(event *Event, boundary string) error {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	subject := fmt.Sprintf("events.%s.%s", boundary, event.EventType)
	return l.natsConn.Publish(subject, eventJSON)
}

// sendStandbyStatus sends a standby status message to PostgreSQL
func (l *LogicalReplicationListener) sendStandbyStatus(lsn pglogrepl.LSN) error {
	standbyStatus := pglogrepl.StandbyStatusUpdate{
		WALWritePosition: lsn,
		WALFlushPosition: lsn,
		WALApplyPosition: lsn,
		ClientTime:       time.Now(),
		ReplyRequested:   false,
	}
	
	return pglogrepl.SendStandbyStatusUpdate(l.ctx, l.pgConn, standbyStatus)
}

// Stop gracefully stops the logical replication listener
func (l *LogicalReplicationListener) Stop() error {
	l.logger.Info("Stopping logical replication listener")
	if l.pgConn != nil {
		return l.pgConn.Close(l.ctx)
	}
	return nil
}

// Helper functions
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	if len(strs) == 1 {
		return strs[0]
	}
	
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}