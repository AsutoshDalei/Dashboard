package auth

import (
	"html/template"
	"net/http"
	"strings"
	"time"
)

type Handler struct {
	store    *SessionStore
	passkey  string
	tmpl     *template.Template
}

func NewHandler(store *SessionStore, passkey string, tmpl *template.Template) *Handler {
	return &Handler{
		store:   store,
		passkey: passkey,
		tmpl:    tmpl,
	}
}

type LoginData struct {
	Error string
	Year  int
}

func (h *Handler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if h.isAuthenticated(r) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := LoginData{Year: time.Now().Year()}
	if err := h.tmpl.ExecuteTemplate(w, "login.html", data); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderLoginError(w, "Invalid form submission")
		return
	}

	if !allowAuthAttempt(r) {
		h.renderLoginError(w, "Invalid access key")
		return
	}

	passkey := r.FormValue("passkey")
	expectedPasskey := strings.TrimSpace(h.passkey)
	if expectedPasskey == "" {
		h.renderLoginError(w, "Invalid access key")
		return
	}

	if passkey == "" || !ConstantTimePasskeyEqual(passkey, expectedPasskey) {
		h.renderLoginError(w, "Invalid access key")
		return
	}

	token := h.store.Create()
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   int(sessionDuration.Seconds()),
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *Handler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_token")
	if err == nil {
		h.store.Delete(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:   "session_token",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie("session_token")
	if err != nil {
		return false
	}
	return h.store.Validate(cookie.Value)
}

func (h *Handler) renderLoginError(w http.ResponseWriter, errorMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "login.html", LoginData{Error: errorMsg, Year: time.Now().Year()}); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}