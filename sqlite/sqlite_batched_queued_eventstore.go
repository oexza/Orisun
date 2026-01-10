package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BatchedQueuedEventStore wraps SQLiteSaveEvents with intelligent batching
// It fetches multiple requests at once, validates concurrency across them, and inserts in one batch
type BatchedQueuedEventStore struct {
	saveEvents   *SQLiteSaveEvents
	requestQueue chan saveRequest
	workerDone   chan struct{}
	started      atomic.Value
	logger       logging.Logger
	maxBatchSize int
}

// NewBatchedQueuedEventStore creates a new batched queued event store
func NewBatchedQueuedEventStore(saveEvents *SQLiteSaveEvents, queueSize int, maxBatchSize int, logger logging.Logger) *BatchedQueuedEventStore {
	q := &BatchedQueuedEventStore{
		saveEvents:   saveEvents,
		requestQueue: make(chan saveRequest, queueSize),
		workerDone:   make(chan struct{}),
		logger:       logger,
		maxBatchSize: maxBatchSize,
	}
	q.started.Store(false)
	return q
}

// Start begins the background worker
func (q *BatchedQueuedEventStore) Start() {
	if q.started.Load().(bool) {
		return
	}
	q.started.Store(true)
	go q.worker()
}

// Stop gracefully shuts down the worker
func (q *BatchedQueuedEventStore) Stop() {
	if !q.started.Load().(bool) {
		return
	}
	close(q.requestQueue)
	<-q.workerDone
	q.started.Store(false)
}

// Save queues a save request and waits for the result
func (q *BatchedQueuedEventStore) Save(
	ctx context.Context,
	events []orisun.EventWithMapTags,
	boundary string,
	expectedPosition *orisun.Position,
	streamConsistencyCondition *orisun.Query,
) (transactionID string, globalID int64, err error) {
	// Ensure worker is started
	if !q.started.Load().(bool) {
		q.logger.Warn("BatchedQueuedEventStore not started, starting automatically")
		q.Start()
	}

	// Create result channel
	resultChan := make(chan saveResult, 1)

	// Create and queue the request
	req := saveRequest{
		ctx:                        ctx,
		events:                     events,
		boundary:                   boundary,
		expectedPosition:           expectedPosition,
		streamConsistencyCondition: streamConsistencyCondition,
		result:                     resultChan,
	}

	select {
	case q.requestQueue <- req:
	case <-ctx.Done():
		return "", 0, ctx.Err()
	}

	// Wait for result
	select {
	case result := <-resultChan:
		return result.transactionID, result.globalID, result.err
	case <-ctx.Done():
		return "", 0, ctx.Err()
	}
}

// worker processes requests in batches with cross-batch concurrency validation
func (q *BatchedQueuedEventStore) worker() {
	defer close(q.workerDone)

	q.logger.Info("BatchedQueuedEventStore worker started")

	for {
		// Fetch a batch of requests (up to maxBatchSize or timeout)
		requests := q.fetchBatch()

		if len(requests) == 0 {
			// Queue is closed and empty
			return
		}

		// Process the batch
		q.processBatch(requests)
	}

	q.logger.Info("BatchedQueuedEventStore worker stopped")
}

// fetchBatch collects up to maxBatchSize requests from the queue
// Drains the channel completely to ensure all available requests are processed together
func (q *BatchedQueuedEventStore) fetchBatch() []saveRequest {
	var batch []saveRequest

	// Collect first request (blocking)
	req, ok := <-q.requestQueue
	if !ok {
		return nil // Queue closed
	}
	batch = append(batch, req)

	// Drain the channel aggressively - collect ALL immediately available requests
	// Keep looping until we hit maxBatchSize or channel is truly empty
	collected := true
	for len(batch) < q.maxBatchSize && collected {
		collected = false
		// Try to collect without blocking
		select {
		case r := <-q.requestQueue:
			batch = append(batch, r)
			collected = true
		default:
			// Channel might be empty OR requests are still being sent
			// Try one more time to catch any in-flight sends
			select {
			case r := <-q.requestQueue:
				batch = append(batch, r)
				collected = true
			default:
				// Truly empty now
			}
		}
	}

	return batch
}

