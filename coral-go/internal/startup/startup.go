// Package startup provides shared server bootstrap logic used by all Coral
// entry points (coral, coral-tray, launch-coral). It handles database setup,
// terminal backend selection, HTTP server creation, and background service
// wiring so that each cmd/ binary doesn't have to duplicate this code.
package startup

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/cdknorow/coral/internal/background"
	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/ptymanager"
	"github.com/cdknorow/coral/internal/server"
	"github.com/cdknorow/coral/internal/store"
	"github.com/cdknorow/coral/internal/tmux"
)

// Options configures optional behaviors that differ between entry points.
type Options struct {
	// BackendType is "pty" or "tmux". Default is "tmux".
	BackendType string

	// OnServerError is called when ListenAndServe fails (non-ErrServerClosed).
	// If nil, log.Printf is used.
	OnServerError func(err error)
}

// RunningServer holds all resources created during startup.
// Callers use it to access the HTTP server for shutdown and the backend for cleanup.
type RunningServer struct {
	HTTPServer *http.Server
	Server     *server.Server
	DB         *store.DB
	Backend    ptymanager.TerminalBackend
}

// Shutdown gracefully shuts down the HTTP server and cleans up resources.
func (rs *RunningServer) Shutdown(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := rs.HTTPServer.Shutdown(ctx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}
	if rs.Backend != nil {
		rs.Backend.Close()
	}
}

// Close closes the database connection.
func (rs *RunningServer) Close() {
	if rs.DB != nil {
		rs.DB.Close()
	}
}

// Start opens the database, selects the terminal backend, creates the HTTP
// server, wires up all background services, and starts listening. The HTTP
// server runs in a background goroutine; callers should wait on ctx.Done()
// then call RunningServer.Shutdown().
func Start(ctx context.Context, cfg *config.Config, opts Options) (*RunningServer, error) {
	if opts.BackendType == "" {
		opts.BackendType = "tmux"
	}

	// Ensure ~/.coral directory exists before any file operations
	coralDir := cfg.CoralDir()
	if err := os.MkdirAll(coralDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory %s: %w", coralDir, err)
	}

	// Ensure DB parent directory exists (may differ from coralDir if overridden)
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Check if tmux is available when using tmux backend
	if opts.BackendType == "tmux" {
		if _, err := exec.LookPath("tmux"); err != nil {
			// Try common install locations
			found := false
			for _, p := range []string{"/opt/homebrew/bin/tmux", "/usr/local/bin/tmux", "/usr/bin/tmux"} {
				if _, err := os.Stat(p); err == nil {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("tmux is required but not found. Install it with: brew install tmux")
			}
		}
	}

	// Check if port is available
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("port %d is already in use: %w", cfg.Port, err)
	}
	ln.Close()

	// Open database
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Select terminal backend and agent runtime
	backend, agentRT, terminal := selectBackend(opts.BackendType, cfg.LogDir)

	// Build the HTTP server
	srv := server.New(cfg, db, backend, terminal)
	srv.RestoreSleepingBoards()

	addr = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // Disabled for WebSocket/SSE
		IdleTimeout:  60 * time.Second,
	}

	// Start all background services
	startBackgroundServices(ctx, db, cfg, srv, agentRT)

	// Start HTTP server in background goroutine
	onErr := opts.OnServerError
	if onErr == nil {
		onErr = func(err error) { log.Printf("[FATAL] Server error: %v", err) }
	}
	go func() {
		log.Printf("Coral dashboard: http://localhost:%d", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			onErr(err)
		}
	}()

	return &RunningServer{
		HTTPServer: httpServer,
		Server:     srv,
		DB:         db,
		Backend:    backend,
	}, nil
}

func selectBackend(backendType, logDir string) (ptymanager.TerminalBackend, background.AgentRuntime, ptymanager.SessionTerminal) {
	if backendType == "pty" {
		ptyBackend := ptymanager.NewPTYBackend()
		log.Println("Using native PTY terminal backend")
		return ptyBackend, background.NewPTYRuntime(ptyBackend), ptymanager.NewPTYSessionTerminal(ptyBackend)
	}
	tmuxClient := tmux.NewClient()
	tmuxBackend := ptymanager.NewTmuxBackend(tmuxClient, logDir)
	log.Println("Using tmux terminal backend")
	return tmuxBackend, background.NewTmuxRuntime(tmuxClient), ptymanager.NewTmuxSessionTerminal(tmuxClient)
}

