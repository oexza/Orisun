package admin

import (
	"context"
	"fmt"
	admin_events "orisun/admin/events"
	admin_common "orisun/admin/slices/common"
	globalCommon "orisun/common"
	"orisun/eventstore"
	"sync"
	"time"

	logger "orisun/logging"

	"github.com/goccy/go-json"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

type Authenticator struct {
	boundary  string
	getEvents admin_common.GetEventsType
	logger    logger.Logger
	// getUserByUsername func(username string) (globalCommon.User, error)
}

func NewAuthenticator(getEvents admin_common.GetEventsType, logger logger.Logger, boundary string) *Authenticator {
	return &Authenticator{
		boundary:  boundary,
		getEvents: getEvents,
		logger:    logger,
		// getUserByUsername: getUserByUsername,
	}
}

var userByIdCache = map[string]*globalCommon.User{}
var userByUsernameCache = map[string]*globalCommon.User{}
var cacheMutex sync.RWMutex

func (a *Authenticator) ValidateCredentials(ctx context.Context, username string, password string) (globalCommon.User, error) {
	// Check if the user is in the cache
	cacheMutex.RLock()
	user, ok := userByUsernameCache[username]
	cacheMutex.RUnlock()
	if ok && user.Username == username {
		// Compare the provided password with the stored hash
		if err := admin_common.ComparePassword(user.HashedPassword, password); err != nil {
			return globalCommon.User{}, fmt.Errorf("invalid credentials")
		}
		return *user, nil
	}

	// Use goroutines to fetch events concurrently
	var userCreated *eventstore.GetEventsResponse
	var userDeleted *eventstore.GetEventsResponse
	var passwordChanged *eventstore.GetEventsResponse

	// Create a common request template
	baseRequest := eventstore.GetEventsRequest{
		Boundary:  a.boundary,
		Count:     1,
		Direction: eventstore.Direction_DESC,
		Stream: &eventstore.GetStreamQuery{
			Name:        admin_events.AdminStream,
			FromVersion: 999999999999999999,
		},
	}

	// Fetch UserCreated event synchronously (required to get user ID)
	// Create a true copy of the base request, then take its address for getEvents
	reqCopy := baseRequest
	reqCopy.Query = &eventstore.Query{
		Criteria: []*eventstore.Criterion{
			{
				Tags: []*eventstore.Tag{
					{Key: "username", Value: username},
					{Key: "eventType", Value: admin_events.EventTypeUserCreated},
				},
			},
		},
	}
	var err error
	userCreated, err = a.getEvents(ctx, &reqCopy)
	if err != nil {
		return globalCommon.User{}, err
	}

	if userCreated == nil || len(userCreated.Events) == 0 {
		return globalCommon.User{}, fmt.Errorf("User Not found, Please contact the administrator")
	}

	var userCreatedEventData admin_events.UserCreated
	err = json.Unmarshal([]byte(userCreated.Events[0].Data), &userCreatedEventData)
	if err != nil {
		return globalCommon.User{}, err
	}

	// Concurrently fetch UserDeleted and PasswordChanged using errgroup
	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		// UserDeleted
		req := baseRequest
		req.Query = &eventstore.Query{
			Criteria: []*eventstore.Criterion{
				{
					Tags: []*eventstore.Tag{
						{Key: "user_id", Value: userCreatedEventData.UserId},
						{Key: "eventType", Value: admin_events.EventTypeUserDeleted},
					},
				},
			},
		}
		resp, err := a.getEvents(gctx, &req)
		if err != nil {
			return err
		}
		userDeleted = resp
		return nil
	})

	g.Go(func() error {
		// PasswordChanged
		req := baseRequest
		req.Query = &eventstore.Query{
			Criteria: []*eventstore.Criterion{
				{
					Tags: []*eventstore.Tag{
						{Key: "user_id", Value: userCreatedEventData.UserId},
						{Key: "eventType", Value: admin_events.EventTypeUserPasswordChanged},
					},
				},
			},
		}
		resp, err := a.getEvents(gctx, &req)
		if err != nil {
			return err
		}
		passwordChanged = resp
		return nil
	})

	if err := g.Wait(); err != nil {
		return globalCommon.User{}, err
	}

	// Check if user is deleted
	if userDeleted != nil && len(userDeleted.Events) > 0 {
		return globalCommon.User{}, fmt.Errorf("user Not found, Please contact the administrator")
	}

	// Determine which password hash to use for verification
	var currentPasswordHash string

	// If there's a password changed event, use the most recent password hash
	if passwordChanged != nil && len(passwordChanged.Events) > 0 {
		var passwordChangedEventData admin_events.UserPasswordChanged
		err := json.Unmarshal([]byte(passwordChanged.Events[0].Data), &passwordChangedEventData)
		if err != nil {
			return globalCommon.User{}, err
		}
		currentPasswordHash = passwordChangedEventData.PasswordHash
	} else {
		// Otherwise use the original password from user creation
		currentPasswordHash = userCreatedEventData.PasswordHash
	}

	// Compare the provided password with the stored hash
	if err := admin_common.ComparePassword(currentPasswordHash, password); err != nil {
		return globalCommon.User{}, fmt.Errorf("invalid credentials")
	}

	user = &globalCommon.User{
		Id:             userCreatedEventData.UserId,
		Username:       username,
		Name:           userCreatedEventData.Name,
		HashedPassword: currentPasswordHash,
		Roles:          userCreatedEventData.Roles,
	}

	// Cache the user
	cacheMutex.Lock()
	userByIdCache[user.Id] = user
	userByUsernameCache[userCreatedEventData.Username] = user
	cacheMutex.Unlock()

	return *user, nil
}

