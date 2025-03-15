package admin

import (
	// "bytes"
	"encoding/json"
	"errors"
	"fmt"

	"html/template"
	"net/http"
	pb "orisun/src/orisun/eventstore"
	l "orisun/src/orisun/logging"
	"strings"

	"orisun/src/orisun/admin/templates"
	"orisun/src/orisun/admin/templates/layout"

	"encoding/base64"

	"github.com/go-chi/chi/v5"
	datastar "github.com/starfederation/datastar/sdk/go"
)

type contextKey string

const (
	contextKeyUser   = contextKey("user")
	userStreamPrefix = "User-Registration:::::"
	registrationTag  = "Registration"
	usernameTag      = "Registration_username"
)

type DB interface {
	ListAdminUsers() ([]*User, error)
	GetProjectorLastPosition(projectorName string) (*pb.Position, error)
	UpdateProjectorPosition(name string, position *pb.Position) error
	CreateNewUser(id string, username string, password_hash string, name string, roles []Role) error
	DeleteUser(id string) error
	GetUserByUsername(username string) (User, error)
}

type AdminServer struct {
	logger               l.Logger
	tmpl                 *template.Template
	router               *chi.Mux
	eventStore           *pb.EventStore
	adminCommandHandlers AdminCommandHandlers
}

func NewAdminServer(logger l.Logger, eventStore *pb.EventStore, adminCommandHandlers AdminCommandHandlers) (*AdminServer, error) {
	funcMap := template.FuncMap{
		"join": strings.Join,
	}

	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFS(content, "templates/*.html"))

	// Add debug logging
	for _, t := range tmpl.Templates() {
		logger.Infof("Loaded template: %s", t.Name())
	}

	router := chi.NewRouter()

	server := &AdminServer{
		logger:               logger,
		tmpl:                 tmpl,
		router:               router,
		eventStore:           eventStore,
		adminCommandHandlers: adminCommandHandlers,
	}

	var userExistsError UserExistsError
	if _, err := adminCommandHandlers.createUser("admin", "admin", "changeit", []Role{RoleAdmin}); err != nil && !errors.As(err, &userExistsError) {
		return nil, err
	}

	// Register routes
	router.Route("/admin", func(r chi.Router) {
		// Add login routes
		r.Get("/dashboard", withAuthentication(server.handleDashboard))
		r.Get("/login", server.handleLoginPage)
		r.Post("/login", server.handleLogin)
		r.Get("/logout", server.handleLogout) // Added logout route

		// Existing routes
		r.Get("/users", withAuthentication(server.handleUsers))
		r.Post("/users", withAuthentication(server.handleCreateUser))
		r.Get("/users/add", withAuthentication(server.handleCreateUserPage))
		// r.Get("/users/list", withAuthentication(server.handleUsersList))
		r.Delete("/users/{userId}/delete", withAuthentication(server.handleUserDelete))
	})

	return server, nil
}

func (s *AdminServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	templates.Dashboard(r.URL.Path).Render(r.Context(), w)
}

func (s *AdminServer) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	err := templates.Login().Render(r.Context(), w)

	if err != nil {
		s.logger.Errorf("Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

type LoginRequest struct {
	Username string
	Password string
}

func (s *AdminServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	store := &LoginRequest{}
	if err := datastar.ReadSignals(r, store); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate credentials
	user, err := s.adminCommandHandlers.login(store.Username, store.Password)
	if err != nil {
		sse := datastar.NewSSE(w, r)
		sse.RemoveFragments("message")
		sse.MergeFragments(`<div id="message">` + `Login Failed` + `</div>`)
		return
	}

	userAsString, err := json.Marshal(user)
	if err != nil {
		sse := datastar.NewSSE(w, r)
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
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	})

	sse := datastar.NewSSE(w, r)

	sse.MergeFragments(`<div id="message">` + `Login Succeded` + `</div>`)

	// Redirect to users page after successful login
	sse.Redirect("/admin/dashboard")
}

func (s *AdminServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Clear the auth cookie by setting an expired cookie with the same name
	http.SetCookie(w, &http.Cookie{
		Name:     "auth",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})

	// Redirect to login page
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (s *AdminServer) handleCreateUserPage(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)

	// Convert []Role to []string for template compatibility
	roleStrings := make([]string, len(Roles))
	for i, role := range Roles {
		roleStrings[i] = string(role)
	}

	sse.MergeFragmentTempl(templates.AddUser(r.URL.Path, roleStrings), datastar.WithMergeMode(datastar.FragmentMergeModeOuter))
}

func (s *AdminServer) handleUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.adminCommandHandlers.listUsers()
	if err != nil {
		s.logger.Debugf("Failed to list users: %v", err)
		http.Error(w, "Failed to list users", http.StatusInternalServerError)
		return
	}

	// Convert internal user type to template user type
	templateUsers := make([]templates.User, len(users))
	for i, user := range users {
		// Convert []Role to []string for template compatibility
		roles := make([]string, len(user.Roles))
		for j, role := range user.Roles {
			roles[j] = string(role)
		}

		templateUsers[i] = templates.User{
			Name:     user.Name,
			Id:       user.Id,
			Username: user.Username,
			Roles:    roles,
		}
	}

	currentUser, err := getCurrentUser(r)
	if err != nil {
		s.logger.Debugf("Failed to get current user: %v", err)
		http.Error(w, "Failed to get current user", http.StatusInternalServerError)
		return
	}
	templates.Users(templateUsers, currentUser, r.URL.Path).Render(r.Context(), w)
}

