package login

import (
	"context"
	"encoding/base64"
	"github.com/goccy/go-json"
	admin_common "github.com/oexza/Orisun/admin/slices/common"
	globalCommon "github.com/oexza/Orisun/common"
	l "github.com/oexza/Orisun/logging"
	datastar "github.com/starfederation/datastar-go/datastar"
	"net/http"
)

type Authenticator interface {
	ValidateCredentials(ctx context.Context, username string, password string) (globalCommon.User, error)
}

type LoginHandler struct {
	logger        l.Logger
	boundary      string
	authenticator Authenticator
}

func NewLoginHandler(
	logger l.Logger,
	boundary string,
	authenticator Authenticator,
) *LoginHandler {
	return &LoginHandler{
		logger:        logger,
		boundary:      boundary,
		authenticator: authenticator,
	}
}

type LoginRequest struct {
	Username string
	Password string
}

func (s *LoginHandler) HandleLoginPage(w http.ResponseWriter, r *http.Request) {
	err := Login().Render(r.Context(), w)

	if err != nil {
		s.logger.Errorf("Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func (s *LoginHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	store := &LoginRequest{}
	if err := datastar.ReadSignals(r, store); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate credentials
	user, err := s.login(
		r.Context(),
		store.Username,
		store.Password,
	)
	if err != nil {
		sse, _ := admin_common.GetOrCreateSSEConnection(w, r)
		sse.RemoveElement("message")
		sse.PatchElements(`<div id="message">` + `Login Failed` + `</div>`)
		return
	}

	userAsString, err := json.Marshal(user)
	if err != nil {
		sse, _ := admin_common.GetOrCreateSSEConnection(w, r)
		sse.PatchElements(`<div id="message">` + `Login Failed` + `</div>`)
		return
	}

	// Base64 encode the JSON string
	encodedValue := base64.StdEncoding.EncodeToString(userAsString)

	// Set the token as an HTTP-only cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "auth",
		Value:    encodedValue,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})

	sse, _ := admin_common.GetOrCreateSSEConnection(w, r)

	sse.PatchElements(`<div id="message">` + `Login Succeded` + `</div>`)

	// Redirect to users page after successful login
	sse.Redirect("/dashboard")
}

func (s *LoginHandler) login(ctx context.Context, username, password string) (globalCommon.User, error) {
	// Validate credentials
	user, err := s.authenticator.ValidateCredentials(ctx, username, password)
	if err != nil {
		return globalCommon.User{}, err
	}

	return user, nil
}
