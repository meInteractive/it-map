package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	userdocs "itmap/docs/user"
	"itmap/internal/auth"
)

type documentationSection struct {
	ID   string `json:"id"`
	File string `json:"file"`
}

func documentationHandler(manager *auth.Manager) http.HandlerFunc {
	manifest, err := userdocs.Files.ReadFile("manifest.json")
	var index struct {
		Sections []documentationSection `json:"sections"`
	}
	if err == nil {
		err = json.Unmarshal(manifest, &index)
	}
	sections := make(map[string]documentationSection, len(index.Sections))
	for _, section := range index.Sections {
		sections[section.ID] = section
	}
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		user, _ := sessionFromRequest(manager, r)
		if !user.IsAdmin {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			jsonReply(w, http.StatusMethodNotAllowed, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Документация временно недоступна."})
			return
		}
		path := r.URL.EscapedPath()
		if path == "/api/docs" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write(manifest)
			return
		}
		section, ok := sections[strings.TrimPrefix(path, "/api/docs/")]
		if !ok {
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Раздел документации не найден."})
			return
		}
		data, readErr := userdocs.Files.ReadFile(section.File)
		if readErr != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Документация временно недоступна."})
			return
		}
		disposition := "inline"
		if r.URL.Query().Get("download") == "1" {
			disposition = "attachment"
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", disposition+"; filename="+strconv.Quote(section.File))
		_, _ = w.Write(data)
	})
}
