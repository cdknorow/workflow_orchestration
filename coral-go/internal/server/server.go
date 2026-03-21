// Package server provides the HTTP server, router, and middleware for Coral.
package server

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/cdknorow/coral/internal/background"
	"github.com/cdknorow/coral/internal/board"
	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/license"
	"github.com/cdknorow/coral/internal/ptymanager"
	"github.com/cdknorow/coral/internal/server/routes"
	"github.com/cdknorow/coral/internal/store"
)

// Frontend assets are embedded at build time. The directories must exist
// under internal/server/frontend/ even if empty (with .gitkeep placeholders).
// To serve the real Python frontend, copy src/coral/static/ and
// src/coral/templates/ into these directories before building.

//go:embed all:frontend/static
var staticFS embed.FS

//go:embed all:frontend/templates
var templateFS embed.FS

// Server holds dependencies and exposes the HTTP router.
type Server struct {
	cfg          *config.Config
	db           *store.DB
	boardStore   *board.Store
	backend      ptymanager.TerminalBackend
	licenseMgr   *license.Manager
	router       chi.Router
	indexTmpl    *template.Template
	diffTmpl     *template.Template
	tasksHandler *routes.TasksHandler
}

// templateData is passed to Go templates during rendering.
type templateData struct {
	CoralRoot string
}

// New creates a Server with all routes registered.
// If backend is nil, the server will use tmux-based terminal management.
func New(cfg *config.Config, db *store.DB, backend ptymanager.TerminalBackend) *Server {
	// Open the board store (separate SQLite DB)
	boardStore, err := board.NewStore(filepath.Join(cfg.CoralDir(), "messageboard.db"))
	if err != nil {
		log.Printf("Warning: failed to open board store: %v", err)
	}

	// Initialize license manager
	licenseMgr := license.NewManager(cfg.CoralDir())
	if licenseMgr.NeedsRevalidation() {
		log.Println("Revalidating license...")
		licenseMgr.Revalidate()
	}

	s := &Server{
		cfg:        cfg,
		db:         db,
		boardStore: boardStore,
		backend:    backend,
		licenseMgr: licenseMgr,
	}

	// Parse Go templates from embedded FS
	indexTmpl, err := template.ParseFS(templateFS,
		"frontend/templates/index.html",
		"frontend/templates/includes/sidebar.html",
		"frontend/templates/includes/modals.html",
		"frontend/templates/includes/views/live_session.html",
		"frontend/templates/includes/views/history_session.html",
		"frontend/templates/includes/views/message_board.html",
	)
	if err != nil {
		log.Printf("Warning: failed to parse index template: %v (serving placeholder)", err)
	}
	s.indexTmpl = indexTmpl

	diffTmpl, err := template.ParseFS(templateFS, "frontend/templates/diff.html")
	if err != nil {
		log.Printf("Warning: failed to parse diff template: %v (serving placeholder)", err)
	}
	s.diffTmpl = diffTmpl

	s.router = s.buildRouter()
	return s
}

// Router returns the configured chi.Router for use with http.Server.
func (s *Server) Router() chi.Router {
	return s.router
}

