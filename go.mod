module github.com/rakyll/go-test-xray

go 1.14

require (
	go.opentelemetry.io/otel v1.0.0-RC3
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.0.0-RC3
	go.opentelemetry.io/otel/sdk v1.0.0-RC3
	go.opentelemetry.io/otel/trace v1.0.0-RC3
	golang.org/x/net v0.0.0-20210614182718-04defd469f4e // indirect
	google.golang.org/grpc v1.40.0
)
