// Command launch-coral discovers worktrees, launches agent tmux sessions,
// and starts the Coral web server. It is the Go equivalent of launch_agents.sh.
//
// Usage:
//
//	launch-coral [flags] [target-dir] [agent-type] [agents]
//
// Examples:
//
//	launch-coral .                     # Start web server only
//	launch-coral . claude agents       # Launch Claude agents + web server
//	launch-coral /path/to/root gemini agents
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
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/cdknorow/coral/internal/agent"
	"github.com/cdknorow/coral/internal/background"
	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/server"
	"github.com/cdknorow/coral/internal/store"
	"github.com/cdknorow/coral/internal/tmux"

	"github.com/google/uuid"
)

const maxAgents = 5

func main() {
	portFlag := flag.Int("port", 0, "Override web server port (default: $CORAL_PORT or 8420)")
	skipWeb := flag.Bool("skip-web-server", false, "Skip launching the web server")
	noBrowser := flag.Bool("no-browser", false, "Don't open the browser on startup")
	flag.Parse()

	args := flag.Args()

	targetDir := "."
	agentType := "claude"
	launchAgents := false

	if len(args) >= 1 {
		targetDir = args[0]
	}
	if len(args) >= 2 {
		agentType = args[1]
	}
	if len(args) >= 3 && args[2] == "agents" {
		launchAgents = true
	}

	// Resolve target directory to absolute path
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		log.Fatalf("Error resolving target directory: %v", err)
	}
	targetDir = absTarget

	logDir := os.TempDir()
	logDir = strings.TrimRight(logDir, "/")

	// Clean up old coral logs
	cleanOldLogs(logDir)

	cfg := config.Load()
	if *portFlag > 0 {
		cfg.Port = *portFlag
	}
	cfg.CoralRoot = targetDir

	tc := tmux.NewClient()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var sessionNames []string

	if launchAgents {
		sessionNames, err = launchAgentSessions(ctx, tc, targetDir, agentType, logDir)
		if err != nil {
			log.Fatalf("Error launching agents: %v", err)
		}
	} else {
		fmt.Println("=== Coral Dashboard ===")
		fmt.Println("Starting web server only (launch agents from the dashboard).")
		fmt.Println("To also launch agents: launch-coral <path> <agent-type> agents")
		fmt.Println()
	}

	if !*skipWeb {
		startWebServer(ctx, cfg)
	}

	// Open the browser
	if !*noBrowser && !isSSH() {
		go func() {
			time.Sleep(time.Second)
			openBrowser(fmt.Sprintf("http://localhost:%d", cfg.Port))
		}()
	}

	fmt.Println("=== All sessions launched ===")
	fmt.Println()
	if len(sessionNames) > 0 {
		fmt.Println("Quick attach commands:")
		for _, sn := range sessionNames {
			fmt.Printf("  tmux attach -t %s\n", sn)
		}
		fmt.Println()
		fmt.Println("Kill all agents:")
		fmt.Printf("  for sn in %s; do tmux kill-session -t $sn 2>/dev/null; done\n",
			strings.Join(sessionNames, " "))
	}

	<-ctx.Done()
	log.Println("Shutting down...")
}

func cleanOldLogs(logDir string) {
	pattern := filepath.Join(logDir, "*_coral_*.log")
	matches, _ := filepath.Glob(pattern)
	for _, m := range matches {
		os.Remove(m)
	}
}

func launchAgentSessions(ctx context.Context, tc *tmux.Client, targetDir, agentType, logDir string) ([]string, error) {
	// Collect valid subdirectories (up to maxAgents)
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read target directory %s: %w", targetDir, err)
	}

	var worktrees []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		worktrees = append(worktrees, filepath.Join(targetDir, entry.Name()))
		if len(worktrees) >= maxAgents {
			break
		}
	}

	if len(worktrees) == 0 {
		return nil, fmt.Errorf("no subdirectories found in %s", targetDir)
	}

	fmt.Printf("=== %s Coral Launcher (Independent Sessions) ===\n", agentType)
	fmt.Printf("Target directory: %s\n", targetDir)
	fmt.Printf("Found %d workspace(s):\n\n", len(worktrees))

	ag := agent.GetAgent(agentType)

	// Locate PROTOCOL.md relative to the executable
	protocolPath := findProtocolMD()

	var sessionNames []string
	for _, dir := range worktrees {
		folderName := filepath.Base(dir)
		sessionID := uuid.New().String()
		sessionName := fmt.Sprintf("%s-%s", agentType, sessionID)
		logFile := filepath.Join(logDir, fmt.Sprintf("%s_coral_%s.log", agentType, sessionID))

		// Clear old log
		os.WriteFile(logFile, nil, 0644)

		// Create a new detached tmux session rooted in the worktree
		if err := tc.NewSession(ctx, sessionName, dir); err != nil {
			log.Printf("Warning: failed to create tmux session %s: %v", sessionName, err)
			continue
		}

		// Stream stdout to log file
		if err := tc.PipePane(ctx, sessionName, logFile); err != nil {
			log.Printf("Warning: failed to set up pipe-pane for %s: %v", sessionName, err)
		}

		// Set pane title
		tc.SendKeysToTarget(ctx, sessionName+".0",
			fmt.Sprintf("printf '\\033]2;%s — %s\\033\\\\'", folderName, agentType))
		time.Sleep(200 * time.Millisecond)

		// Build and send the launch command
		launchCmd := ag.BuildLaunchCommand(sessionID, protocolPath, "", nil, dir)
		tc.SendKeysToTarget(ctx, sessionName+".0", launchCmd)

		// Open a terminal window attached to this session
		if !isSSH() {
			openAgentTerminal(sessionName, fmt.Sprintf("%s — %s", folderName, agentType))
		}

		fmt.Printf("  [+] Session : %s\n", sessionName)
		fmt.Printf("      Dir     : %s\n", dir)
		fmt.Printf("      Log     : %s\n", logFile)
		fmt.Printf("      Attach  : tmux attach -t %s\n\n", sessionName)

		sessionNames = append(sessionNames, sessionName)
	}

	return sessionNames, nil
}

