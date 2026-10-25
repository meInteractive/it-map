package httpapi

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"itmap/internal/auth"
)

func sessionFromRequest(m *auth.Manager, r *http.Request) (auth.User, bool) {
	if m == nil {
		return auth.User{}, false
	}
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil {
		return auth.User{}, false
	}
	return m.Verify(cookie.Value, time.Now())
}

// RequireSession returns 401 for unauthenticated API calls, matching the
// legacy JS contract, but still leaves unported authenticated APIs as 503.
func RequireSession(m *auth.Manager, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := sessionFromRequest(m, r); !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]any{"error": "Требуется войти в систему."})
			return
		}
		next(w, r)
	}
}

func SessionHandler(m *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]any{"error": "Требуется войти в систему."})
			return
		}
		jsonReply(w, http.StatusOK, u)
	}
}

func LoginHandler(m *auth.Manager) http.HandlerFunc {
	return loginHandler(m, "")
}

func loginHandler(m *auth.Manager, webDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Never trust proxy-supplied X-Forwarded-* values for same-origin checks.
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || parsed.Host != r.Host || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Path != "" {
				jsonReply(w, http.StatusForbidden, map[string]any{"error": "Недопустимый источник запроса."})
				return
			}
		}
		if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
			jsonReply(w, http.StatusForbidden, map[string]any{"error": "Недопустимый источник запроса."})
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			jsonReply(w, http.StatusBadRequest, map[string]any{"error": "Некорректные данные."})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if err := r.ParseForm(); err != nil {
			jsonReply(w, http.StatusBadRequest, map[string]any{"error": "Некорректные данные."})
			return
		}
		user, ok := m.Authenticate(r.PostForm.Get("login"), r.PostForm.Get("password"))
		if !ok {
			w.Header().Set("X-App-Login-Error", "1")
			w.Header().Set("Cache-Control", "private, no-store")
			if webDir != "" {
				serveIndexHTML(w, r, os.DirFS(webDir), http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		token, err := m.Issue(user, time.Now())
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]any{"error": "auth unavailable"})
			return
		}
		http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: token, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: int(auth.SessionTTL.Seconds())})
		w.Header().Set("Cache-Control", "private, no-store")
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func LogoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		w.Header().Set("Cache-Control", "private, no-store")
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}
