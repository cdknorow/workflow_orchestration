// Command coral-tray runs the Coral dashboard as a macOS/Windows/Linux system tray app.
//
// Usage:
//
//	coral-tray                     # Spawn a background tray process and exit
//	coral-tray --foreground        # Run in foreground (used internally by the background spawn)
//	coral-tray --stop              # Stop a running tray instance
//	coral-tray --port 9000         # Use a custom port
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fyne.io/systray"
	"github.com/gen2brain/beeep"
	"github.com/robfig/cron/v3"

	"github.com/cdknorow/coral/internal/background"
	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/ptymanager"
	"github.com/cdknorow/coral/internal/server"
	"github.com/cdknorow/coral/internal/store"
	"github.com/cdknorow/coral/internal/tmux"
)

//go:embed icon.png
var iconData []byte

const (
	githubReleasesAPI = "https://api.github.com/repos/cdknorow/coral/releases/latest"
	githubReleasesURL = "https://github.com/cdknorow/coral/releases"
)

var version = "dev"

func main() {
	// macOS requires Cocoa calls on the main thread
	runtime.LockOSThread()

	host := flag.String("host", "0.0.0.0", "Host to bind to")
	port := flag.Int("port", 8420, "Port to bind to")
	homeDir := flag.String("home", "", "Home directory for Coral")
	foreground := flag.Bool("foreground", false, "Run in foreground (used internally)")
	stop := flag.Bool("stop", false, "Stop a running tray instance")
	noBrowser := flag.Bool("no-browser", false, "Don't open the browser on startup")
	devMode := flag.Bool("dev", false, "Development mode: skip license check")
	debugMode := flag.Bool("debug", false, "Enable debug logging to ~/.coral/tray.log")
	backendFlag := flag.String("backend", "tmux", "Terminal backend: pty or tmux")
	flag.Parse()

	// When launched from a .app bundle on macOS, run in foreground automatically
	// (macOS expects the main process to keep running)
	if !*foreground && runtime.GOOS == "darwin" && isInsideAppBundle() {
		*foreground = true
	}

	dataDir := getDataDir()

	// Handle --stop
	if *stop {
		pid := readPID(dataDir)
		if pid > 0 {
			if err := signalProcess(pid, syscall.SIGTERM); err != nil {
				fmt.Printf("Failed to stop Coral tray (PID %d): %v\n", pid, err)
			} else {
				fmt.Printf("Stopped Coral tray (PID %d)\n", pid)
			}
		} else {
			fmt.Println("No running Coral tray found.")
		}
		return
	}

	// If --foreground, run directly (this is the detached child)
	if *foreground {
		if *homeDir != "" {
			os.MkdirAll(*homeDir, 0755)
			os.Chdir(*homeDir)
		}
		runForeground(*host, *port, *noBrowser, *devMode, *debugMode, *backendFlag, dataDir)
		return
	}

	// Check if already running
	pid := readPID(dataDir)
	if pid > 0 {
		fmt.Printf("Coral tray is already running (PID %d). Use --stop to stop it.\n", pid)
		return
	}

	// Resolve home directory
	home := *homeDir
	if home == "" {
		home, _ = os.UserHomeDir()
	}

	// Spawn ourselves as a detached background process
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("Cannot find executable: %v", err)
	}
	args := []string{"--foreground", "--host", *host, "--port", strconv.Itoa(*port), "--home", home, "--backend", *backendFlag}
	if *devMode {
		args = append(args, "--dev")
	}
	if *debugMode {
		args = append(args, "--debug")
	}
	cmd := exec.Command(exe, args...)

	logFile := filepath.Join(dataDir, "tray.log")
	lf, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Cannot open log file: %v", err)
	}
	cmd.Stdout = lf
	cmd.Stderr = lf
	cmd.Stdin = nil
	cmd.SysProcAttr = detachProcessAttrs()

	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start background process: %v", err)
	}

	fmt.Printf("Coral tray started in background (dashboard on port %d)\n", *port)
	fmt.Printf("  Home: %s\n", home)
	fmt.Printf("  Logs: %s\n", logFile)
	fmt.Printf("  Stop: coral-tray --stop\n")
}

