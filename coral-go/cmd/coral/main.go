// Command coral starts the Coral dashboard web server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/executil"
	"github.com/cdknorow/coral/internal/startup"
	"github.com/cdknorow/coral/internal/tracking"
)

// setupCrashLogging redirects log output to ~/.coral/coral.log so that panics
// and errors are captured even when the server runs in the background.
func setupCrashLogging() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	logDir := filepath.Join(home, ".coral")
	os.MkdirAll(logDir, 0755)
	logFile := filepath.Join(logDir, "coral.log")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	log.SetOutput(f)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
}

func main() {
	setupCrashLogging()

	// Global panic recovery — log the full stack trace before exiting
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[FATAL] panic in main: %v\n%s", r, debug.Stack())
			os.Exit(1)
		}
	}()

	log.Printf("[STARTUP] coral server starting pid=%d go=%s os=%s arch=%s",
		os.Getpid(), runtime.Version(), runtime.GOOS, runtime.GOARCH)

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

	if *devMode {
		cfg.DevMode = true
	}

	cfg.Host = *host
	cfg.Port = *port

	// Graceful shutdown on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rs, err := startup.Start(ctx, cfg, startup.Options{
		BackendType: *backendFlag,
	})
	if err != nil {
		log.Fatalf("Failed to start: %v", err)
	}
	defer rs.Close()

	// Anonymous install/upgrade tracking (non-blocking)
	tracking.TrackInstallAsync()

	// Open browser unless --no-browser
	if !*noBrowser {
		go func() {
			time.Sleep(500 * time.Millisecond)
			executil.OpenBrowser(fmt.Sprintf("http://localhost:%d", cfg.Port))
		}()
	}

	<-ctx.Done()
	log.Println("Shutting down...")
	rs.Shutdown(10 * time.Second)
}
