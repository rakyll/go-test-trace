// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// gotest is a tiny program that shells out to `go test`
// and prints the output in color.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

func main() {
	endpoint := flag.String("endpoint", "127.0.0.1:55680", "OpenTelemetry gRPC endpoint to send traces")
	flag.Parse()

	t, err := newTracer(*endpoint)
	if err != nil {
		log.Fatal(err)
	}
	if err := t.Parse(os.Stdin); err != nil {
		log.Fatal(err)
	}
}

type tracer struct {
	globalCtx     context.Context
	tracer        oteltrace.Tracer
	traceProvider *sdktrace.TracerProvider
	spans         map[string]*spanData
}

type spanData struct {
	span      oteltrace.Span
	startTime time.Time
}

func newTracer(endpoint string) (*tracer, error) {
	ctx := context.Background()
	traceExporter, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithDialOption(grpc.WithBlock()),
	)
	if err != nil {
		return nil, err
	}
	res, err := resource.New(ctx, resource.WithAttributes(attribute.String("service.name", "go test")))
	if err != nil {
		return nil, err
	}
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(traceExporter)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tracerProvider)

	t := otel.Tracer("go-test-tracer")
	globalCtx, _ := t.Start(ctx, "go-test-otel")
	return &tracer{
		globalCtx:     globalCtx,
		tracer:        t,
		traceProvider: tracerProvider,
		spans:         make(map[string]*spanData, 1000),
	}, nil
}

func (t *tracer) Parse(r io.Reader) error {
	reader := bufio.NewReader(r)
	for {
		l, _, err := reader.ReadLine()
		if err == io.EOF {
			oteltrace.SpanFromContext(t.globalCtx).End()
			t.traceProvider.Shutdown(context.Background())
			return nil
		}
		if err != nil {
			return err
		}
		t.parse(string(l))
	}
}

func (t *tracer) parse(line string) {
	defer fmt.Printf("%s\n", line)

	trimmed := strings.TrimSpace(line)

	switch {
	case strings.HasPrefix(trimmed, "ok"):
		// Do nothing.
	case strings.HasPrefix(trimmed, "PASS"):
		// Do nothing.
	case strings.HasPrefix(trimmed, "FAIL"):
		// Do nothing.
	case strings.Contains(trimmed, "[no test files]"):
		// TODO(jbd): Annotate.
	case strings.HasPrefix(trimmed, "--- SKIP"):
		// TODO(jbd): Annotate.

		// start segment
	case strings.HasPrefix(trimmed, "=== RUN"):
		t.start(trimmed)

		// finished
	case strings.HasPrefix(trimmed, "--- PASS"):
		fallthrough
	case strings.HasPrefix(trimmed, "ok"):
		t.end(trimmed, false)

		// failed
	case strings.HasPrefix(trimmed, "--- FAIL"):
		// end segment with error
		t.end(trimmed, true)
	}

}

func (t *tracer) start(line string) error {
	name := parseName(line)
	_, span := t.tracer.Start(t.globalCtx, name)
	t.spans[name] = &spanData{
		span:      span,
		startTime: time.Now(),
	}
	return nil
}

func (t *tracer) end(line string, errored bool) {
	name, dur := parseNameAndDuration(line)
	data, ok := t.spans[name]
	if !ok {
		return
	}
	data.span.End(oteltrace.WithTimestamp(data.startTime.Add(dur)))
}

func parseName(line string) string {
	return testNameRegex.FindAllStringSubmatch(line, -1)[0][0]
}

func parseNameAndDuration(line string) (string, time.Duration) {
	m := testNameWithDurationRegex.FindAllStringSubmatch(line, -1)
	name := m[0][1]
	duration := m[0][2]

	dur, _ := time.ParseDuration(duration)
	return name, dur
}

var (
	testNameRegex             = regexp.MustCompile(`(Test.+)`)
	testNameWithDurationRegex = regexp.MustCompile(`(Test.+)\s\(([\w|\.]+)\)`)
)