func runForeground(host string, port int, noBrowser, devMode, debugMode bool, backendType, dataDir string) {
	// Setup log file FIRST — when launched from .app there's no terminal,
	// so log.Fatalf messages would disappear silently without this.
	logFile := filepath.Join(dataDir, "tray.log")
	if lf, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		log.SetOutput(lf)
	}
	log.Println("coral-tray starting in foreground mode")

	if debugMode {
		os.Setenv("CORAL_DEBUG", "1")
		log.Println("Debug mode enabled")
	}

	// Write PID
	writePID(dataDir)
	defer removePID(dataDir)

	// Setup signal handling
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start the HTTP server
	cfg := config.Load()
	cfg.Host = host
	cfg.Port = port
	if devMode {
		cfg.DevMode = true
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Printf("Failed to open database: %v", err)
		beeep.Notify("Coral", "Failed to open database: "+err.Error(), "")
		// Still launch tray so user sees the error
		systray.Run(func() {
			systray.SetTemplateIcon(iconData, iconData)
			systray.SetTooltip("Coral — Error")
			mQuit := systray.AddMenuItem("Quit", "Exit Coral")
			go func() { <-mQuit.ClickedCh; systray.Quit() }()
		}, func() {})
		return
	}
	defer db.Close()

	// Select terminal backend
	var backend ptymanager.TerminalBackend
	var agentRT background.AgentRuntime
	var terminal ptymanager.SessionTerminal
	if backendType == "pty" {
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

	srv := server.New(cfg, db, backend, terminal)
	srv.RestoreSleepingBoards()

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}
	gitStore := store.NewGitStore(db)
	webhookStore := store.NewWebhookStore(db)
	taskStore := store.NewTaskStore(db)

	gitPoller := background.NewGitPoller(gitStore, agentRT, time.Duration(cfg.GitPollerIntervalS)*time.Second)
	go gitPoller.Run(ctx)

	indexer := background.NewSessionIndexer(
		store.NewSessionStore(db), nil,
		time.Duration(cfg.IndexerIntervalS)*time.Second,
		time.Duration(cfg.IndexerStartupDelayS)*time.Second,
	)
	go indexer.Run(ctx)

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
	scheduler := background.NewJobScheduler(schedStore, 30*time.Second)
	sessStore := store.NewSessionStore(db)
	launcher := background.NewAgentLauncher(agentRT, sessStore)
	scheduler.SetLaunchFn(launcher.BuildSchedulerLaunchFn(schedStore))
	scheduler.SetSessionStore(sessStore)
	scheduler.SetNextFireTimeFn(nextFireTime)
	go scheduler.Run(ctx)

	srv.SetScheduler(scheduler)

	boardNotifier := background.NewBoardNotifier(srv.BoardStore(), agentRT, time.Duration(cfg.BoardNotifierIntervalS)*time.Second)
	boardNotifier.SetDiscoverFn(discoverFn)
	if bh := srv.BoardHandler(); bh != nil {
		boardNotifier.SetIsPausedFn(bh.IsPaused)
	}
	go boardNotifier.Run(ctx)

	rbStore := store.NewRemoteBoardStore(db)
	remotePoller := background.NewRemoteBoardPoller(rbStore, agentRT, 30*time.Second)
	remotePoller.SetDiscoverFn(discoverFn)
	go remotePoller.Run(ctx)

	summarizeFn := background.BuildSummarizeFn(sessStore)
	batchSummarizer := background.NewBatchSummarizer(sessStore, summarizeFn)
	go batchSummarizer.Run(ctx)
	srv.SetSummarizeFn(summarizeFn)

	// Start HTTP server
	go func() {
		log.Printf("Coral dashboard: http://localhost:%d", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
			beeep.Notify("Coral", "Server failed to start: "+err.Error(), "")
		}
	}()

	// Open dashboard (try native app first, fall back to browser)
	if !noBrowser {
		go func() {
			time.Sleep(time.Second)
			dashURL := fmt.Sprintf("http://localhost:%d", port)
			if err := launchCoralApp(dashURL); err != nil {
				openBrowser(dashURL)
			}
		}()
	}

	// Check for updates in background
	go checkForUpdatesOnStartup(port)

	url := fmt.Sprintf("http://localhost:%d", port)

	// Run systray (blocks until quit)
	systray.Run(func() {
		systray.SetTemplateIcon(iconData, iconData)
		systray.SetTitle("")
		systray.SetTooltip("Coral Dashboard")

		mOpenApp := systray.AddMenuItem("Open in App", "Open Coral in native window")
		mOpenBrowser := systray.AddMenuItem("Open in Browser", "Open Coral in web browser")
		mUpdate := systray.AddMenuItem("Check for Updates", "Check for new versions")
		systray.AddSeparator()
		mShutdown := systray.AddMenuItem("Shutdown — Kill Agents & Stop Server", "Kill all agents and stop the server")
		mQuit := systray.AddMenuItem("Quit — Exit Coral", "Exit the tray app")

		go func() {
			for {
				select {
				case <-mOpenApp.ClickedCh:
					go launchCoralApp(url)
				case <-mOpenBrowser.ClickedCh:
					openBrowser(url)
				case <-mUpdate.ClickedCh:
					go checkForUpdates()
				case <-mShutdown.ClickedCh:
					go func() {
						killed := killAllAgents(url)
						shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						httpServer.Shutdown(shutdownCtx)
						beeep.Notify("Coral", fmt.Sprintf("Shut down %d agent(s) and dashboard server.", killed), "")
					}()
				case <-mQuit.ClickedCh:
					func() {
						shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						httpServer.Shutdown(shutdownCtx)
					}()
					systray.Quit()
				case <-ctx.Done():
					func() {
						shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						httpServer.Shutdown(shutdownCtx)
					}()
					systray.Quit()
					return
				}
			}
		}()
	}, func() {
		// onExit — kill coral-app subprocess and stop server
		if coralAppProcess != nil {
			coralAppProcess.Signal(syscall.SIGTERM)
			coralAppProcess = nil
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdownCtx)
		log.Println("coral-tray exited")
	})
}

