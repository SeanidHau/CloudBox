package telemetry

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func SetupTracing(serviceName string, exportedName string) (func(context.Context) error, error) {
	if exportedName == "none" {
		return func(context.Context) error {
			return nil
		}, nil
	}

	if exportedName != "stdout" {
		return nil, fmt.Errorf("unsupported trace exporter: %s", exportedName)
	}

	exporter, err := stdouttrace.New(
		stdouttrace.WithWriter(os.Stdout),
	)
	if err != nil {
		return nil, fmt.Errorf("create stdout trace exporter: %w", err)
	}

	traceResource, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			"",
			attribute.String("service.name", serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create trace resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(traceResource),
		sdktrace.WithBatcher(exporter),
	)

	otel.SetTracerProvider(provider)

	otel.SetTextMapPropagator(propagation.TraceContext{})

	return provider.Shutdown, nil
}