// processBatch validates and processes a batch of requests together
func (q *BatchedQueuedEventStore) processBatch(requests []saveRequest) {
	// Start transaction early to lock the DB for writes
	// This prevents other processes from inserting events while we validate
	ctx := requests[0].ctx
	tx, err := q.saveEvents.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelSerializable,
		ReadOnly:  false,
	})
	if err != nil {
		q.logger.Errorf("Failed to begin transaction: %v", err)
		// Send error to all requests
		for _, req := range requests {
			q.sendResult(req, saveResult{"", 0,
				status.Errorf(codes.Internal, "failed to begin transaction: %v", err)})
		}
		return
	}

	// Rollback on error (will be cleared if we commit successfully)
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Phase 1: Fetch positions from DB and validate against DB
	// This returns only requests that passed DB-level validation
	batchState, validRequests := q.fetchPositionsAndValidateAgainstDB(requests, tx)

	// Phase 2: Validate cross-batch concurrency within this batch
	validRequests = q.validateCrossBatchConcurrency(validRequests, batchState)

	// Phase 3: Batch insert all valid events using the same transaction
	if txErr := q.batchInsertWithTx(validRequests, tx); txErr != nil {
		err = txErr
		return
	}

	// Clear error so defer doesn't rollback
	err = nil
}

// batchState tracks the latest position for each criteria within the batch
type batchState struct {
	sync.Mutex
	// Map from criteria hash to latest position seen in this batch
	latestPositions map[string]*orisun.Position
	// Map from criteria hash to number of events validated for this criteria in this batch
	eventCounts map[string]int
}

// validatedRequest represents a request that passed validation
type validatedRequest struct {
	request      saveRequest
	criteriaHash string // Used to update batch state after successful insert
}

// eventLocation represents the location of an event within a batch
type eventLocation struct {
	requestIdx int
	eventIdx   int
}

// fetchPositionsAndValidateAgainstDB fetches positions from DB and validates requests against DB in parallel
// Uses a worker pool to validate requests concurrently while preserving order
// Returns batch state with positions from DB, and only the requests that passed DB-level validation
func (q *BatchedQueuedEventStore) fetchPositionsAndValidateAgainstDB(requests []saveRequest, tx *sql.Tx) (*batchState, []validatedRequest) {
	state := &batchState{
		latestPositions: make(map[string]*orisun.Position),
		eventCounts:     make(map[string]int),
	}
	var stateMutex sync.Mutex

	// Number of workers - use fewer workers to reduce synchronization overhead
	// Profiling showed lock contention is a major bottleneck
	numWorkers := 2
	if runtime.NumCPU() < 2 {
		numWorkers = 1
	}

	// Create channels for worker pool
	jobs := make(chan int, len(requests))
	results := make(chan validateResult, len(requests))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				result := q.validateRequestAgainstDB(idx, requests[idx], tx, state, &stateMutex)
				results <- result
			}
		}()
	}

	// Send all jobs directly (no need for extra goroutine)
	for i := range requests {
		jobs <- i
	}
	close(jobs)

	// Wait for workers to finish
	wg.Wait()
	close(results)

	// Collect results in original order
	resultMap := make([]validateResult, len(requests))
	for result := range results {
		resultMap[result.idx] = result
	}

	// Build validRequests slice from results
	validRequests := make([]validatedRequest, 0, len(requests))
	for _, result := range resultMap {
		if result.valid {
			validRequests = append(validRequests, validatedRequest{
				request:      requests[result.idx],
				criteriaHash: result.criteriaHash,
			})
		}
	}

	return state, validRequests
}

// validateResult holds the result of validating a single request
type validateResult struct {
	idx          int
	valid        bool
	criteriaHash string
}

