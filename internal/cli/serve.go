// Package cli. serve - the `benchbook serve` command: a small static file server
// for local previewing. It is a dev convenience only; the generated site needs no
// server at all and works from GitHub Pages or a file:// URL.
package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/seyallius/benchbook/internal/config"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Flag names of the serve command.
const (
	// flagPort selects the listening port.
	flagPort = "port"
	// flagHost selects the interface to bind.
	flagHost = "host"
)

// Server settings.
const (
	// defaultPort is used when --port is absent.
	defaultPort = 8000
	// defaultHost binds every interface, so the preview works inside containers
	// and on a remote machine without extra flags.
	defaultHost = "0.0.0.0"
	// shutdownGrace bounds how long in-flight requests may finish.
	shutdownGrace = 3 * time.Second
	// readHeaderTimeout protects against slow clients.
	readHeaderTimeout = 10 * time.Second
	// directoryIndexFile is served for directory URLs.
	directoryIndexFile = "index.html"
)

// serveOptions holds the parsed flags of `benchbook serve`.
type serveOptions struct {
	// port is the TCP port to listen on.
	port int
	// host is the interface to bind.
	host string
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// newServeCommand builds `benchbook serve`.
func (a *App) newServeCommand() *cobra.Command {
	options := serveOptions{}
	cmd := &cobra.Command{
		Use:     cmdServe,
		GroupID: groupPublish,
		Short:   "Serve the generated site locally",
		Long: `Serve the generated site over HTTP, for previewing it in a browser.

This is a development convenience: the site is static and works from GitHub Pages or
from a plain directory, so nothing about it depends on this server. It binds 0.0.0.0
by default, which also makes the preview reachable from inside a container.`,
		Example: `  benchbook serve
  benchbook serve --port 9000 --host 127.0.0.1`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runServe(cmd.Context(), options)
		},
	}
	cmd.Flags().IntVar(&options.port, flagPort, defaultPort, "TCP port to listen on")
	cmd.Flags().StringVar(&options.host, flagHost, defaultHost, "interface to bind")
	return cmd
}

// ----------------------------------------- Public API ----------------------------------------- //

// runServe serves the generated site until interrupted.
func (a *App) runServe(ctx context.Context, options serveOptions) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	root := cfg.SiteDir()
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return &usageError{err: fmt.Errorf("site directory %s does not exist: run `benchbook build` first", relativeTo(cfg, root))}
	}
	return a.serveSite(ctx, cfg, root, options.host, options.port)
}

// serveSite runs the HTTP server. It returns when the context is cancelled (which is
// what makes it testable) or when serving fails.
func (a *App) serveSite(ctx context.Context, cfg *config.Config, root, host string, port int) error {
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return &usageError{err: fmt.Errorf("listen on %s: %w (is another server already using that port?)", net.JoinHostPort(host, strconv.Itoa(port)), err)}
	}
	server := &http.Server{
		Handler:           staticHandler(root),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	a.printf("🌐 serving %s at http://localhost:%d\n", relativeTo(cfg, root), port)
	a.info("press Ctrl+C to stop")

	done := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			done <- err
			return
		}
		done <- nil
	}()

	interrupt, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("serve %s: %w", root, err)
		}
		return nil
	case <-interrupt.Done():
		a.info("stopping")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down server: %w", err)
		}
		return nil
	}
}

// staticHandler serves files from the site directory and reports a missing page with
// a hint instead of a bare 404, because a 404 on the manifest is nearly always a
// forgotten `benchbook build`.
func staticHandler(root string) http.Handler {
	fileServer := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		cleaned := filepath.Join(root, filepath.FromSlash(filepath.Clean("/"+request.URL.Path)))
		if info, err := os.Stat(cleaned); err == nil && info.IsDir() {
			index := filepath.Join(cleaned, directoryIndexFile)
			if _, err := os.Stat(index); err != nil {
				http.NotFound(writer, request)
				return
			}
		}
		if _, err := os.Stat(cleaned); err != nil && request.URL.Path != "/" {
			writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(writer, "not found: %s\nrun `benchbook build` to regenerate the site\n", request.URL.Path)
			return
		}
		fileServer.ServeHTTP(writer, request)
	})
}
