// Package debugserver serves the Go runtime profiles (net/http/pprof) on an
// address of their own, apart from the public API, for the soak and for
// production debugging. Bind it to localhost: the profiles expose the
// process's internals.
package debugserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"time"

	log "github.com/sirupsen/logrus"
)

// shutdownTimeout bounds the wait for profiles in flight at shutdown; a CPU
// profile can run for its whole ?seconds=, so it is cut short.
const shutdownTimeout = 2 * time.Second

// Handler returns the pprof routes under /debug/pprof/, on a mux of its own
// (never http.DefaultServeMux).
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

// Run serves Handler on addr until ctx is cancelled and returns nil on a
// clean stop. onListen, when not nil, receives the bound address.
func Run(ctx context.Context, addr string, onListen func(net.Addr)) error {
	ln, err := new(net.ListenConfig).Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("debug server listen: %w", err)
	}
	log.WithField("addr", ln.Addr().String()).Info("debug server (pprof) listening")
	if onListen != nil {
		onListen(ln.Addr())
	}
	srv := &http.Server{Handler: Handler(), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	select {
	case err := <-served:
		return fmt.Errorf("debug server: %w", err)
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		_ = srv.Close()
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("debug server: %w", err)
	}
	return nil
}
