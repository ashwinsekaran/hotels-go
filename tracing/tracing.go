// Package tracing wires OpenTelemetry tracing with an OTLP/gRPC exporter. Jaeger
// speaks OTLP natively (enable COLLECTOR_OTLP_ENABLED and expose :4317), so no
// Jaeger-specific exporter is needed — the deprecated go.opentelemetry.io/otel/
// exporters/jaeger package is intentionally NOT used.
//
// Cross-cutting, like metrics/health/auth: the composition root calls Init once
// and defers the returned shutdown so buffered spans are flushed on exit. Nothing
// in ent/uc/repo/handlers imports this package; spans reach handlers via the
// request context that the otelhttp server handler installs in main.
package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

// Init installs a global TracerProvider that batches spans to an OTLP/gRPC
// endpoint (host:port, no scheme — e.g. "jaeger:4317") and sets the W3C
// trace-context + baggage propagators. It returns a shutdown func the caller
// MUST defer/call to flush spans. WithInsecure sends plaintext, which is fine
// for local/dev; use TLS credentials in production.
func Init(ctx context.Context, serviceName, endpoint string) (func(context.Context) error, error) {
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	// service.name is the attribute Jaeger shows in its Service dropdown.
	res, err := resource.Merge(resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}
