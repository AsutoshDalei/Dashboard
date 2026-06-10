package middleware

import (
	"net/http"
	"strings"
)

type SessionValidator interface {
	Validate(token string) bool
}

func RequireAuth(store SessionValidator) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session_token")
			if err != nil || !store.Validate(cookie.Value) {
				if strings.HasPrefix(r.URL.Path, "/api/") {
					RespondJSON(w, http.StatusUnauthorized, false, "authentication required", "")
					return
				}
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			next(w, r)
		}
	}
}