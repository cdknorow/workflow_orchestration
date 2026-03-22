// Command coral starts the Coral dashboard web server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/cdknorow/coral/internal/background"
	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/ptymanager"
	"github.com/cdknorow/coral/internal/server"
	"github.com/cdknorow/coral/internal/store"
	"github.com/cdknorow/coral/internal/tmux"
)

func main() {
	cfg := config.Load()

	// CLI flags override config
	host := flag.String("host", cfg.Host, "Host to bind to")
	port := flag.Int("port", cfg.Port, "Port to bind to")
	noBrowser := flag.Bool("no-browser", false, "Don't open the browser on startup")
	devMode := flag.Bool("dev", false, "Development mode: skip license check")
	defaultBackend := "tmux"
	if runtime.GOOS == "windows" {
		defaultBackend = "pty"
	}
	backendFlag := flag.String("backend", defaultBackend, "Terminal backend: pty or tmux")
	flag.Parse()

	cfg.DevMode = *devMode

	cfg.Host = *host
	cfg.Port = *port

	// Open database
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Select terminal backend and agent runtime
	var backend ptymanager.TerminalBackend
	var agentRT background.AgentRuntime
	var terminal ptymanager.SessionTerminal
	if *backendFlag == "pty" {
		ptyBackend := ptymanager.NewPTYBackend()
		backend = ptyBackend
		agentRT = background.NewPTYRuntime(ptyBackend)
		terminal = ptymanager.NewPTYSessionTerminal(ptyBackend)
		log.Println("Using native PTY terminal backend")
	} else {
		tmuxClient := tmux.NewClient()
		tmuxBackend := ptymanager.NewTmuxBackend(tmuxClient, cfg.LogDir)
		backend = tmuxBackend
		agentRT = background.NewTmuxRuntime(tmuxClient)
		terminal = ptymanager.NewTmuxSessionTerminal(tmuxClient)
		log.Println("Using tmux terminal backend")
	}

	// Build and start the HTTP server
	srv := server.New(cfg, db, backend, terminal)
	srv.RestoreSleepingBoards()
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // Disabled for WebSocket/SSE
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ── Start background services ───────────────────────────────
	gitStore := store.NewGitStore(db)
	webhookStore := store.NewWebhookStore(db)
	taskStore := store.NewTaskStore(db)

	gitPoller := background.NewGitPoller(gitStore, agentRT, time.Duration(cfg.GitPollerIntervalS)*time.Second)
	go gitPoller.Run(ctx)

	indexer := background.NewSessionIndexer(
		store.NewSessionStore(db), nil, // scanners added later per agent type
		time.Duration(cfg.IndexerIntervalS)*time.Second,
		time.Duration(cfg.IndexerStartupDelayS)*time.Second,
	)
	go indexer.Run(ctx)
	srv.SetIndexer(indexer)

	// Shared agent discovery function for background services
	discoverFn := func(ctx context.Context) ([]background.AgentInfo, error) {
		return agentRT.ListAgents(ctx)
	}

	idleDetector := background.NewIdleDetector(taskStore, webhookStore, time.Duration(cfg.IdleDetectorIntervalS)*time.Second)
	idleDetector.SetSessionStore(store.NewSessionStore(db))
	idleDetector.SetDiscoverFn(discoverFn)
	go idleDetector.Run(ctx)

	webhookDispatcher := background.NewWebhookDispatcher(webhookStore, time.Duration(cfg.WebhookDispatcherIntervalS)*time.Second)
	go webhookDispatcher.Run(ctx)

	schedStore := store.NewScheduleStore(db)
	scheduler := background.NewJobScheduler(schedStore, time.Duration(30)*time.Second)

	// Wire real agent launching into the scheduler
	sessStore := store.NewSessionStore(db)
	launcher := background.NewAgentLauncher(agentRT, sessStore)
	scheduler.SetLaunchFn(launcher.BuildSchedulerLaunchFn(schedStore))
	scheduler.SetSessionStore(sessStore)
	scheduler.SetRuntime(agentRT)
	scheduler.SetNextFireTimeFn(nextFireTime)

	go scheduler.Run(ctx)

	// Wire scheduler into the tasks API for launching/killing
	srv.SetScheduler(scheduler)

	// Board notifier — nudges agents about unread board messages
	boardNotifier := background.NewBoardNotifier(srv.BoardStore(), agentRT, time.Duration(cfg.BoardNotifierIntervalS)*time.Second)
	boardNotifier.SetDiscoverFn(discoverFn)
	if bh := srv.BoardHandler(); bh != nil {
		boardNotifier.SetIsPausedFn(bh.IsPaused)
	}
	go boardNotifier.Run(ctx)

	// Remote board poller — polls remote Coral servers for unread messages
	rbStore := store.NewRemoteBoardStore(db)
	remotePoller := background.NewRemoteBoardPoller(rbStore, agentRT, 30*time.Second)
	remotePoller.SetDiscoverFn(discoverFn)
	go remotePoller.Run(ctx)

	// Batch summarizer — auto-summarizes sessions using Claude CLI
	summarizeFn := background.BuildSummarizeFn(sessStore)
	batchSummarizer := background.NewBatchSummarizer(sessStore, summarizeFn)
	go batchSummarizer.Run(ctx)

	// Wire summarize function into history handler for sync resummarize endpoint
	srv.SetSummarizeFn(summarizeFn)

	log.Printf("Started 8 background services (git poller, indexer, idle detector, webhook dispatcher, scheduler, board notifier, remote board poller, batch summarizer)")

	// ── Start HTTP server ───────────────────────────────────────
	go func() {
		log.Printf("Coral dashboard: http://localhost:%d", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Open browser unless --no-browser
	if !*noBrowser {
		go func() {
			time.Sleep(500 * time.Millisecond)
			openBrowser(fmt.Sprintf("http://localhost:%d", cfg.Port))
		}()
	}

	<-ctx.Done()
	log.Println("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}

	// Clean up PTY sessions
	if backend != nil {
		backend.Close()
	}
}

// nextFireTime computes the next fire time for a cron expression after the given time.
// Uses 5-field cron format (minute hour dom month dow), matching the Python cron_parser.
func nextFireTime(cronExpr, tz string, after time.Time) (time.Time, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := parser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}

	// Convert to the job's timezone for evaluation
	loc := time.UTC
	if tz != "" {
		loc, err = time.LoadLocation(tz)
		if err != nil {
			loc = time.UTC
		}
	}
	afterLocal := after.In(loc)
	next := sched.Next(afterLocal)
	return next.UTC(), nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default: // linux, freebsd, etc.
		cmd = exec.Command("xdg-open", url)
	}
	// Best-effort; ignore errors
	cmd.Start()
}
