// Package server builds the echo instance of the HTTP API and runs it with a
// graceful shutdown. It lives outside cmd/ so acceptance tests can start the
// real server in-process.
package server

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	appmw "github.com/bze-alphateam/bze-scan/backend/app/middleware"
)

// ShutdownTimeout bounds how long a shutdown waits for in-flight requests.
const ShutdownTimeout = 10 * time.Second

// APIPrefix is the prefix of the explorer's own routes.
const APIPrefix = "/api/v1"

// CompatPrefix is the prefix of the aggregator-compatible routes: the BZE
// aggregator API's paths and answers (/api/prices, …), mounted next to
// /api/v1, not inside it. None of the /api/v1 conventions apply to them,
// and they answer CORS requests from any origin, whatever
// CORSAllowedOrigins says, because browsers call them the way they call the
// aggregator.
const CompatPrefix = "/api"

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
	// OpenOrders answers the accounts' open orders; nil leaves
	// /api/v1/accounts/{address}/open-orders out.
	OpenOrders controller.OpenOrdersReader
	// Tokens reads the denoms and their history; nil leaves /api/v1/tokens
	// and /api/v1/token out.
	Tokens controller.TokenReader
	// Proposals reads the proposals, votes and deposits; nil leaves
	// /api/v1/proposals out.
	Proposals controller.ProposalReader
	// Upgrades reads the software upgrades' recovery; nil leaves
	// /api/v1/upgrades out.
	Upgrades controller.UpgradeReader
	// LiveHalt serves what the halt watch sees during an upgrade's halt;
	// nil (no watch) answers /api/v1/upgrades/live as never halted.
	LiveHalt controller.LiveHaltReader
	// Status serves the status snapshots; nil leaves /api/v1/status out.
	Status controller.StatusReader
	// Stats reads the home tiles and the validators header; nil leaves
	// /api/v1/stats out and answers the header as null.
	Stats controller.StatsReader
	// Daily reads the daily series of /api/v1/stats; nil answers it empty.
	Daily controller.DailyStatsReader
	// Params reads the live module parameters and ParamChanges their
	// recorded changes; /api/v1/params needs both.
	Params       controller.LiveParams
	ParamChanges controller.ParamChangeReader
	// HaltedDenoms reads the denoms governance halted on the DEX; nil
	// leaves /api/v1/params/halted-denoms out.
	HaltedDenoms controller.HaltedDenomsReader
	// StakeFlows serves the validators' stake movements of the last 7 days
	// from memory and ValidatorNames names their validators; nil StakeFlows
	// leaves /api/v1/validators/stake-changes and
	// /api/v1/validators/{operator}/movements out.
	StakeFlows     controller.StakeFlowReader
	ValidatorNames controller.ValidatorNames
	// Raw serves the node's raw by-height JSON; nil leaves /api/v1/raw out.
	Raw controller.RawReader
	// CompatPrices serves CoinGecko's prices; nil leaves the
	// aggregator-compatible /api/prices out.
	CompatPrices controller.PriceFeed
	// CompatArticles serves the Medium articles; nil leaves the
	// aggregator-compatible /api/articles/medium out.
	CompatArticles controller.ArticleFeed
	// CORSAllowedOrigins enables CORS for these origins ("*" for any); empty
	// sends no CORS headers. The aggregator-compatible routes ignore it.
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
			Skipper:       isCompat,
			AllowOrigins:  deps.CORSAllowedOrigins,
			AllowMethods:  []string{http.MethodGet, http.MethodHead, http.MethodOptions},
			ExposeHeaders: []string{appmw.RequestIDHeader},
		}))
	}
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		Skipper:       func(c *echo.Context) bool { return !isCompat(c) },
		AllowOrigins:  []string{"*"},
		AllowMethods:  []string{http.MethodGet, http.MethodHead, http.MethodOptions},
		ExposeHeaders: []string{appmw.RequestIDHeader},
	}))
	e.Use(appmw.Recover())

	health := controller.NewHealthController()
	e.GET("/health", health.Health)

	explorer := controller.NewExplorerController(deps.Explorer, deps.Stats)
	api := e.Group(APIPrefix)
	api.GET("/blocks", explorer.Blocks)
	api.GET("/blocks/:height", explorer.Block)
	api.GET("/blocks/:height/events", explorer.BlockEvents)
	api.GET("/blocks/:height/transfers", explorer.BlockTransfers)
	api.GET("/block-events", explorer.ChainEvents)
	api.GET("/txs", explorer.Txs)
	api.GET("/txs/:hash", explorer.Tx)
	api.GET("/orders/:id", explorer.Order)
	api.GET("/ibc/channels", explorer.IBCChannels)
	api.GET("/modules", explorer.Modules)
	api.GET("/validators", explorer.Validators)
	api.GET("/validators/hall-of-fame", explorer.HallOfFame)
	if deps.StakeFlows != nil {
		flows := controller.NewStakeFlowController(deps.StakeFlows, deps.ValidatorNames)
		// Before /validators/:operator, which would otherwise read the path
		// as an operator.
		api.GET("/validators/stake-changes", flows.StakeChanges)
		api.GET("/validators/:operator/movements", flows.Movements)
	}
	api.GET("/validators/:operator", explorer.Validator)
	api.GET("/validators/:operator/blocks", explorer.ValidatorBlocks)
	api.GET("/search", explorer.Search)
	if deps.Accounts != nil {
		accounts := controller.NewAccountController(deps.Accounts, deps.AccountState, deps.OpenOrders)
		api.GET("/accounts/:address", accounts.Account)
		api.GET("/accounts/:address/activity", accounts.Activity)
		api.GET("/accounts/:address/orders", accounts.Orders)
		api.GET("/accounts/:address/fills", accounts.Fills)
		api.GET("/accounts/:address/ibc", accounts.IBCTransfers)
		if deps.OpenOrders != nil {
			api.GET("/accounts/:address/open-orders", accounts.OpenOrders)
		}
	}
	if deps.Tokens != nil {
		tokens := controller.NewTokenController(deps.Tokens)
		api.GET("/tokens", tokens.Tokens)
		api.GET("/tokens/:denom", tokens.Token)
		api.GET("/tokens/:denom/events", tokens.TokenEvents)
		api.GET("/tokens/:denom/transfers", tokens.TokenTransfers)
		api.GET("/tokens/:denom/holders", tokens.TokenHolders)
		api.GET("/token", tokens.Token)
		api.GET("/token/events", tokens.TokenEvents)
		api.GET("/token/transfers", tokens.TokenTransfers)
		api.GET("/token/holders", tokens.TokenHolders)
	}
	if deps.Proposals != nil {
		proposals := controller.NewProposalController(deps.Proposals)
		api.GET("/proposals", proposals.Proposals)
		api.GET("/proposals/:id", proposals.Proposal)
		api.GET("/proposals/:id/votes", proposals.ProposalVotes)
		api.GET("/proposals/:id/deposits", proposals.ProposalDeposits)
		api.GET("/proposals/:id/non-voters", proposals.ProposalNonVoters)
	}
	api.GET("/upgrades/live", controller.NewHaltController(deps.LiveHalt, deps.ValidatorNames).Live)
	if deps.Upgrades != nil {
		api.GET("/upgrades/:height", controller.NewUpgradeController(deps.Upgrades).Upgrade)
	}
	if deps.Stats != nil {
		api.GET("/stats", controller.NewStatsController(deps.Stats, deps.Daily).Stats)
	}
	if deps.Params != nil && deps.ParamChanges != nil {
		api.GET("/params", controller.NewParamsController(deps.Params, deps.ParamChanges).Params)
	}
	if deps.HaltedDenoms != nil {
		api.GET("/params/halted-denoms", controller.NewHaltedDenomsController(deps.HaltedDenoms).HaltedDenoms)
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

	// The aggregator-compatible routes: every endpoint ported from the BZE
	// aggregator API is added here.
	compat := e.Group(CompatPrefix)
	if deps.CompatPrices != nil {
		compat.GET("/prices", controller.NewCompatPricesController(deps.CompatPrices).Prices)
	}
	if deps.CompatArticles != nil {
		compat.GET("/articles/medium", controller.NewCompatArticlesController(deps.CompatArticles).Medium)
	}

	return e
}

// isCompat reports whether c's path is an aggregator-compatible one: under
// CompatPrefix, outside APIPrefix.
func isCompat(c *echo.Context) bool {
	p := c.Request().URL.Path
	if p != CompatPrefix && !strings.HasPrefix(p, CompatPrefix+"/") {
		return false
	}
	return p != APIPrefix && !strings.HasPrefix(p, APIPrefix+"/")
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