func (a *Authenticator) HasRole(roles []globalCommon.Role, requiredRole globalCommon.Role) bool {
	for _, role := range roles {
		if role == requiredRole || role == globalCommon.RoleAdmin {
			return true
		}
	}
	return false
}

type AuthUserProjector struct {
	boundary          string
	logger            logger.Logger
	subscribeToEvents admin_common.SubscribeToEventStoreType
}

func NewAuthUserProjector(
	logger logger.Logger,
	subscribeToEvents admin_common.SubscribeToEventStoreType,
	boundary string,
) *AuthUserProjector {
	return &AuthUserProjector{
		boundary:          boundary,
		logger:            logger,
		subscribeToEvents: subscribeToEvents,
	}
}

func (p *AuthUserProjector) Start(ctx context.Context) error {
	var projectorName = "auth-user-projector-" + uuid.New().String()
	p.logger.Info("Starting auth user projector %s", projectorName)

	stream := globalCommon.NewMessageHandler[eventstore.Event](ctx)

	go func() {
		for {
			p.logger.Debugf("Receiving events for: %s", projectorName)
			event, err := stream.Recv()
			if err != nil {
				p.logger.Error("Error receiving event: %v", err)
				continue
			}

			for {
				if err := p.handleEvent(event); err != nil {
					p.logger.Error("Error handling event: %v", err)

					time.Sleep(5 * time.Second)
					continue
				}
				break
			}
		}
	}()
	// Subscribe from last checkpoint
	err := p.subscribeToEvents(
		ctx,
		p.boundary,
		projectorName,
		nil,
		nil,
		*stream,
	)
	if err != nil {
		return err
	}
	return nil
}

func (p *AuthUserProjector) handleEvent(event *eventstore.Event) error {
	p.logger.Debugf("Handling event %v", event)

	switch event.EventType {
	case admin_events.EventTypeUserPasswordChanged:
		var userEvent admin_events.UserPasswordChanged
		if err := json.Unmarshal([]byte(event.Data), &userEvent); err != nil {
			return err
		}

		if userByIdCache[userEvent.UserId] != nil {
			p.logger.Debugf("Updating password hash for user %s", userEvent.UserId)
			cacheMutex.Lock()
			defer cacheMutex.Unlock()
			userByIdCache[userEvent.UserId].HashedPassword = userEvent.PasswordHash
			userByUsernameCache[userByIdCache[userEvent.UserId].Username].HashedPassword = userEvent.PasswordHash
		}

	case admin_events.EventTypeUserDeleted:
		var userEvent admin_events.UserDeleted
		if err := json.Unmarshal([]byte(event.Data), &userEvent); err != nil {
			return err
		}

		cacheMutex.Lock()
		defer cacheMutex.Unlock()
		if userByIdCache[userEvent.UserId] != nil {
			p.logger.Infof("Deleting user %s from cache", userEvent.UserId)
			delete(userByUsernameCache, userByIdCache[userEvent.UserId].Username)
			delete(userByIdCache, userEvent.UserId)
		}
	}
	return nil
}
