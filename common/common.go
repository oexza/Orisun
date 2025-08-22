package common

import "context"

type UserContextKeyType string

const UserContextKey UserContextKeyType = "user"

type DatastarTabCookieKeyType string

const DatastarTabCookieKey DatastarTabCookieKeyType = DatastarTabCookieKeyType("tab")
func (k DatastarTabCookieKeyType) String() string {
	return string(k)
}

type Role string

const (
	RoleAdmin      Role = "ADMIN"
	RoleOperations Role = "OPERATIONS"
)

var Roles = []Role{RoleAdmin, RoleOperations}

func (r Role) String() string {
	return string(r)
}

type User struct {
	Id             string `json:"id"`
	Name           string `json:"name"`
	Username       string `json:"username"`
	HashedPassword string `json:"-"`
	Roles          []Role `json:"roles"`
}

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
