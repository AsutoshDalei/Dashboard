package auth

import "net/http"

type Service interface {
	CreateSession() string
	ValidateSession(token string) bool
	DeleteSession(token string)
	IsAuthenticated(r *http.Request) bool
	Authenticate(w http.ResponseWriter, r *http.Request, passkey string) bool
}