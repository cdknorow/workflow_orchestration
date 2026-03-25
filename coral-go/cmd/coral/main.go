// Command coral starts the Coral dashboard web server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/executil"
	"github.com/cdknorow/coral/internal/startup"
	"github.com/cdknorow/coral/internal/tracking"
)

func main() {
	// Panic recovery — write crash info to stderr and ~/.coral/crash.log
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("[CRASH] panic: %v\n", r)
			os.Stderr.WriteString(msg)
			home, _ := os.UserHomeDir()
			if home != "" {
				crashDir := fmt.Sprintf("%s/.coral", home)
				os.MkdirAll(crashDir, 0755)
				f, err := os.OpenFile(crashDir+"/crash.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
				if err == nil {
					f.WriteString(fmt.Sprintf("%s %s", time.Now().Format(time.RFC3339), msg))
					f.Close()
				}
			}
			os.Exit(1)
		}
	}()

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
