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
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

var (
	endpoint    string
	name        string
	stdin       bool
	traceparent string
	help        bool
)

type spanData struct {
	ctx       context.Context
	span      oteltrace.Span
	startTime time.Time
}

var collectedSpans = make(map[string]*spanData, 1000)

func main() {
	fset := flag.NewFlagSet("", flag.ContinueOnError)
	fset.StringVar(&endpoint, "endpoint", "127.0.0.1:55680", "")
	fset.StringVar(&name, "name", "go-test-trace", "")
	fset.BoolVar(&stdin, "stdin", false, "")
	fset.BoolVar(&help, "help", false, "")
	fset.StringVar(&traceparent, "traceparent", "", "")
	fset.Usage = func() {} // don't error instead pass remaining arguments to go test
	fset.Parse(os.Args[1:])

	if help {
		fmt.Println(usageText)
		os.Exit(0)
	}
	if err := trace(fset.Args()); err != nil {
		log.Fatal(err)
	}
}

func trace(args []string) error {
	ctx := context.Background()
	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithTimeout(100*time.Millisecond),
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
	t := otel.Tracer(name)

	// If there is a parent trace, participate into it.
	// If not, create a new root span.
	if traceparent != "" {
		propagation := propagation.TraceContext{}
		ctx = propagation.Extract(ctx, &carrier{traceparent: traceparent})
	}

	globalCtx, globalSpan := t.Start(ctx, name)
	defer func() {
		globalSpan.End()
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
	cmd.Env = append(
		os.Environ(),
		fmt.Sprintf("TRACEPARENT=%q", globalSpan.SpanContext().TraceID()),
	)
	r, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	decoder := json.NewDecoder(r)
	go func() {
		for decoder.More() {
			var data goTestOutput
			if err := decoder.Decode(&data); err != nil {
				if err == io.EOF {
					return
				}
				log.Printf("Failed to decode JSON: %v", err)
			}

			key := testKey(data.Package, data.Test)
			switch data.Action {
			case "start":
				ctx, span := t.Start(globalCtx, data.Package, oteltrace.WithTimestamp(data.Time))
				collectedSpans[key] = &spanData{
					ctx:       ctx,
					span:      span,
					startTime: data.Time,
				}
			case "run":
				ctx, span := t.Start(parentContext(globalCtx, data.Package, data.Test), data.Test, oteltrace.WithTimestamp(data.Time))
				collectedSpans[key] = &spanData{
					ctx:       ctx,
					span:      span,
					startTime: data.Time,
				}
			case "pass", "fail", "skip":
				spanData, ok := collectedSpans[key]
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

type goTestOutput struct {
	Time    time.Time
	Action  string
	Package string
	Test    string
	Output  string
}

func testKey(pkg, test string) string {
	if test == "" {
		return pkg
	}
	return pkg + "." + test
}

func parentContext(ctx context.Context, pkg, test string) context.Context {
	// For a test "a/b/c" try to take parent "a/b" then "a".
	until := len(test)
	for {
		sep := strings.LastIndex(test[:until], "/")
		if sep == -1 {
			break
		}
		until = sep
		if testData, ok := collectedSpans[testKey(pkg, test[:until])]; ok {
			return testData.ctx
		}
	}

	// Try to use the parent's context.
	if pkgData, ok := collectedSpans[pkg]; ok {
		return pkgData.ctx
	}

	// Use the fallback context.
	return ctx
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

const usageText = `Usage:
go-test-trace [flags...] [go test flags...]

Flags:
-name        Name of the trace span created for the test, optional.
-endpoint    OpenTelemetry gRPC collector endpoint, 127.0.0.1:55680 by default.
-traceparent Trace to participate into if any, in W3C Trace Context format.
-stdin       Parse go test verbose output from stdin.
-help        Print this text.

Run "go help test" for go test flags.`
