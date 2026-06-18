package telemetry

import (
	"context"
	"log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// InitMetrics initializes the OpenTelemetry metrics exporter and sets up the global MeterProvider.
func InitMetrics(ctx context.Context, collectorAddr string) (*metric.MeterProvider, error) {

	// 1. Configure the OTLP gRPC exporter (pointing to port 4317 of the OTel Collector).
	exporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithEndpoint(collectorAddr),
	)
	if err != nil {
		return nil, err
	}

	// 2. Define resource metadata (identifying which service issued the indicator)
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("exploreService"),
			semconv.ServiceVersionKey.String("0.1.0"),
		),
	)

	if err != nil {
		return nil, err
	}

	// 3. Configure MeterProvider and set it to automatically push data every 10 seconds (Metric Export Interval).
	mp := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(exporter, metric.WithInterval(10*time.Second))),
	)

	// 4. Configure a global Meter feed
	otel.SetMeterProvider(mp)

	log.Printf("📡 OTLP Metrics pusher initialized. Exporting to %s via gRPC", collectorAddr)

	return mp, nil
}

func ShutdownMetrics(ctx context.Context, mp *metric.MeterProvider) {
	log.Println("🔄 Shutting down OTLP Metrics provider...")
	if err := mp.Shutdown(ctx); err != nil {
		log.Printf("❌ Error during OTLP shutdown: %v", err)
	}
}