func startWebServer(ctx context.Context, cfg *config.Config) {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	srv := server.New(cfg, db, nil) // nil backend = tmux mode
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}

	// Start background services
	tmuxClient := tmux.NewClient()
	gitStore := store.NewGitStore(db)
	webhookStore := store.NewWebhookStore(db)
	taskStore := store.NewTaskStore(db)

	gitPoller := background.NewGitPoller(gitStore, tmuxClient, time.Duration(cfg.GitPollerIntervalS)*time.Second)
	go gitPoller.Run(ctx)

	indexer := background.NewSessionIndexer(
		store.NewSessionStore(db), nil,
		time.Duration(cfg.IndexerIntervalS)*time.Second,
		time.Duration(cfg.IndexerStartupDelayS)*time.Second,
	)
	go indexer.Run(ctx)

	idleDetector := background.NewIdleDetector(taskStore, webhookStore, time.Duration(cfg.IdleDetectorIntervalS)*time.Second)
	go idleDetector.Run(ctx)

	webhookDispatcher := background.NewWebhookDispatcher(webhookStore, time.Duration(cfg.WebhookDispatcherIntervalS)*time.Second)
	go webhookDispatcher.Run(ctx)

	schedStore := store.NewScheduleStore(db)
	scheduler := background.NewJobScheduler(schedStore, 30*time.Second)

	// Wire real agent launching into the scheduler
	sessStore := store.NewSessionStore(db)
	launcher := background.NewAgentLauncher(tmuxClient, sessStore)
	scheduler.SetLaunchFn(launcher.BuildSchedulerLaunchFn(schedStore))

	go scheduler.Run(ctx)

	go func() {
		log.Printf("Coral dashboard: http://localhost:%d", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdownCtx)
		db.Close()
	}()
}

// findProtocolMD locates PROTOCOL.md relative to the running binary or source.
func findProtocolMD() string {
	// Check next to the executable
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "PROTOCOL.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Check common source locations
	for _, rel := range []string{
		"src/coral/PROTOCOL.md",
		"PROTOCOL.md",
	} {
		if _, err := os.Stat(rel); err == nil {
			abs, _ := filepath.Abs(rel)
			return abs
		}
	}

	return ""
}

func isSSH() bool {
	return os.Getenv("SSH_CONNECTION") != ""
}

func openAgentTerminal(session, title string) {
	if runtime.GOOS != "darwin" {
		// Try Linux terminal emulators
		for _, term := range []string{"x-terminal-emulator", "gnome-terminal", "konsole", "xfce4-terminal"} {
			if path, err := exec.LookPath(term); err == nil {
				cmd := exec.Command(path, "-e", fmt.Sprintf("tmux attach -t %s", session))
				cmd.Start()
				return
			}
		}
		fmt.Printf("  [~] No supported terminal emulator found (use: tmux attach -t %s)\n", session)
		return
	}

	// macOS: try iTerm2 first, then Terminal.app
	script := fmt.Sprintf(`tell application "iTerm2"
    create window with default profile command "tmux attach -t %s"
end tell`, session)
	cmd := exec.Command("osascript", "-e", script)
	if err := cmd.Run(); err == nil {
		return
	}

	script = fmt.Sprintf(`tell application "Terminal"
    do script "tmux attach -t %s"
    set custom title of front window to "%s"
end tell`, session, title)
	exec.Command("osascript", "-e", script).Run()
}

func openBrowser(url string) {
	var cmd string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "linux":
		cmd = "xdg-open"
	default:
		return
	}
	exec.Command(cmd, url).Start()
}
