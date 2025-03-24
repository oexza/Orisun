package common

import "context"

type MessageHandler[T any] struct {
	ctx    context.Context
	events chan *T
}

// Add constructor and methods for the generic stream
func NewMessageHandler[T any](ctx context.Context) *MessageHandler[T] {
	handler := &MessageHandler[T]{
		ctx:    ctx,
		events: make(chan *T, 100), // Buffer size of 100
	}
	go func() {
		<-ctx.Done()
		close(handler.events)
	}()
	return handler
}

func (s *MessageHandler[T]) Send(event *T) error {
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.events <- event:
		return nil
	}
}

func (s *MessageHandler[T]) Recv() (*T, error) {
	// Wait for either an event or context cancellation
	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case event, ok := <-s.events:
		if !ok {
			// Channel was closed
			return nil, context.Canceled
		}
		return event, nil
	}
}

func (s *MessageHandler[T]) Context() context.Context {
	return s.ctx
}
