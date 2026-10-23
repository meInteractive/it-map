package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"itmap/internal/auth"
	"itmap/internal/catalog"
)

// DBChecker is only used to verify the active DB connection; the readiness gate
// remains closed until the entire API and parser have been accepted.
type DBChecker interface{ PingContext(context.Context) error }

// NewHandler returns the migration shell. No legacy business API returns success
// until its handler has been ported and tested against the frozen contract.
func NewHandler(logger *slog.Logger, database ...DBChecker) http.Handler {
	return NewHandlerWithWeb(logger, os.Getenv("WEB_DIST_DIR"), database...)
}

// NewHandlerWithWeb additionally serves built React static assets when configured.
func NewHandlerWithWeb(logger *slog.Logger, webDir string, database ...DBChecker) http.Handler {
	opts := Options{WebDir: webDir}
	if len(database) > 0 {
		opts.DB = database[0]
	}
	return NewHandlerWithOptions(logger, opts)
}

// Options wires auth and DB functionality independently during migration.
type Options struct {
	WebDir               string
	DB                   DBChecker
	Auth                 *auth.Manager
	Catalog              CatalogSearcher
	Compact              CompactCatalog
	Channels             ChannelLister
	Roles                RoleReader
	Vacancies            VacancyLister
	VacancyDetails       VacancyDetailer
	VacancyCreates       ManualVacancyCreator
	VacancyDeletes       ManualVacancyDeleter
	VacancyUpdates       ManualVacancyUpdater
	VacancyFlags         VacancyFlagWriter
	Applications         ApplicationReader
	ApplicationCompanies ApplicationCompanyReader
	ApplicationWrites    ApplicationMutator
	ApplicationCreates   ApplicationCreator
	CompanyFlags         CompanyFlagLister
	CompanyFlagWrites    CompanyFlagWriter
	CompanyCRM           CompanyCRMReader
	Tasks                TaskLister
	TaskCompletion       TaskCompletionWriter
}

