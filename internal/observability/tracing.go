// Package observability wires OpenTelemetry tracing for the gateway.
// v1 exports spans to stdout so traces are visible with no extra infra.
package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// InitTracing sets up a global TracerProvider exporting spans to stdout.
// Returns a shutdown func the caller must defer to flush spans on exit.
func InitTracing(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	// Stdout exporter: prints spans as JSON. Swap for OTLP/Jaeger later.
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, err
	}

	// Batcher exports spans off the hot path, so tracing never slows a request.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}