// safeGo runs fn in a goroutine with panic recovery. If fn panics, the panic
// and stack trace are logged, and fn is restarted after a short delay. This
// prevents a single background service crash from taking down the entire
// server process. When the context is cancelled (normal shutdown), the
// goroutine exits without restarting.
func safeGo(ctx context.Context, name string, fn func()) {
	go func() {
		for {
			panicked := false
			func() {
				defer func() {
					if r := recover(); r != nil {
						panicked = true
						log.Printf("[CRASH] background service %q panicked: %v\n%s", name, r, debug.Stack())
					}
				}()
				fn()
			}()
			// Normal shutdown — don't restart.
			if ctx.Err() != nil {
				return
			}
			if !panicked {
				// fn returned without panic and context isn't done — unexpected.
				log.Printf("[WARN] background service %q returned unexpectedly", name)
			}
			log.Printf("[RESTART] background service %q restarting in 5s...", name)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()
}

func startBackgroundServices(ctx context.Context, db *store.DB, cfg *config.Config, srv *server.Server, agentRT background.AgentRuntime) {
	gitStore := store.NewGitStore(db)
	webhookStore := store.NewWebhookStore(db)
	taskStore := store.NewTaskStore(db)
	sessStore := store.NewSessionStore(db)
	schedStore := store.NewScheduleStore(db)
	rbStore := store.NewRemoteBoardStore(db)

	// Shared agent discovery function
	discoverFn := func(ctx context.Context) ([]background.AgentInfo, error) {
		return agentRT.ListAgents(ctx)
	}

	// Git poller
	gitPoller := background.NewGitPoller(gitStore, agentRT, time.Duration(cfg.GitPollerIntervalS)*time.Second)
	safeGo(ctx, "git_poller", func() { gitPoller.Run(ctx) })

	// Session indexer
	indexer := background.NewSessionIndexer(
		sessStore, nil,
		time.Duration(cfg.IndexerIntervalS)*time.Second,
		time.Duration(cfg.IndexerStartupDelayS)*time.Second,
	)
	safeGo(ctx, "session_indexer", func() { indexer.Run(ctx) })
	srv.SetIndexer(indexer)

	// Idle detector
	idleDetector := background.NewIdleDetector(taskStore, webhookStore, time.Duration(cfg.IdleDetectorIntervalS)*time.Second)
	idleDetector.SetSessionStore(sessStore)
	idleDetector.SetDiscoverFn(discoverFn)
	safeGo(ctx, "idle_detector", func() { idleDetector.Run(ctx) })

	// Webhook dispatcher
	webhookDispatcher := background.NewWebhookDispatcher(webhookStore, time.Duration(cfg.WebhookDispatcherIntervalS)*time.Second)
	safeGo(ctx, "webhook_dispatcher", func() { webhookDispatcher.Run(ctx) })

	// Job scheduler
	scheduler := background.NewJobScheduler(schedStore, 30*time.Second)
	launcher := background.NewAgentLauncher(agentRT, sessStore)
	scheduler.SetLaunchFn(launcher.BuildSchedulerLaunchFn(schedStore))
	scheduler.SetSessionStore(sessStore)
	scheduler.SetRuntime(agentRT)
	scheduler.SetNextFireTimeFn(background.NextFireTime)
	safeGo(ctx, "job_scheduler", func() { scheduler.Run(ctx) })
	srv.SetScheduler(scheduler)

	// Board notifier
	boardNotifier := background.NewBoardNotifier(srv.BoardStore(), agentRT, time.Duration(cfg.BoardNotifierIntervalS)*time.Second)
	boardNotifier.SetDiscoverFn(discoverFn)
	if bh := srv.BoardHandler(); bh != nil {
		boardNotifier.SetIsPausedFn(bh.IsPaused)
	}
	safeGo(ctx, "board_notifier", func() { boardNotifier.Run(ctx) })

	// Remote board poller
	remotePoller := background.NewRemoteBoardPoller(rbStore, agentRT, 30*time.Second)
	remotePoller.SetDiscoverFn(discoverFn)
	safeGo(ctx, "remote_board_poller", func() { remotePoller.Run(ctx) })

	// Batch summarizer
	summarizeFn := background.BuildSummarizeFn(sessStore)
	batchSummarizer := background.NewBatchSummarizer(sessStore, summarizeFn)
	safeGo(ctx, "batch_summarizer", func() { batchSummarizer.Run(ctx) })
	srv.SetSummarizeFn(summarizeFn)

	log.Printf("Started 8 background services (git poller, indexer, idle detector, webhook dispatcher, scheduler, board notifier, remote board poller, batch summarizer)")
}