// validateRequestAgainstDB validates a single request against the DB
func (q *BatchedQueuedEventStore) validateRequestAgainstDB(idx int, req saveRequest, tx *sql.Tx, state *batchState, stateMutex *sync.Mutex) validateResult {
	// Check if context is cancelled
	if err := req.ctx.Err(); err != nil {
		q.sendResult(req, saveResult{"", 0, err})
		return validateResult{idx: idx, valid: false}
	}

	var criteriaHash string
	var validated bool = false

	// Validate optimistic concurrency if criteria provided
	if req.streamConsistencyCondition != nil && len(req.streamConsistencyCondition.Criteria) > 0 {
		criteriaHash = hashCriteria(req.streamConsistencyCondition.Criteria)

		// Fetch latest position from DB for this criteria (only once per unique criteria)
		stateMutex.Lock()
		_, exists := state.latestPositions[criteriaHash]
		stateMutex.Unlock()

		if !exists {
			latestTxID, latestGID, err := q.saveEvents.getLatestPositionForCriteria(tx, req.streamConsistencyCondition.Criteria)
			if err != nil {
				// Real database error - fail the request immediately
				q.sendResult(req, saveResult{
					"",
					0,
					status.Errorf(codes.Internal, "failed to get latest position for criteria: %v", err),
				})
				return validateResult{idx: idx, valid: false}
			}

			// Store the position with mutex protection
			stateMutex.Lock()
			state.latestPositions[criteriaHash] = &orisun.Position{
				CommitPosition:  latestTxID,
				PreparePosition: latestGID,
			}
			// Initialize event count to 0
			state.eventCounts[criteriaHash] = 0
			stateMutex.Unlock()
		}

		// Now validate against DB position
		stateMutex.Lock()
		latestPos := state.latestPositions[criteriaHash]
		stateMutex.Unlock()

		// Check if expected position matches DB position
		if req.expectedPosition != nil {
			expectedTxID := req.expectedPosition.CommitPosition
			expectedGID := req.expectedPosition.PreparePosition
			latestTxID := latestPos.CommitPosition
			latestGID := latestPos.PreparePosition

			// For new streams (latestTxID == -1), allow if expected position is also -1
			// For existing streams, position must match exactly
			if latestTxID != -1 && (latestTxID != expectedTxID || latestGID != expectedGID) {
				q.sendResult(req, saveResult{
					"",
					0,
					q.createConcurrencyError(expectedTxID, expectedGID, latestTxID, latestGID),
				})
				return validateResult{idx: idx, valid: false}
			}
		}

		validated = true
	} else {
		// No criteria means no concurrency validation needed
		validated = true
	}

	return validateResult{
		idx:          idx,
		valid:        validated,
		criteriaHash: criteriaHash,
	}
}

// validateCrossBatchConcurrency validates requests against events in the current batch
// This performs an in-memory version of the DB optimistic concurrency check:
// For each request, check if any events in the batch match its criteria - if yes, fail immediately
func (q *BatchedQueuedEventStore) validateCrossBatchConcurrency(requests []validatedRequest, state *batchState) []validatedRequest {
	// Pre-build total events count
	totalEvents := 0
	for _, vreq := range requests {
		totalEvents += len(vreq.request.events)
	}

	// Build tag -> events mapping (single pass)
	// Pre-allocate with estimated capacity to reduce reallocations
	tagIndex := make(map[string][]eventLocation, totalEvents*2)

	for reqIdx, vreq := range requests {
		for eventIdx, event := range vreq.request.events {
			// Extract tags from event data (data is map[string]interface{})
			if dataMap, ok := event.Data.(map[string]interface{}); ok {
				for key, value := range dataMap {
					// Normalize value to string for consistent matching with criteria tags
					valueStr := fastStringConversion(value)
					// Build tag key efficiently without fmt.Sprintf
					tagKey := key + ":" + valueStr
					tagIndex[tagKey] = append(tagIndex[tagKey], eventLocation{reqIdx, eventIdx})
				}
			}
		}
	}

	validRequests := make([]validatedRequest, 0, len(requests))

	// For each request, check if any in-batch events match its criteria
	for reqIdx, vreq := range requests {
		// Check if context is cancelled
		if err := vreq.request.ctx.Err(); err != nil {
			q.sendResult(vreq.request, saveResult{"", 0, err})
			continue
		}

		// If no criteria, no validation needed
		if vreq.request.streamConsistencyCondition == nil ||
			len(vreq.request.streamConsistencyCondition.Criteria) == 0 {
			validRequests = append(validRequests, vreq)
			continue
		}

		// Check if any events in batch match this request's criteria
		hasMatchingEvents := q.hasMatchingInBatchEvents(
			reqIdx,
			vreq.request.streamConsistencyCondition.Criteria,
			tagIndex,
		)

		// If any events match, fail immediately
		if hasMatchingEvents {
			latestPos, exists := state.latestPositions[vreq.criteriaHash]
			if !exists {
				latestPos = &orisun.Position{CommitPosition: -1, PreparePosition: -1}
			}

			// Get expected position (may be nil)
			var expectedTxID, expectedGID int64 = -1, -1
			if vreq.request.expectedPosition != nil {
				expectedTxID = vreq.request.expectedPosition.CommitPosition
				expectedGID = vreq.request.expectedPosition.PreparePosition
			}

			q.sendResult(vreq.request, saveResult{
				"",
				0,
				q.createConcurrencyError(
					expectedTxID,
					expectedGID,
					latestPos.CommitPosition,
					latestPos.PreparePosition,
				),
			})
			continue
		}

		// Request passed cross-batch validation
		validRequests = append(validRequests, vreq)
	}

	return validRequests
}

