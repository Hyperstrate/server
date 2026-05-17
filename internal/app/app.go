package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"strings"

	"hyperstrate/server/internal/config"
	dbmodule "hyperstrate/server/internal/db"
	"hyperstrate/server/internal/modules/ai"
	"hyperstrate/server/internal/modules/auth"
	"hyperstrate/server/internal/modules/observability"
	"hyperstrate/server/internal/modules/prompts"
	"hyperstrate/server/internal/modules/router"
	routerApplication "hyperstrate/server/internal/modules/router/application"
	"hyperstrate/server/internal/shared/httpserver"
	"hyperstrate/server/internal/shared/metrics"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			config.New,
			dbmodule.New,
			httpserver.NewRouter,
		),
		ai.Module(),
		auth.Module(),
		prompts.Module(),
		router.Module(),
		observability.Module(),
		fx.Invoke(registerMetricsEndpoint),
	)
}

// registerMetricsEndpoint mounts GET /metrics in Prometheus text format.
// Aggregates all registered Collector implementations.
func registerMetricsEndpoint(r *gin.Engine, routerSvc routerApplication.Service) {
	// The router pipeline metrics implement metrics.Collector via MetricsSnapshot.
	// We wrap the service in an inline adapter to satisfy the interface.
	collector := &routerMetricsCollector{svc: routerSvc}
	handler := metrics.Handler(collector)
	r.GET("/metrics", func(c *gin.Context) {
		handler(c.Writer, c.Request)
	})
}

// routerMetricsCollector bridges routerApplication.Service to metrics.Collector.
type routerMetricsCollector struct {
	svc routerApplication.Service
}

func (c *routerMetricsCollector) Collect(buf *strings.Builder) {
	for _, snap := range c.svc.MetricsSnapshot() {
		for status, count := range snap.Counts {
			buf.WriteString("# HELP router_requests_total Total requests by router and status.\n")
			buf.WriteString("# TYPE router_requests_total counter\n")
			fmt.Fprintf(buf, "router_requests_total{router_id=%q,status=%q} %d\n",
				snap.RouterID, status, count)
		}
		buf.WriteString("# HELP router_avg_latency_ms Average request latency in milliseconds.\n")
		buf.WriteString("# TYPE router_avg_latency_ms gauge\n")
		fmt.Fprintf(buf, "router_avg_latency_ms{router_id=%q} %g\n", snap.RouterID, snap.AvgLatMs)
		buf.WriteString("# HELP router_total_requests Total requests handled.\n")
		buf.WriteString("# TYPE router_total_requests counter\n")
		fmt.Fprintf(buf, "router_total_requests{router_id=%q} %d\n", snap.RouterID, snap.TotalReqs)
	}
}

func NewHTTPApp() *fx.App {
	return fx.New(
		Module(),
		fx.Invoke(startHTTPServer),
	)
}

func NewLambdaApp(populateTargets ...interface{}) *fx.App {
	options := []fx.Option{Module()}
	if len(populateTargets) > 0 {
		options = append(options, fx.Populate(populateTargets...))
	}
	return fx.New(options...)
}

// NewWorkerApp builds a minimal Fx app for the SQS worker Lambda.
func NewWorkerApp(populateTargets ...interface{}) *fx.App {
	options := []fx.Option{
		fx.Provide(
			config.New,
			dbmodule.New,
		),
		ai.WorkerModule(),
	}
	if len(populateTargets) > 0 {
		options = append(options, fx.Populate(populateTargets...))
	}
	return fx.New(options...)
}

func startHTTPServer(lc fx.Lifecycle, cfg config.Config, router *gin.Engine) {
	server := &http.Server{
		Addr:              fmt.Sprintf(":%s", cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				slog.Info("HTTP server listening", "port", cfg.Port)
				if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					slog.Error("server failed", "err", err)
					os.Exit(1)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return server.Shutdown(ctx)
		},
	})
}
