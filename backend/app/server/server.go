// Package server builds the echo instance of the HTTP API and runs it with a
// graceful shutdown. It lives outside cmd/ so acceptance tests can start the
// real server in-process.
package server

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	appmw "github.com/bze-alphateam/bze-scan/backend/app/middleware"
)

// ShutdownTimeout bounds how long a shutdown waits for in-flight requests.
const ShutdownTimeout = 10 * time.Second

// APIPrefix is the prefix of every route but /health.
const APIPrefix = "/api/v1"

// Deps is what the routes read from, built by the composition root.
type Deps struct {
	// Explorer reads the explorer tables.
	Explorer controller.ExplorerReader
	// Accounts reads the account rows and labels; nil leaves
	// /api/v1/accounts out.
	Accounts controller.AccountReader
	// AccountState reads accounts live from the node; nil answers every
	// account with the live part unavailable.
	AccountState controller.AccountState
	// Status serves the status snapshots; nil leaves /api/v1/status out.
	Status controller.StatusReader
	// Raw serves the node's raw by-height JSON; nil leaves /api/v1/raw out.
	Raw controller.RawReader
	// CORSAllowedOrigins enables CORS for these origins ("*" for any); empty
	// sends no CORS headers.
	CORSAllowedOrigins []string
	// Log receives the request log; nil is the standard logger.
	Log log.FieldLogger
}

// New wires the echo instance: global middleware, the error handler and the
// routes. Any unknown path answers 404 with the JSON error envelope.
func New(deps Deps) *echo.Echo {
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	e := echo.New()
	e.HTTPErrorHandler = appmw.ErrorHandler

	e.Use(appmw.RequestID())
	e.Use(appmw.RequestLog(deps.Log))
	if len(deps.CORSAllowedOrigins) > 0 {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins:  deps.CORSAllowedOrigins,
			AllowMethods:  []string{http.MethodGet, http.MethodHead, http.MethodOptions},
			ExposeHeaders: []string{appmw.RequestIDHeader},
		}))
	}
	e.Use(appmw.Recover())

	health := controller.NewHealthController()
	e.GET("/health", health.Health)

	explorer := controller.NewExplorerController(deps.Explorer)
	api := e.Group(APIPrefix)
	api.GET("/blocks", explorer.Blocks)
	api.GET("/blocks/:height", explorer.Block)
	api.GET("/blocks/:height/events", explorer.BlockEvents)
	api.GET("/txs", explorer.Txs)
	api.GET("/txs/:hash", explorer.Tx)
	api.GET("/validators", explorer.Validators)
	api.GET("/validators/:operator", explorer.Validator)
	api.GET("/validators/:operator/blocks", explorer.ValidatorBlocks)
	api.GET("/search", explorer.Search)
	if deps.Accounts != nil {
		api.GET("/accounts/:address", controller.NewAccountController(deps.Accounts, deps.AccountState).Account)
	}
	if deps.Status != nil {
		api.GET("/status", controller.NewStatusController(deps.Status).Status)
	}
	if deps.Raw != nil {
		raw := controller.NewRawController(deps.Raw, deps.Explorer)
		api.GET("/raw/block/:height", raw.Block)
		api.GET("/raw/block_results/:height", raw.BlockResults)
		api.GET("/raw/commit/:height", raw.Commit)
		api.GET("/raw/tx/:hash", raw.Tx)
	}

	return e
}

// Run serves e on addr until ctx is cancelled, then stops accepting
// connections and drains in-flight requests for up to ShutdownTimeout. It
// returns nil after a clean shutdown and an error when the listener cannot be
// opened. onListen, when not nil, receives the bound address (useful with
// port 0 in tests).
func Run(ctx context.Context, e *echo.Echo, addr string, onListen func(net.Addr)) error {
	sc := echo.StartConfig{
		Address:         addr,
		HideBanner:      true,
		HidePort:        true,
		GracefulTimeout: ShutdownTimeout,
		ListenerAddrFunc: func(a net.Addr) {
			log.WithField("addr", a.String()).Info("HTTP server listening")
			if onListen != nil {
				onListen(a)
			}
		},
		OnShutdownError: func(err error) {
			log.WithError(err).Warn("HTTP server did not drain within the shutdown timeout")
		},
	}
	if err := sc.Start(ctx, e); err != nil {
		return err
	}
	log.Info("HTTP server stopped")
	return nil
}
