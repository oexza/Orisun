package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	create_user "orisun/admin/slices/create_user"
	dashboard "orisun/admin/slices/dashboard"
	"orisun/admin/slices/delete_user"
	login "orisun/admin/slices/login"
	"orisun/admin/slices/users_page"
	l "orisun/logging"
	"orisun/admin/assets"

	globalCommon "orisun/common"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type AdminServer struct {
	logger            l.Logger
	router            *chi.Mux
	createUserHandler *create_user.CreateUserHandler
	dashboardHandler  *dashboard.DashboardHandler
	loginHandler      *login.LoginHandler
	deleteUserHandler *delete_user.DeleteUserHandler
	usersHandler      *users_page.UsersPageHandler
}

func NewAdminServer(
	logger l.Logger,
	createUserHandler *create_user.CreateUserHandler,
	dashboardHandler *dashboard.DashboardHandler,
	loginHandler *login.LoginHandler,
	deleteUserHandler *delete_user.DeleteUserHandler,
	usersHandler *users_page.UsersPageHandler) (*AdminServer, error) {

	router := chi.NewRouter()

	server := &AdminServer{
		logger:            logger,
		router:            router,
		createUserHandler: createUserHandler,
		dashboardHandler:  dashboardHandler,
		loginHandler:      loginHandler,
		deleteUserHandler: deleteUserHandler,
		usersHandler:      usersHandler,
	}

	// Register routes
	router.Route("/", func(r chi.Router) {
		// Apply the tab ID middleware only to routes under /admin
		r.Use(server.tabIDMiddleware)

		// Serve static files
		r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(assets.GetFileSystem())))

		r.Get("/dashboard", withAuthentication(server.dashboardHandler.HandleDashboardPage))
		r.Get("/login", server.loginHandler.HandleLoginPage)
		r.Post("/login", server.loginHandler.HandleLogin)
		r.Get("/logout", server.handleLogout)

		r.Get("/users", withAuthentication(server.usersHandler.HandleUsersPage))
		r.Post("/users", withAuthentication(server.createUserHandler.HandleCreateUser))
		r.Get("/users/add", withAuthentication(server.createUserHandler.HandleCreateUserPage))
		// r.Get("/users/list", withAuthentication(server.handleUsersList))
		r.Delete("/users/{userId}/delete", withAuthentication(server.deleteUserHandler.HandleUserDelete))
	})

	return server, nil
}

// Move the middleware to a method on AdminServer
func (s *AdminServer) tabIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tabId := ""
		tabIDCookie, err := r.Cookie(globalCommon.DatastarTabCookieKey)

		if err != nil {
			newTabId, err := uuid.NewUUID()
			if err != nil {
				s.logger.Error("Error generating tab ID", "error", err)
				http.Redirect(w, r, "/error", http.StatusSeeOther)
				return
			}
			tabId = newTabId.String()
		} else {
			tabId = tabIDCookie.Value
		}

		http.SetCookie(w, &http.Cookie{
			Name:     globalCommon.DatastarTabCookieKey,
			Value:    tabId,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
			Path:     "/",
		})

		ctx := context.WithValue(r.Context(), globalCommon.DatastarTabCookieKey, tabId)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
	http.Redirect(w, r, "/login", http.StatusSeeOther)
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
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		userStr, err := base64.StdEncoding.DecodeString(c.Value)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		var unmarshled globalCommon.User = globalCommon.User{}

		err = json.Unmarshal(userStr, &unmarshled)

		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		ctx := context.WithValue(r.Context(), globalCommon.UserContextKey, unmarshled)
		call(w, r.WithContext(ctx))
	}
}