// NewHandlerWithOptions constructs only explicitly implemented routes.
func NewHandlerWithOptions(logger *slog.Logger, opts Options) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, http.StatusOK, map[string]any{"status": "alive"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		reason := "mariadb_not_connected"
		if opts.DB != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := opts.DB.PingContext(ctx); err != nil {
				reason = "mariadb_unavailable"
			} else {
				reason = "business_api_not_migrated"
			}
		}
		jsonReply(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "reason": reason})
	})
	unmigrated := func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, http.StatusServiceUnavailable, map[string]any{"error": "backend_not_migrated"})
	}
	protected := unmigrated
	if opts.Auth != nil {
		mux.HandleFunc("POST /auth", LoginHandler(opts.Auth))
		mux.HandleFunc("GET /logout", LogoutHandler())
		mux.HandleFunc("GET /api/session", SessionHandler(opts.Auth))
		if opts.Catalog != nil {
			mux.HandleFunc("GET /api/companies/search", RequireSession(opts.Auth, CompanySearchHandler(opts.Catalog)))
			// v316 agencies/search is an alias to the company name search.
			mux.HandleFunc("GET /api/agencies/search", RequireSession(opts.Auth, CompanySearchHandler(opts.Catalog)))
		}
		if opts.Compact != nil {
			for _, entry := range []struct {
				prefix string
				kind   catalog.EntityType
			}{
				{"companies", catalog.CompanyType}, {"agencies", catalog.AgencyType},
			} {
				path := "/api/" + entry.prefix
				mux.HandleFunc("GET "+path+"/list", compactListHandler(opts.Auth, opts.Compact, entry.kind, catalog.ListView))
				mux.HandleFunc("GET "+path+"/map", compactListHandler(opts.Auth, opts.Compact, entry.kind, catalog.MapView))
				mux.HandleFunc("GET "+path+"/detail", compactDetailHandler(opts.Auth, opts.Compact, entry.kind, false))
				mux.HandleFunc("GET "+path+"/{id}", compactDetailHandler(opts.Auth, opts.Compact, entry.kind, true))
			}
		}
		if opts.Channels != nil {
			mux.HandleFunc("GET /api/channels", ChannelListHandler(opts.Auth, opts.Channels))
		}
		if opts.Tasks != nil {
			mux.HandleFunc("GET /api/tasks", TaskListHandler(opts.Auth, opts.Tasks))
		}
		if opts.CompanyCRM != nil {
			mux.HandleFunc("GET /api/crm", CompanyCRMHandler(opts.Auth, opts.CompanyCRM))
		}
		if opts.CompanyFlagWrites != nil {
			mux.HandleFunc("PUT /api/crm/favorite", CompanyFlagWriteHandler(opts.Auth, opts.CompanyFlagWrites, "favorite"))
			mux.HandleFunc("PUT /api/crm/hidden-company", CompanyFlagWriteHandler(opts.Auth, opts.CompanyFlagWrites, "hidden"))
		}
		if opts.CompanyFlags != nil {
			mux.HandleFunc("GET /api/crm/favorites", CompanyFlagListHandler(opts.Auth, opts.CompanyFlags, "favorite"))
			mux.HandleFunc("GET /api/crm/hidden-companies", CompanyFlagListHandler(opts.Auth, opts.CompanyFlags, "hidden"))
		}
		if opts.Applications != nil {
			mux.HandleFunc("GET /api/crm/applications", ApplicationsCRMHandler(opts.Auth, opts.Applications))
			mux.HandleFunc("GET /api/applications/{id}", ApplicationDetailHandler(opts.Auth, opts.Applications))
		}
		if opts.ApplicationCreates != nil {
			mux.HandleFunc("GET /api/applications/create-context", ApplicationCreateContextHandler(opts.Auth, opts.ApplicationCreates))
			mux.HandleFunc("POST /api/applications/create", ApplicationCreateHandler(opts.Auth, opts.ApplicationCreates))
			mux.HandleFunc("POST /api/applications", ApplicationCreateHandler(opts.Auth, opts.ApplicationCreates))
		}
		if opts.ApplicationCompanies != nil {
			mux.HandleFunc("GET /api/applications", CompanyApplicationsHandler(opts.Auth, opts.ApplicationCompanies))
			if opts.ApplicationWrites != nil {
				mux.HandleFunc("PUT /api/applications/{id}", ApplicationMutationHandler(opts.Auth, opts.ApplicationWrites, opts.ApplicationCompanies))
				mux.HandleFunc("DELETE /api/applications/{id}", ApplicationMutationHandler(opts.Auth, opts.ApplicationWrites, opts.ApplicationCompanies))
			}
		}
		if opts.TaskCompletion != nil {
			mux.HandleFunc("PUT /api/tasks/{id}", TaskCompletionHandler(opts.Auth, opts.TaskCompletion))
		}
		if opts.Vacancies != nil {
			mux.HandleFunc("GET /api/vacancies/list", VacancyListHandler(opts.Auth, opts.Vacancies, time.Now))
		}
		if opts.VacancyCreates != nil {
			mux.HandleFunc("POST /api/vacancies", ManualVacancyCreateHandler(opts.Auth, opts.VacancyCreates))
		}
		if opts.VacancyDeletes != nil {
			mux.HandleFunc("DELETE /api/vacancies/{id}", ManualVacancyDeleteHandler(opts.Auth, opts.VacancyDeletes))
		}
		if opts.VacancyUpdates != nil {
			mux.HandleFunc("PUT /api/vacancies/{id}", ManualVacancyUpdateHandler(opts.Auth, opts.VacancyUpdates))
		}
		if opts.VacancyFlags != nil {
			mux.HandleFunc("PUT /api/vacancies/{id}/state", VacancyStateHandler(opts.Auth, opts.VacancyFlags))
		}
		if opts.VacancyDetails != nil {
			mux.HandleFunc("GET /api/vacancies/{id}", VacancyDetailHandler(opts.Auth, opts.VacancyDetails))
		}
		if opts.Roles != nil {
			mux.HandleFunc("GET /api/vacancy-roles", VacancyRoleHandler(opts.Auth, opts.Roles, "all"))
			mux.HandleFunc("GET /api/vacancy-roles/roots", VacancyRoleHandler(opts.Auth, opts.Roles, "roots"))
			mux.HandleFunc("GET /api/vacancy-roles/leaves", VacancyRoleHandler(opts.Auth, opts.Roles, "leaves"))
			mux.HandleFunc("GET /api/vacancy-roles/{id}/descendants", VacancyRoleHandler(opts.Auth, opts.Roles, "descendants"))
		}
		protected = RequireSession(opts.Auth, unmigrated)
	}
	mux.HandleFunc("/api/", protected)
	mux.HandleFunc("/api", protected)
	mux.HandleFunc("/mcp", unmigrated)
	mux.HandleFunc("/mcp/", unmigrated)
	mux.HandleFunc("/auth/", unmigrated)
	mux.HandleFunc("/logout/", unmigrated)
	if opts.Auth == nil {
		mux.HandleFunc("/auth", unmigrated)
		mux.HandleFunc("/logout", unmigrated)
	}
	if opts.WebDir != "" {
		mux.Handle("/", StaticHandler(opts.WebDir))
	} else {
		mux.HandleFunc("/", unmigrated)
	}
	return recoverMiddleware(logger, safeHeaders(mux))
}

func jsonReply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func safeHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://*.openfreemap.org; connect-src 'self' https://*.openfreemap.org; worker-src 'self' blob:; font-src 'self' data: https://*.openfreemap.org; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func recoverMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				logger.Error("http request panic", "method", r.Method, "path", r.URL.Path)
				jsonReply(w, http.StatusInternalServerError, map[string]any{"error": "internal_error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