// SetScheduler injects the job scheduler into the tasks handler for launching/killing.
func (s *Server) SetScheduler(sched *background.JobScheduler) {
	if s.tasksHandler != nil {
		s.tasksHandler.SetScheduler(sched)
	}
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(cors.Handler(cors.Options{
		AllowOriginFunc: func(r *http.Request, origin string) bool {
			// Allow localhost origins only (matches Python CORS config)
			return isLocalhostOrigin(origin)
		},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	}))

	// License gate — disabled for now
	// r.Use(license.Middleware(s.licenseMgr))

	// License endpoints (ungated — must be accessible to activate)
	licRoutes := license.NewRoutes(s.licenseMgr)
	r.Post("/api/license/activate", licRoutes.Activate)
	r.Get("/api/license/status", licRoutes.Status)

	// ── API Routes ──────────────────────────────────────────────
	sessHandler := routes.NewSessionsHandler(s.db, s.cfg, s.backend, s.boardStore)
	sysHandler := routes.NewSystemHandler(s.db, s.cfg)
	histHandler := routes.NewHistoryHandler(s.db, s.cfg, s.boardStore)
	schedHandler := routes.NewScheduleHandler(s.db, s.cfg)
	whHandler := routes.NewWebhooksHandler(s.db, s.cfg)
	themeHandler := routes.NewThemesHandler(s.cfg)

	// Live sessions
	r.Get("/api/sessions/live", sessHandler.List)
	r.Get("/api/sessions/live/{name}", sessHandler.Detail)
	r.Get("/api/sessions/live/{name}/capture", sessHandler.Capture)
	r.Get("/api/sessions/live/{name}/poll", sessHandler.Poll)
	r.Get("/api/sessions/live/{name}/chat", sessHandler.Chat)
	r.Get("/api/sessions/live/{name}/info", sessHandler.Info)
	r.Get("/api/sessions/live/{name}/files", sessHandler.Files)
	r.Post("/api/sessions/live/{name}/files/refresh", sessHandler.RefreshFiles)
	r.Get("/api/sessions/live/{name}/diff", sessHandler.Diff)
	r.Get("/api/sessions/live/{name}/search-files", sessHandler.SearchFiles)
	r.Get("/api/sessions/live/{name}/git", sessHandler.Git)
	r.Post("/api/sessions/live/{name}/send", sessHandler.Send)
	r.Post("/api/sessions/live/{name}/keys", sessHandler.Keys)
	r.Post("/api/sessions/live/{name}/resize", sessHandler.Resize)
	r.Post("/api/sessions/live/{name}/kill", sessHandler.Kill)
	r.Post("/api/sessions/live/{name}/restart", sessHandler.Restart)
	r.Post("/api/sessions/live/{name}/resume", sessHandler.Resume)
	r.Post("/api/sessions/live/{name}/attach", sessHandler.Attach)
	r.Put("/api/sessions/live/{name}/display-name", sessHandler.SetDisplayName)
	r.Post("/api/sessions/launch", sessHandler.Launch)
	r.Post("/api/sessions/launch-team", sessHandler.LaunchTeam)

	// Agent tasks
	r.Get("/api/sessions/live/{name}/tasks", sessHandler.ListTasks)
	r.Post("/api/sessions/live/{name}/tasks", sessHandler.CreateTask)
	r.Patch("/api/sessions/live/{name}/tasks/{taskID}", sessHandler.UpdateTask)
	r.Delete("/api/sessions/live/{name}/tasks/{taskID}", sessHandler.DeleteTask)
	r.Post("/api/sessions/live/{name}/tasks/reorder", sessHandler.ReorderTasks)

	// Agent notes
	r.Get("/api/sessions/live/{name}/notes", sessHandler.ListNotes)
	r.Post("/api/sessions/live/{name}/notes", sessHandler.CreateNote)
	r.Patch("/api/sessions/live/{name}/notes/{noteID}", sessHandler.UpdateNote)
	r.Delete("/api/sessions/live/{name}/notes/{noteID}", sessHandler.DeleteNote)

	// Agent events
	r.Get("/api/sessions/live/{name}/events", sessHandler.ListEvents)
	r.Post("/api/sessions/live/{name}/events", sessHandler.CreateEvent)
	r.Get("/api/sessions/live/{name}/events/counts", sessHandler.EventCounts)
	r.Delete("/api/sessions/live/{name}/events", sessHandler.ClearEvents)

	// WebSocket
	r.Get("/ws/coral", sessHandler.WSCoral)
	r.Get("/ws/terminal/{name}", sessHandler.WSTerminal)

	// System / settings
	r.Get("/api/settings", sysHandler.GetSettings)
	r.Put("/api/settings", sysHandler.PutSettings)
	r.Get("/api/settings/default-prompts", sysHandler.GetDefaultPrompts)
	r.Get("/api/system/status", sysHandler.Status)
	r.Get("/api/system/update-check", sysHandler.UpdateCheck)
	r.Get("/api/filesystem/list", sysHandler.ListFilesystem)
	r.Post("/api/indexer/refresh", sysHandler.RefreshIndexer)

	// Tags
	r.Get("/api/tags", sysHandler.ListTags)
	r.Post("/api/tags", sysHandler.CreateTag)
	r.Delete("/api/tags/{tagID}", sysHandler.DeleteTag)
	r.Post("/api/sessions/{sessionID}/tags", sysHandler.AddSessionTag)
	r.Delete("/api/sessions/{sessionID}/tags/{tagID}", sysHandler.RemoveSessionTag)

	// Folder tags
	r.Get("/api/folder-tags", sysHandler.GetAllFolderTags)
	r.Get("/api/folder-tags/{folderName}", sysHandler.GetFolderTags)
	r.Post("/api/folder-tags/{folderName}", sysHandler.AddFolderTag)
	r.Delete("/api/folder-tags/{folderName}/{tagID}", sysHandler.RemoveFolderTag)

	// History
	r.Get("/api/sessions/history", histHandler.ListSessions)
	r.Get("/api/sessions/history/{sessionID}", histHandler.GetSessionDetail)
	r.Get("/api/sessions/history/{sessionID}/agent-notes", histHandler.GetSessionAgentNotes)
	r.Get("/api/sessions/{sessionID}/notes", histHandler.GetSessionNotes)
	r.Put("/api/sessions/{sessionID}/notes", histHandler.SaveSessionNotes)
	r.Post("/api/sessions/{sessionID}/resummarize", histHandler.Resummarize)
	r.Get("/api/sessions/{sessionID}/tags", histHandler.GetSessionTags)
	r.Get("/api/sessions/{sessionID}/git", histHandler.GetSessionGit)
	r.Get("/api/sessions/{sessionID}/events", histHandler.GetSessionEvents)
	r.Get("/api/sessions/{sessionID}/tasks", histHandler.GetSessionTasks)

	// Scheduled jobs
	r.Get("/api/scheduled/jobs", schedHandler.ListJobs)
	r.Get("/api/scheduled/jobs/{jobID}", schedHandler.GetJob)
	r.Post("/api/scheduled/jobs", schedHandler.CreateJob)
	r.Put("/api/scheduled/jobs/{jobID}", schedHandler.UpdateJob)
	r.Delete("/api/scheduled/jobs/{jobID}", schedHandler.DeleteJob)
	r.Post("/api/scheduled/jobs/{jobID}/toggle", schedHandler.ToggleJob)
	r.Get("/api/scheduled/jobs/{jobID}/runs", schedHandler.GetJobRuns)
	r.Get("/api/scheduled/runs/recent", schedHandler.GetRecentRuns)
	r.Post("/api/scheduled/validate-cron", schedHandler.ValidateCron)

	// Webhooks
	r.Get("/api/webhooks", whHandler.ListWebhooks)
	r.Post("/api/webhooks", whHandler.CreateWebhook)
	r.Patch("/api/webhooks/{webhookID}", whHandler.UpdateWebhook)
	r.Delete("/api/webhooks/{webhookID}", whHandler.DeleteWebhook)
	r.Post("/api/webhooks/{webhookID}/test", whHandler.TestWebhook)
	r.Get("/api/webhooks/{webhookID}/deliveries", whHandler.ListDeliveries)

	// Themes
	r.Get("/api/themes", themeHandler.ListThemes)
	r.Get("/api/themes/variables", themeHandler.GetThemeVariables)
	r.Get("/api/themes/{name}", themeHandler.GetTheme)
	r.Put("/api/themes/{name}", themeHandler.SaveTheme)
	r.Delete("/api/themes/{name}", themeHandler.DeleteTheme)
	r.Post("/api/themes/import", themeHandler.ImportTheme)
	r.Post("/api/themes/generate", themeHandler.GenerateTheme)

	// Uploads
	r.Post("/api/upload", routes.UploadFile)

	// Board remotes
	brHandler := routes.NewBoardRemotesHandler(s.db, s.cfg)
	r.Post("/api/board/remotes", brHandler.AddSubscription)
	r.Delete("/api/board/remotes", brHandler.RemoveSubscription)
	r.Get("/api/board/remotes", brHandler.ListSubscriptions)
	r.Get("/api/board/remotes/proxy/{remoteServer}/projects", brHandler.ProxyProjects)
	r.Get("/api/board/remotes/proxy/{remoteServer}/{project}/messages/all", brHandler.ProxyMessages)
	r.Get("/api/board/remotes/proxy/{remoteServer}/{project}/subscribers", brHandler.ProxySubscribers)
	r.Get("/api/board/remotes/proxy/{remoteServer}/{project}/messages/check", brHandler.ProxyCheckUnread)

	// Message board
	boardHandler := routes.NewBoardHandler(s.boardStore)
	r.Get("/api/board/projects", boardHandler.ListProjects)
	r.Post("/api/board/{project}/subscribe", boardHandler.Subscribe)
	r.Delete("/api/board/{project}/subscribe", boardHandler.Unsubscribe)
	r.Post("/api/board/{project}/messages", boardHandler.PostMessage)
	r.Get("/api/board/{project}/messages", boardHandler.ReadMessages)
	r.Get("/api/board/{project}/messages/all", boardHandler.ListAllMessages)
	r.Get("/api/board/{project}/messages/check", boardHandler.CheckUnread)
	r.Delete("/api/board/{project}/messages/{messageID}", boardHandler.DeleteMessage)
	r.Get("/api/board/{project}/subscribers", boardHandler.ListSubscribers)
	r.Post("/api/board/{project}/pause", boardHandler.PauseBoard)
	r.Post("/api/board/{project}/resume", boardHandler.ResumeBoard)
	r.Get("/api/board/{project}/paused", boardHandler.GetPaused)
	r.Delete("/api/board/{project}", boardHandler.DeleteBoard)
	r.Get("/api/board/{project}/groups", boardHandler.ListGroups)
	r.Get("/api/board/{project}/groups/{groupID}/members", boardHandler.ListGroupMembers)
	r.Post("/api/board/{project}/groups/{groupID}/members", boardHandler.AddGroupMember)
	r.Delete("/api/board/{project}/groups/{groupID}/members/{sessionID}", boardHandler.RemoveGroupMember)

	// One-shot tasks
	tasksHandler := routes.NewTasksHandler(s.db, s.cfg)
	s.tasksHandler = tasksHandler
	r.Post("/api/tasks/run", tasksHandler.SubmitTask)
	r.Get("/api/tasks/runs", tasksHandler.ListTasks)
	r.Get("/api/tasks/active", tasksHandler.ListActiveRuns)
	r.Get("/api/tasks/runs/{runID}", tasksHandler.GetTaskStatus)
	r.Post("/api/tasks/runs/{runID}/kill", tasksHandler.KillTask)

	// ── Static Files ────────────────────────────────────────────
	staticSub, err := fs.Sub(staticFS, "frontend/static")
	if err != nil {
		log.Fatalf("Failed to embed static files: %v", err)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// ── Dashboard SPA ───────────────────────────────────────────
	r.Get("/", s.serveIndex)
	r.Get("/diff", s.serveDiff)

	return r
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// License gate disabled for now
	// if !s.licenseMgr.IsValid() {
	// 	s.serveActivation(w, r)
	// 	return
	// }

	if s.indexTmpl == nil {
		w.Write([]byte(`<!DOCTYPE html><html><body>Template not loaded</body></html>`))
		return
	}
	data := templateData{CoralRoot: s.cfg.CoralRoot}
	if err := s.indexTmpl.Execute(w, data); err != nil {
		log.Printf("Error rendering index template: %v", err)
		http.Error(w, "Template render error", http.StatusInternalServerError)
	}
}

func (s *Server) serveActivation(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(activationPage))
}

const activationPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Coral — Activate License</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    background: #0f1117;
    color: #e1e4e8;
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 100vh;
  }
  .card {
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 12px;
    padding: 48px;
    max-width: 440px;
    width: 100%;
    text-align: center;
  }
  .card h1 {
    font-size: 24px;
    font-weight: 600;
    margin-bottom: 8px;
  }
  .card p {
    color: #8b949e;
    margin-bottom: 24px;
    font-size: 14px;
    line-height: 1.5;
  }
  .input-group { position: relative; margin-bottom: 16px; }
  .input-group input {
    width: 100%;
    padding: 12px 16px;
    background: #0d1117;
    border: 1px solid #30363d;
    border-radius: 8px;
    color: #e1e4e8;
    font-size: 15px;
    font-family: monospace;
    letter-spacing: 1px;
    outline: none;
    transition: border-color 0.2s;
  }
  .input-group input:focus { border-color: #58a6ff; }
  button {
    width: 100%;
    padding: 12px;
    background: #238636;
    color: #fff;
    border: none;
    border-radius: 8px;
    font-size: 15px;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.2s;
  }
  button:hover { background: #2ea043; }
  button:disabled { background: #21262d; color: #484f58; cursor: not-allowed; }
  .error {
    color: #f85149;
    font-size: 13px;
    margin-top: 12px;
    display: none;
  }
  .success {
    color: #3fb950;
    font-size: 13px;
    margin-top: 12px;
    display: none;
  }
</style>
</head>
<body>
<div class="card">
  <h1>Coral</h1>
  <p>Enter your license key to activate Coral.</p>
  <form id="activate-form">
    <div class="input-group">
      <input type="text" id="license-key" placeholder="XXXXX-XXXXX-XXXXX-XXXXX" autocomplete="off" spellcheck="false" required>
    </div>
    <button type="submit" id="submit-btn">Activate License</button>
  </form>
  <div class="error" id="error-msg"></div>
  <div class="success" id="success-msg"></div>
</div>
<script>
  const form = document.getElementById('activate-form');
  const input = document.getElementById('license-key');
  const btn = document.getElementById('submit-btn');
  const errorEl = document.getElementById('error-msg');
  const successEl = document.getElementById('success-msg');

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const key = input.value.trim();
    if (!key) return;

    btn.disabled = true;
    btn.textContent = 'Activating...';
    errorEl.style.display = 'none';
    successEl.style.display = 'none';

    try {
      const resp = await fetch('/api/license/activate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ license_key: key }),
      });
      const data = await resp.json();
      if (data.valid) {
        successEl.textContent = 'License activated! Redirecting...';
        successEl.style.display = 'block';
        setTimeout(() => window.location.reload(), 1000);
      } else {
        errorEl.textContent = data.error || 'Invalid license key.';
        errorEl.style.display = 'block';
        btn.disabled = false;
        btn.textContent = 'Activate License';
      }
    } catch (err) {
      errorEl.textContent = 'Network error. Please try again.';
      errorEl.style.display = 'block';
      btn.disabled = false;
      btn.textContent = 'Activate License';
    }
  });
</script>
</body>
</html>`

func (s *Server) serveDiff(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if s.diffTmpl == nil {
		w.Write([]byte(`<!DOCTYPE html><html><body>Template not loaded</body></html>`))
		return
	}
	if err := s.diffTmpl.Execute(w, nil); err != nil {
		log.Printf("Error rendering diff template: %v", err)
		http.Error(w, "Template render error", http.StatusInternalServerError)
	}
}

func isLocalhostOrigin(origin string) bool {
	// Match http(s)://localhost:PORT or http(s)://127.0.0.1:PORT
	if len(origin) < 16 {
		return false
	}
	for _, prefix := range []string{
		"http://localhost", "https://localhost",
		"http://127.0.0.1", "https://127.0.0.1",
	} {
		if len(origin) >= len(prefix) && origin[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
