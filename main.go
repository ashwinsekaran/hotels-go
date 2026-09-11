// Command hotels is the composition root: it reads config from the environment,
// sets up observability (structured logging + tracing), builds every layer
// bottom-up (repo → use-case factories → handlers), wires routing and the
// cross-cutting concerns, and runs the server with graceful shutdown. This is the
// ONLY file allowed to import every layer.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ashwinsekaran/hotels-go/auth"
	"github.com/ashwinsekaran/hotels-go/handlers"
	"github.com/ashwinsekaran/hotels-go/health"
	"github.com/ashwinsekaran/hotels-go/metrics"
	"github.com/ashwinsekaran/hotels-go/repo"
	"github.com/ashwinsekaran/hotels-go/tracing"
	"github.com/ashwinsekaran/hotels-go/uc"

	"github.com/julienschmidt/httprouter"
	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Compile-time assertion that the repo still satisfies the use-case port; this is
// the one place already importing both, so a drift fails the build early.
var _ uc.HotelStore = (*repo.HotelRepo)(nil)

// Config is populated from the environment by envconfig. split_words maps
// HttpAddress → HTTP_ADDRESS, etc. In k8s, SHUTDOWN_DELAY should be >= the
// readiness probe period so the load balancer observes not-ready before the
// listener closes.
type Config struct {
	HttpAddress   string        `split_words:"true" default:":8080"`
	ShutdownDelay time.Duration `split_words:"true" default:"5s"`
	AuthToken     string        `split_words:"true" default:"changeme"`
	ServiceName   string        `split_words:"true" default:"hotels"`
	OtelEndpoint  string        `split_words:"true" default:"localhost:4317"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	var config Config
	if err := envconfig.Process("", &config); err != nil {
		logger.Error("config", "err", err)
		os.Exit(1)
	}

	// Tracing: global OTLP/gRPC TracerProvider (Jaeger speaks OTLP). Non-fatal if
	// the collector is down — spans are dropped and the service still serves.
	shutdownTracer, err := tracing.Init(context.Background(), config.ServiceName, config.OtelEndpoint)
	if err != nil {
		logger.Error("tracing init", "err", err)
		shutdownTracer = func(context.Context) error { return nil }
	}

	m := metrics.NewMetrics()
	reg := m.Register()

	api := httprouter.New()

	// PER-RESOURCE WIRING (Hotel) — repo → use-case factories → handlers.
	hotelRepo := repo.NewHotelRepo()
	ingestHotels := uc.MakeIngestHotelsUc(hotelRepo)
	listHotels := uc.MakeListHotelsUc(hotelRepo)
	getHotel := uc.MakeGetHotelUc(hotelRepo)
	api.Handle("POST", "/hotels", metrics.Middleware(m, handlers.IngestHotelsHandler(logger, ingestHotels)))
	api.Handle("GET", "/hotels", metrics.Middleware(m, handlers.ListHotelsHandler(logger, listHotels)))
	api.Handle("GET", "/hotels/:id", metrics.Middleware(m, handlers.GetHotelHandler(logger, getHotel)))
	// END PER-RESOURCE WIRING

	ready := &health.Readiness{}

	// otelhttp wraps the API router so every business request gets a server span
	// in its context; auth runs inside that span.
	traced := otelhttp.NewHandler(
		auth.Auth(config.AuthToken, api),
		"http.server",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)

	// Outer mux carries infra endpoints WITHOUT auth or tracing and forwards
	// everything else to the traced, auth-wrapped API router.
	root := http.NewServeMux()
	root.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	root.HandleFunc("/.well-known/live", health.Live)
	root.HandleFunc("/.well-known/ready", ready.Handler)
	root.Handle("/", traced)

	server := &http.Server{Addr: config.HttpAddress, Handler: root}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	ready.SetReady(true)

	go func() {
		logger.Info("listening", "addr", config.HttpAddress)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	<-shutdown
	logger.Info("shutting down")

	// Flip readiness first so the LB drains us; liveness stays 200 meanwhile.
	ready.SetReady(false)
	if config.ShutdownDelay > 0 {
		logger.Info("draining before shutdown", "delay", config.ShutdownDelay.String())
		time.Sleep(config.ShutdownDelay)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server shutdown", "err", err)
	}
	if err := shutdownTracer(ctx); err != nil {
		logger.Error("tracer shutdown", "err", err)
	}
}
