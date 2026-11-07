package main

import (
	"context"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// shutdownDrain is how long in-flight requests get to finish on SIGINT/SIGTERM.
const shutdownDrain = 10 * time.Second

func runServe(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "path to the YAML config")
	adapterName := fs.String("adapter", "codex", "adapter name")
	if code, ok := parseFlags(fs, args, stderr, cfgPath); !ok {
		return code
	}
	logf := log.New(stderr, "", log.LstdFlags).Printf
	app, err := NewApp(AppOptions{ConfigPath: *cfgPath, Adapter: *adapterName, Version: version, Logf: logf})
	if err != nil {
		return fail(stderr, err)
	}
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)
	if err := app.Start(); err != nil {
		app.Close()
		return fail(stderr, err)
	}
	return serveLoop(app, sigs, logf, shutdownDrain)
}

// serveLoop runs until a stop signal or a listener failure. SIGHUP reloads
// (App.Reload logs the outcome). A second stop signal during the drain
// closes everything at once.
func serveLoop(app *App, sigs <-chan os.Signal, logf func(string, ...any), drain time.Duration) int {
	for {
		select {
		case err := <-app.Errors():
			logf("cui-proxy: listener failed: %v", err)
			app.Close()
			return exitFailure
		case s := <-sigs:
			if s == syscall.SIGHUP {
				_ = app.Reload()
				continue
			}
			logf("shutting down (%v); draining for up to %v", s, drain)
			ctx, cancel := context.WithTimeout(context.Background(), drain)
			done := make(chan error, 1)
			go func() { done <- app.Shutdown(ctx) }()
			var err error
		wait:
			for {
				select {
				case err = <-done:
					break wait
				case s2 := <-sigs:
					if s2 != syscall.SIGHUP {
						logf("second signal; closing now")
						cancel()
					}
				}
			}
			cancel()
			if err != nil {
				logf("cui-proxy: %v", err)
				return exitFailure
			}
			logf("stopped")
			return exitOK
		}
	}
}
