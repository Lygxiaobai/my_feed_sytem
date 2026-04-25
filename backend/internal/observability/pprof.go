package observability

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"

	"my_feed_system/internal/config"
)

const pprofShutdownTimeout = 5 * time.Second

// NewPprofHandler 返回独立的 pprof HTTP Handler，避免污染业务路由。
func NewPprofHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	for _, profile := range []string{
		"allocs",
		"block",
		"goroutine",
		"heap",
		"mutex",
		"threadcreate",
	} {
		mux.Handle("/debug/pprof/"+profile, pprof.Handler(profile))
	}

	return mux
}

// StartPprof 在独立地址启动 pprof；当 ctx 结束时自动关闭。
func StartPprof(ctx context.Context, name string, cfg config.PprofServerConfig) error {
	if !cfg.Enabled {
		log.Printf("%s pprof disabled", name)
		return nil
	}

	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		return fmt.Errorf("%s pprof enabled but addr is empty", name)
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s pprof on %s: %w", name, addr, err)
	}

	server := &http.Server{
		Addr:    addr,
		Handler: NewPprofHandler(),
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), pprofShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("%s pprof shutdown failed: %v", name, err)
		}
	}()

	go func() {
		log.Printf("%s pprof listening on %s", name, addr)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("%s pprof server stopped unexpectedly: %v", name, err)
		}
	}()

	return nil
}
