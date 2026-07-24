// Package observability wires OpenTelemetry tracing for the gateway.
// Spans are exported over OTLP to a local collector (Jaeger).
package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// otlpEndpoint is the local collector. Move to config when we deploy anywhere
// other than a developer machine.
const otlpEndpoint = "localhost:4317"

// InitTracing sets up a global TracerProvider exporting spans over OTLP.
// Returns a shutdown func the caller must defer to flush spans on exit.
func InitTracing(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	// Insecure is fine for a local collector; use TLS for anything remote.
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(otlpEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	// Schemaless resource carries the service name without a schema URL, so it
	// can't clash with the SDK's own schema version.
	res := resource.NewSchemaless(
		attribute.String("service.name", serviceName),
	)

	// Batcher exports spans off the hot path, so tracing never slows a request.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}