// fastStringConversion converts interface{} to string faster than fmt.Sprintf
// Optimized for the hot path of tag value conversion
func fastStringConversion(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		// Fallback for complex types - still use fmt.Sprintf
		return fmt.Sprintf("%v", v)
	}
}

// hasMatchingInBatchEvents checks if any events in the batch match the given criteria
// Tags within a criterion are ANDed, criteria are ORed
// Returns true if ANY criterion matches (OR across criteria, AND within each criterion)
func (q *BatchedQueuedEventStore) hasMatchingInBatchEvents(
	excludeReqIdx int,
	criteria []*orisun.Criterion,
	tagIndex map[string][]eventLocation,
) bool {
	// For each criterion, check if events match all tags in that criterion (AND logic)
	// If any criterion has matching events, return true (OR logic across criteria)
	for _, criterion := range criteria {
		if q.matchesCriterion(excludeReqIdx, criterion, tagIndex) {
			return true // This criterion matched, so overall criteria match (OR logic)
		}
	}

	return false // No criteria matched
}

// matchesCriterion checks if any events match all tags in a single criterion (AND logic)
func (q *BatchedQueuedEventStore) matchesCriterion(
	excludeReqIdx int,
	criterion *orisun.Criterion,
	tagIndex map[string][]eventLocation,
) bool {
	if len(criterion.Tags) == 0 {
		return false
	}

	// Build list of tag keys for this criterion - pre-allocate for performance
	tagKeys := make([]string, 0, len(criterion.Tags))
	for _, tag := range criterion.Tags {
		// Build tag key efficiently without fmt.Sprintf
		tagKeys = append(tagKeys, tag.Key+":"+tag.Value)
	}

	// Start with events matching first tag
	firstTagEvents, exists := tagIndex[tagKeys[0]]
	if !exists {
		return false // No events match first tag
	}

	// Build a set of events matching first tag (excluding current request)
	matchingEvents := make(map[eventLocation]bool, len(firstTagEvents))
	for _, loc := range firstTagEvents {
		if loc.requestIdx != excludeReqIdx {
			matchingEvents[loc] = true
		}
	}

	if len(matchingEvents) == 0 {
		return false
	}

	// Intersect with events matching remaining tags (AND logic within criterion)
	for i := 1; i < len(tagKeys); i++ {
		tagEvents, exists := tagIndex[tagKeys[i]]
		if !exists {
			return false // No events match this tag
		}

		// Build new intersection
		intersection := make(map[eventLocation]bool)
		for _, loc := range tagEvents {
			if loc.requestIdx != excludeReqIdx && matchingEvents[loc] {
				intersection[loc] = true
			}
		}

		matchingEvents = intersection
		if len(matchingEvents) == 0 {
			return false // Empty intersection
		}
	}

	// Found at least one event matching all tags in this criterion
	return true
}