// killAllAgents calls the REST API to kill all running agent sessions.
func killAllAgents(baseURL string) int {
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/sessions/live")
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var sessions []map[string]any
	if err := json.Unmarshal(body, &sessions); err != nil {
		return 0
	}

	killed := 0
	for _, s := range sessions {
		name, _ := s["name"].(string)
		if name == "" {
			continue
		}
		payload := map[string]any{
			"session_id": s["session_id"],
			"agent_type": s["agent_type"],
		}
		data, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST",
			fmt.Sprintf("%s/api/sessions/live/%s/kill", baseURL, name),
			strings.NewReader(string(data)))
		req.Header.Set("Content-Type", "application/json")
		if r, err := client.Do(req); err == nil {
			r.Body.Close()
			killed++
		}
	}
	return killed
}

// checkForUpdatesOnStartup checks for updates silently on startup.
func checkForUpdatesOnStartup(port int) {
	time.Sleep(5 * time.Second) // Wait for server to be ready
	latest := fetchLatestVersion()
	if latest != "" && latest != version && version != "dev" {
		beeep.Notify("Coral", fmt.Sprintf("Update available: v%s (you have v%s)", latest, version), "")
	}
}

// checkForUpdates checks for updates and notifies.
func checkForUpdates() {
	latest := fetchLatestVersion()
	if latest == "" {
		beeep.Notify("Coral", "Could not check for updates.", "")
		return
	}
	if latest != version && version != "dev" {
		beeep.Notify("Coral", fmt.Sprintf("Update available: v%s\nYou have v%s", latest, version), "")
		openBrowser(githubReleasesURL)
	} else {
		beeep.Notify("Coral", "You're on the latest version.", "")
	}
}

// fetchLatestVersion queries GitHub for the latest release version.
func fetchLatestVersion() string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(githubReleasesAPI)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var data struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return ""
	}
	return strings.TrimPrefix(data.TagName, "v")
}

// PID file management

func getDataDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".coral")
	os.MkdirAll(dir, 0755)
	return dir
}

func writePID(dataDir string) {
	pidFile := filepath.Join(dataDir, "tray.pid")
	os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func removePID(dataDir string) {
	os.Remove(filepath.Join(dataDir, "tray.pid"))
}

func readPID(dataDir string) int {
	data, err := os.ReadFile(filepath.Join(dataDir, "tray.pid"))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	// Check if process is alive
	if err := signalProcess(pid, 0); err != nil {
		removePID(dataDir)
		return 0
	}
	return pid
}

// coralAppProcess tracks the running coral-app subprocess.
var coralAppProcess *os.Process

// launchCoralApp launches coral-app or brings the existing one to front.
// Returns an error if coral-app is not found or fails to start.
func launchCoralApp(url string) error {
	// If already running, don't spawn another
	if coralAppProcess != nil {
		// Check if still alive (signal 0 = no-op, just checks existence)
		if err := coralAppProcess.Signal(syscall.Signal(0)); err == nil {
			return nil // already running
		}
		// Process exited — clear the reference
		coralAppProcess = nil
	}

	// Find the coral-app binary
	appPath := findCoralApp()
	if appPath == "" {
		return fmt.Errorf("coral-app not found")
	}

	cmd := exec.Command(appPath, "--url", url)
	if err := cmd.Start(); err != nil {
		return err
	}
	coralAppProcess = cmd.Process

	// Reap the process in background so we don't leak zombies
	go cmd.Wait()

	return nil
}

// findCoralApp looks for the coral-app binary next to the executable, then in PATH.
func findCoralApp() string {
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "coral-app")
		if runtime.GOOS == "windows" {
			candidate += ".exe"
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if p, err := exec.LookPath("coral-app"); err == nil {
		return p
	}
	return ""
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

func nextFireTime(cronExpr, tz string, after time.Time) (time.Time, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := parser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}
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

// isInsideAppBundle detects if the binary is running inside a macOS .app bundle
// by checking if the executable path contains ".app/Contents/MacOS/".
func isInsideAppBundle() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.Contains(exe, ".app/Contents/MacOS/")
}
