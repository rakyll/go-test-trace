// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type parser struct {
	globalCtx context.Context
	tracer    oteltrace.Tracer
}

func newParser(ctx context.Context, tracer oteltrace.Tracer) (*parser, error) {
	return &parser{globalCtx: ctx, tracer: tracer}, nil
}

func (p *parser) parse(r io.Reader) error {
	reader := bufio.NewReader(r)
	for {
		l, _, err := reader.ReadLine()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p.parseLine(string(l))
	}
}

func (p *parser) parseLine(line string) {
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
		p.start(trimmed)

		// finished
	case strings.HasPrefix(trimmed, "--- PASS"):
		fallthrough
	case strings.HasPrefix(trimmed, "ok"):
		p.end(trimmed, false)

		// failed
	case strings.HasPrefix(trimmed, "--- FAIL"):
		// end segment with error
		p.end(trimmed, true)
	}

}

func (p *parser) start(line string) error {
	name := parseName(line)
	_, span := p.tracer.Start(p.globalCtx, name)
	collectedSpans[name] = &spanData{
		span:      span,
		startTime: time.Now(),
	}
	return nil
}

func (p *parser) end(line string, errored bool) {
	name, dur := parseNameAndDuration(line)
	data, ok := collectedSpans[name]
	if !ok {
		return
	}
	if errored {
		data.span.SetStatus(codes.Error, "")
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