// batchInsertWithTx performs a single batch insert of all valid events using an existing transaction
func (q *BatchedQueuedEventStore) batchInsertWithTx(validRequests []validatedRequest, tx *sql.Tx) error {
	if len(validRequests) == 0 {
		return nil
	}

	// Generate a single transaction ID for this batch
	currentTxID := time.Now().UnixNano()

	// Single-pass: count events and marshal in one loop
	valuePlaceholders := make([]string, 0, len(validRequests)*100)
	insertArgs := make([]interface{}, 0, len(validRequests)*100*5) // 5 args per event
	requestEventCounts := make([]int, len(validRequests))

	currentTotalEvents := 0
	for reqIdx, vreq := range validRequests {
		startIdx := currentTotalEvents
		for _, event := range vreq.request.events {
			// Marshal data and metadata to JSON (sequential is fast enough!)
			dataJSON, err := json.Marshal(event.Data)
			if err != nil {
				q.logger.Errorf("Failed to marshal event data: %v", err)
				q.sendResult(vreq.request, saveResult{"", 0,
					status.Errorf(codes.Internal, "failed to marshal event data: %v", err)})
				return fmt.Errorf("failed to marshal event data")
			}

			metadataJSON, err := json.Marshal(event.Metadata)
			if err != nil {
				q.logger.Errorf("Failed to marshal event metadata: %v", err)
				q.sendResult(vreq.request, saveResult{"", 0,
					status.Errorf(codes.Internal, "failed to marshal event metadata: %v", err)})
				return fmt.Errorf("failed to marshal event metadata")
			}

			valuePlaceholders = append(valuePlaceholders, "(?, ?, ?, ?, ?)")
			insertArgs = append(insertArgs,
				currentTxID,
				event.EventId,
				event.EventType,
				string(dataJSON),
				string(metadataJSON),
			)

			currentTotalEvents++
		}
		requestEventCounts[reqIdx] = currentTotalEvents - startIdx
	}

	if len(valuePlaceholders) == 0 {
		return nil
	}

	// Build the batch insert query
	query := fmt.Sprintf(`
		INSERT INTO orisun_es_event (transaction_id, event_id, event_type, data, metadata)
		VALUES %s
	`, strings.Join(valuePlaceholders, ", "))

	// Execute batch insert in a single statement
	ctx := validRequests[0].request.ctx
	result, err := tx.ExecContext(ctx, query, insertArgs...)
	if err != nil {
		q.logger.Errorf("Failed to insert events: %v", err)
		// Send error to all requests
		for _, vreq := range validRequests {
			q.sendResult(vreq.request, saveResult{"", 0,
				status.Errorf(codes.Internal, "failed to insert events: %v", err)})
		}
		return fmt.Errorf("failed to insert events: %w", err)
	}

	// Get the last insert ID (global_id of the last inserted row)
	lastGlobalID, err := result.LastInsertId()
	if err != nil {
		q.logger.Errorf("Failed to get last insert ID: %v", err)
		// Send error to all requests
		for _, vreq := range validRequests {
			q.sendResult(vreq.request, saveResult{"", 0,
				status.Errorf(codes.Internal, "failed to get last insert ID: %v", err)})
		}
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		q.logger.Errorf("Failed to commit transaction: %v", err)
		// Send error to all requests
		for _, vreq := range validRequests {
			q.sendResult(vreq.request, saveResult{"", 0,
				status.Errorf(codes.Internal, "failed to commit transaction: %v", err)})
		}
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Success! Send results back to all requests
	baseGlobalID := lastGlobalID - int64(currentTotalEvents) + 1
	currentGlobalID := baseGlobalID

	for i := 0; i < len(validRequests); i++ {
		eventCount := requestEventCounts[i]
		if eventCount > 0 {
			// Send success result with the global ID of the last event in this request
			lastGIDForRequest := currentGlobalID + int64(eventCount) - 1
			q.sendResult(validRequests[i].request, saveResult{
				transactionID: fmt.Sprintf("%d", currentTxID),
				globalID:      lastGIDForRequest,
				err:           nil,
			})
			currentGlobalID += int64(eventCount)
		}
	}

	return nil
}

// sendResult sends a single result back to a request
func (q *BatchedQueuedEventStore) sendResult(req saveRequest, result saveResult) {
	select {
	case req.result <- result:
	default:
		q.logger.Warn("Failed to send save result: channel closed or full")
	}
}

// createConcurrencyError creates an optimistic concurrency error
func (q *BatchedQueuedEventStore) createConcurrencyError(expectedTxID, expectedGID, latestTxID, latestGID int64) error {
	return status.Error(codes.AlreadyExists,
		fmt.Sprintf("OptimisticConcurrencyException: Expected (%d, %d), Actual (%d, %d)",
			expectedTxID, expectedGID, latestTxID, latestGID))
}

// hashCriteria creates a simple hash from criteria for grouping
func hashCriteria(criteria []*orisun.Criterion) string {
	// Simple hash implementation - in production, use a proper hash function
	// Pre-allocate builder with reasonable capacity
	var b strings.Builder
	b.Grow(64) // Pre-allocate space for typical criteria

	for _, c := range criteria {
		for _, t := range c.Tags {
			b.WriteString(t.Key)
			b.WriteByte(':')
			b.WriteString(t.Value)
			b.WriteByte(';')
		}
	}
	return b.String()
}