type AddNewUserRequest struct {
	Name     string
	Username string
	Password string
	Role     string
}

func (r *AddNewUserRequest) validate() error {
	if r.Name == "" {
		return fmt.Errorf("name is required")
	}

	if r.Username == "" {
		return fmt.Errorf("username is required")
	}

	if len(r.Username) < 3 {
		return fmt.Errorf("username must be at least 3 characters")
	}

	if r.Password == "" {
		return fmt.Errorf("password is required")
	}

	if len(r.Password) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}

	if r.Role == "" {
		return fmt.Errorf("role is required")
	}

	// Check if role is valid
	validRole := false
	for _, role := range Roles {
		if strings.EqualFold(string(role), r.Role) {
			validRole = true
			break
		}
	}

	if !validRole {
		return fmt.Errorf("invalid role: %s", r.Role)
	}

	return nil
}

func (s *AdminServer) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	store := &AddNewUserRequest{}
	response := struct {
		Message string `json:"message"`
		Success bool   `json:"success"`
		Failed  bool   `json:"failed"`
	}{}

	if err := datastar.ReadSignals(r, store); err != nil {
		sse := datastar.NewSSE(w, r)
		response.Failed = true
		response.Message = err.Error()
		sse.MarshalAndMergeSignals(response)
		return
	}

	err := store.validate()

	if err != nil {
		sse := datastar.NewSSE(w, r)
		response.Failed = true
		response.Message = err.Error()
		sse.MarshalAndMergeSignals(response)
		return
	}

	s.logger.Debugf("Creating user %v", store)

	sse := datastar.NewSSE(w, r)

	evt, err := s.adminCommandHandlers.createUser(
		store.Name,
		store.Username,
		store.Password,
		[]Role{Role(strings.ToUpper(store.Role))},
	)
	if err != nil {
		response.Failed = true
		response.Message = err.Error()
		sse.MarshalAndMergeSignals(response)
		return
	}

	response.Success = true
	response.Message = "User created successfully"
	sse.MarshalAndMergeSignals(response)

	currentUser, err := getCurrentUser(r)
	if err != nil {
		response.Failed = true
		response.Message = err.Error()
		sse.MarshalAndMergeSignals(response)
		return
	}
	sse.MergeFragmentTempl(templates.UserRow(&templates.User{
		Name: evt.Name,
		Id:       evt.UserId,
		Username: evt.Username,
		Roles:    []string{store.Role},
	}, currentUser),
		datastar.WithMergeAppend(),
		datastar.WithSelectorID("users-table-body"),
	)
	sse.MergeFragmentTempl(layout.Alert("User created!", layout.AlertSuccess), datastar.WithSelector("body"),
		datastar.WithMergeMode(datastar.FragmentMergeModePrepend),
	)
	sse.ExecuteScript("document.querySelector('#alert').toast()")
	sse.ExecuteScript("document.querySelector('#add-user-dialog').hide()")
}

func (s *AdminServer) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	userId := chi.URLParam(r, "userId")
	currentUser, err := getCurrentUser(r)
	sse := datastar.NewSSE(w, r)

	if err != nil {
		sse.RemoveFragments("#alert")
		sse.MergeFragmentTempl(layout.Alert(err.Error(), layout.AlertDanger), datastar.WithSelector("body"),
			datastar.WithMergeMode(datastar.FragmentMergeModePrepend),
		)
		sse.ExecuteScript("document.querySelector('#alert').toast()")
		return
	}

	if err := s.adminCommandHandlers.deleteUser(userId, currentUser); err != nil {
		sse.RemoveFragments("#alert")
		sse.MergeFragmentTempl(layout.Alert(err.Error(), layout.AlertDanger), datastar.WithSelector("body"),
			datastar.WithMergeMode(datastar.FragmentMergeModePrepend),
		)
		// time.Sleep(1000 * time.Millisecond)
		sse.ExecuteScript("document.querySelector('#alert').toast()")
		return
	}
	sse.MergeFragmentTempl(layout.Alert("User Deleted", layout.AlertSuccess), datastar.WithSelector("body"),
		datastar.WithMergeMode(datastar.FragmentMergeModePrepend),
	)
	// time.Sleep(1000 * time.Millisecond)
	sse.ExecuteScript("document.querySelector('#alert').toast()")
	sse.RemoveFragments("#user_" + userId)
}

func (s *AdminServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func withAuthentication(call func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check for authentication token
		c, err := r.Cookie("auth")
		if err != nil {
			fmt.Errorf("Template execution error: %v", err)
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		fmt.Errorf("Cookie is : %v", c)

		call(w, r)
	}
}

// In getCurrentUser function
func getCurrentUser(r *http.Request) (string, error) {
	cookie, err := r.Cookie("auth")
	if err != nil {
		return "", err
	}

	// Base64 decode the cookie value
	decodedBytes, err := base64.StdEncoding.DecodeString(cookie.Value)
	if err != nil {
		return "", err
	}

	var user User
	if err := json.Unmarshal(decodedBytes, &user); err != nil {
		return "", err
	}

	if user.Id == "" {
		return "", fmt.Errorf("user ID is empty")
	}

	return user.Id, nil
}
