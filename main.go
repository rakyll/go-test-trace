// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// go-test-trace is a tiny program that generates OpenTelemetry
// traces when testing a Go package.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

var (
	endpoint string
	stdin    bool
)

type spanData struct {
	span      oteltrace.Span
	startTime time.Time
}

var collectedSpans = make(map[string]*spanData, 1000)

func main() {
	fset := flag.NewFlagSet("", flag.ContinueOnError)
	fset.StringVar(&endpoint, "endpoint", "127.0.0.1:55680", "OpenTelemetry gRPC endpoint to send traces")
	fset.BoolVar(&stdin, "stdin", false, "read from stdin")
	fset.Usage = func() {} // pass all arguments to go test
	fset.Parse(os.Args[1:])

	if err := trace(fset.Args()); err != nil {
		log.Fatal(err)
	}
}

func trace(args []string) error {
	ctx := context.Background()
	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithDialOption(grpc.WithBlock()),
	)
	if err != nil {
		return err
	}
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceNameKey.String("go test"),
	))
	if err != nil {
		return err
	}
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(traceExporter)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tracerProvider)

	const name = "go-test-trace"
	t := otel.Tracer(name)
	globalCtx, span := t.Start(ctx, name)

	defer func() {
		span.End()
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			log.Printf("Failed shutting down the tracer provider: %v", err)
		}
	}()

	if stdin {
		p, err := newParser(globalCtx, t)
		if err != nil {
			return err
		}
		return p.parse(os.Stdin)
	}

	// Otherwise, act like a drop-in replacement for `go test`.
	goTestArgs := append([]string{"test"}, args...)
	goTestArgs = append(goTestArgs, "-json")
	cmd := exec.Command("go", goTestArgs...)

	r, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	decoder := json.NewDecoder(r)
	go func() {
		for decoder.More() {
			var data goTestJSON
			if err := decoder.Decode(&data); err != nil {
				if err == io.EOF {
					return
				}
				log.Printf("Failed to decode JSON: %v", err)
			}
			switch data.Action {
			case "run":
				var span oteltrace.Span
				_, span = t.Start(globalCtx, data.Test, oteltrace.WithTimestamp(data.Time))
				collectedSpans[data.Test] = &spanData{
					span:      span,
					startTime: data.Time,
				}
			case "pass", "fail", "skip":
				spanData, ok := collectedSpans[data.Test]
				if !ok {
					return // should never happen
				}
				if data.Action == "fail" {
					spanData.span.SetStatus(codes.Error, "")
				}
				spanData.span.End(oteltrace.WithTimestamp(data.Time))
			}
			fmt.Print(data.Output)
		}
	}()
	return cmd.Run()
}

type goTestJSON struct {
	Time   time.Time
	Action string
	Test   string
	Output string
}

type carrier struct{ traceparent string }

func (c *carrier) Get(key string) string {
	if key == "traceparent" {
		return c.traceparent
	}
	return ""
}

func (c *carrier) Set(key string, value string) {
	panic("not implemented")
}

func (c *carrier) Keys() []string {
	return []string{"traceparent"}
}
