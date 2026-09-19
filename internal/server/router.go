package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/arthurr0/backvault/internal/core"
)

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(s.recoverer)
	r.Use(s.requestLogger)
	r.Use(s.securityHeaders)

	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/metrics", s.handleMetrics)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(noStore)
		r.Use(s.mw.Resolve)
		r.Use(s.mw.CSRF)

		r.Get("/healthz", s.handleHealth)
		r.Get("/readyz", s.handleReady)
		r.Get("/setup/status", s.handleSetupStatus)
		r.Post("/setup", s.handleSetup)
		r.Post("/auth/login", s.handleLogin)

		r.Get("/docs", s.handleDocsIndex)
		r.Get("/docs/page", s.handleDocsPage)
		r.Get("/docs/search", s.handleDocsSearch)
		r.Get("/docs/assets/*", s.handleDocsAsset)

		r.Group(func(r chi.Router) {
			r.Use(s.mw.RequireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Get("/meta/version", s.handleMetaVersion)
			r.Get("/meta/sources", s.handleMetaSources)
			r.Get("/meta/destinations", s.handleMetaDestinations)
			r.Get("/meta/notifiers", s.handleMetaNotifiers)
			r.Get("/meta/tools", s.handleMetaTools)
			r.Get("/meta/timezones", s.handleMetaTimezones)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.mw.RequireScope(core.ScopeRead))

			r.Get("/dashboard", s.handleDashboard)
			r.Get("/events/stream", s.handleEventStream)

			r.Get("/sources", s.handleListSources)
			r.Get("/sources/{id}", s.handleGetSource)
			r.Get("/destinations", s.handleListDestinations)
			r.Get("/destinations/{id}", s.handleGetDestination)
			r.Get("/destinations/{id}/browse", s.handleBrowseDestination)

			r.Get("/jobs", s.handleListJobs)
			r.Get("/jobs/{id}", s.handleGetJob)
			r.Get("/jobs/{id}/runs", s.handleJobRuns)
			r.Get("/jobs/{id}/artifacts", s.handleJobArtifacts)
			r.Get("/jobs/{id}/schedule/preview", s.handleSchedulePreview)

			r.Get("/runs", s.handleListRuns)
			r.Get("/runs/{id}", s.handleGetRun)
			r.Get("/runs/{id}/log", s.handleRunLog)
			r.Get("/runs/{id}/log/stream", s.handleRunLogStream)

			r.Get("/artifacts", s.handleListArtifacts)
			r.Get("/artifacts/{id}", s.handleGetArtifact)
			r.Get("/artifacts/{id}/download", s.handleDownloadArtifact)

			r.Get("/notifications/channels", s.handleListChannels)
			r.Get("/notifications/channels/{id}", s.handleGetChannel)

			r.Get("/settings", s.handleGetSettings)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.mw.RequireScope(core.ScopeRun))
			r.Post("/jobs/{id}/run", s.handleRunJob)
			r.Post("/runs/{id}/cancel", s.handleCancelRun)
			r.Post("/artifacts/{id}/verify", s.handleVerifyArtifact)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.mw.RequireScope(core.ScopeIngest))
			r.Post("/ingest/{jobSlug}", s.handleIngest)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.mw.RequireScope(core.ScopeAdmin))

			r.Post("/sources", s.handleCreateSource)
			r.Put("/sources/{id}", s.handleUpdateSource)
			r.Delete("/sources/{id}", s.handleDeleteSource)
			r.Post("/sources/test", s.handleTestSourceConfig)
			r.Post("/sources/{id}/test", s.handleTestSource)

			r.Post("/destinations", s.handleCreateDestination)
			r.Put("/destinations/{id}", s.handleUpdateDestination)
			r.Delete("/destinations/{id}", s.handleDeleteDestination)
			r.Post("/destinations/test", s.handleTestDestinationConfig)
			r.Post("/destinations/{id}/test", s.handleTestDestination)

			r.Post("/jobs", s.handleCreateJob)
			r.Put("/jobs/{id}", s.handleUpdateJob)
			r.Delete("/jobs/{id}", s.handleDeleteJob)
			r.Post("/jobs/{id}/enable", s.handleEnableJob)
			r.Post("/jobs/{id}/disable", s.handleDisableJob)
			r.Post("/jobs/{id}/prune", s.handlePruneJob)
			r.Post("/jobs/{id}/duplicate", s.handleDuplicateJob)

			r.Delete("/artifacts/{id}", s.handleDeleteArtifact)
			r.Post("/artifacts/{id}/restore", s.handleRestoreArtifact)

			r.Post("/notifications/channels", s.handleCreateChannel)
			r.Put("/notifications/channels/{id}", s.handleUpdateChannel)
			r.Delete("/notifications/channels/{id}", s.handleDeleteChannel)
			r.Post("/notifications/channels/test", s.handleTestChannelConfig)
			r.Post("/notifications/channels/{id}/test", s.handleTestChannel)

			r.Put("/settings", s.handleUpdateSettings)

			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Get("/users/{id}", s.handleGetUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)

			r.Get("/tokens", s.handleListTokens)
			r.Post("/tokens", s.handleCreateToken)
			r.Delete("/tokens/{id}", s.handleDeleteToken)

			r.Get("/audit", s.handleListAudit)
			r.Get("/export", s.handleExport)
			r.Post("/import", s.handleImport)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.mw.RequireAuth)
			r.Put("/users/{id}/password", s.handleChangePassword)
		})

		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			s.writeError(w, r, http.StatusNotFound, codeNotFound, "no such endpoint: "+r.URL.Path)
		})
		r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			s.writeError(w, r, http.StatusMethodNotAllowed, codeBadRequest, "method not allowed")
		})
	})

	r.NotFound(s.handleStatic)
	r.MethodNotAllowed(s.handleStatic)
	return r
}
