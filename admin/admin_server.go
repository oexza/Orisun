package admin

import (
	"context"
	"encoding/base64"
	"github.com/oexza/Orisun/admin/assets"
	changepassword "github.com/oexza/Orisun/admin/slices/change_password"
	create_user "github.com/oexza/Orisun/admin/slices/create_user"
	dashboard "github.com/oexza/Orisun/admin/slices/dashboard"
	"github.com/oexza/Orisun/admin/slices/delete_user"
	"github.com/oexza/Orisun/admin/slices/login"
	"github.com/oexza/Orisun/admin/slices/users_page"
	l "github.com/oexza/Orisun/logging"
	"net/http"

	"github.com/goccy/go-json"

	"github.com/oexza/Orisun/orisun"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type AdminServer struct {
	logger                l.Logger
	router                *chi.Mux
	createUserHandler     *create_user.CreateUserHandler
	dashboardHandler      *dashboard.DashboardHandler
	loginHandler          *login.LoginHandler
	deleteUserHandler     *delete_user.DeleteUserHandler
	usersHandler          *users_page.UsersPageHandler
	changePasswordHandler *changepassword.ChangePasswordHandler
}

func NewAdminServer(
	logger l.Logger,
	createUserHandler *create_user.CreateUserHandler,
	dashboardHandler *dashboard.DashboardHandler,
	loginHandler *login.LoginHandler,
	deleteUserHandler *delete_user.DeleteUserHandler,
	usersHandler *users_page.UsersPageHandler,
	changePasswordHandler *changepassword.ChangePasswordHandler,
) (*AdminServer, error) {

	router := chi.NewRouter()

	server := &AdminServer{
		logger:                logger,
		router:                router,
		createUserHandler:     createUserHandler,
		dashboardHandler:      dashboardHandler,
		loginHandler:          loginHandler,
		deleteUserHandler:     deleteUserHandler,
		usersHandler:          usersHandler,
		changePasswordHandler: changePasswordHandler,
	}

	// Register routes
	router.Route("/", func(r chi.Router) {
		r.Use(server.tabIDMiddleware)

		// Serve static files
		r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(assets.GetFileSystem())))

		// Public routes
		r.Group(func(public chi.Router) {
			public.Get("/login", server.loginHandler.HandleLoginPage)
			public.Post("/login", server.loginHandler.HandleLogin)
			public.Get("/logout", server.handleLogout)
		})

		// Protected routes
		r.Group(func(protected chi.Router) {
			// Apply authentication middleware to all routes in this group
			protected.Use(server.authMiddleware)

			//redirect to /dashboard
			protected.Get("/", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			})
			protected.Get("/dashboard", server.dashboardHandler.HandleDashboardPage)
			protected.Get("/users", server.usersHandler.HandleUsersPage)
			protected.Get("/users/sse", server.usersHandler.HandleUsersPageSSE)
			protected.Post("/users", server.createUserHandler.HandleCreateUser)
			protected.Get("/users/add", server.createUserHandler.HandleCreateUserPage)
			protected.Delete("/users/{userId}/delete", server.deleteUserHandler.HandleUserDelete)
			protected.Get("/change-password", server.changePasswordHandler.HandleChangePasswordPage)
			protected.Post("/change-password", server.changePasswordHandler.HandleChangePassword)
		})
	})

	return server, nil
}

// Convert withAuthentication to a middleware method on AdminServer
func (s *AdminServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for authentication token
		c, err := r.Cookie("auth")
		if err != nil {
			s.logger.Error("Authentication error", "error", err)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		userStr, err := base64.StdEncoding.DecodeString(c.Value)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		var unmarshled orisun.User = orisun.User{}
		err = json.Unmarshal(userStr, &unmarshled)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		ctx := context.WithValue(r.Context(), orisun.UserContextKey, unmarshled)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *AdminServer) tabIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tabId := ""
		tabIDCookie, err := r.Cookie(orisun.DatastarTabCookieKey.String())

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
			Name:     orisun.DatastarTabCookieKey.String(),
			Value:    tabId,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
			Path:     "/",
		})

		ctx := context.WithValue(r.Context(), orisun.DatastarTabCookieKey, orisun.DatastarTabCookieKeyType(tabId))
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
