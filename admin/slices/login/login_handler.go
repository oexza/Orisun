package login

import (
	"encoding/base64"
	"net/http"
	"orisun/admin/slices/common"
	l "orisun/logging"

	"github.com/goccy/go-json"

	globalCommon "orisun/common"

	datastar "github.com/starfederation/datastar/sdk/go"
)

type LoginHandler struct {
	logger        l.Logger
	boundary      string
	authenticator interface {
		ValidateCredentials(username string, password string) (globalCommon.User, error)
	}
}

func NewLoginHandler(
	logger l.Logger,
	boundary string,
	authenticator interface {
		ValidateCredentials(username string, password string) (globalCommon.User, error)
	},
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
	user, err := s.login(store.Username, store.Password)
	if err != nil {
		sse, _ := common.GetOrCreateSSEConnection(w, r)
		sse.RemoveFragments("message")
		sse.MergeFragments(`<div id="message">` + `Login Failed` + `</div>`)
		return
	}

	userAsString, err := json.Marshal(user)
	if err != nil {
		sse, _ := common.GetOrCreateSSEConnection(w, r)
		sse.MergeFragments(`<div id="message">` + `Login Failed` + `</div>`)
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

	sse, _ := common.GetOrCreateSSEConnection(w, r)

	sse.MergeFragments(`<div id="message">` + `Login Succeded` + `</div>`)

	// Redirect to users page after successful login
	sse.Redirect("/dashboard")
}

func (s *LoginHandler) login(username, password string) (globalCommon.User, error) {
	user, err := s.authenticator.ValidateCredentials(username, password)

	if err != nil {
		return globalCommon.User{}, err
	}

	return user, nil
}
