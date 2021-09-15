# go-test-trace

go-test-trace is like go test but it also generates
distributed traces.

Generated traces are exported in OTLP to a
[OpenTelemetry collector](https://github.com/open-telemetry/opentelemetry-collector).
You need to run go-test-trace alongside a collector to export data to
distributed tracing service.

![Honeycomb](https://i.imgur.com/E18PYk4.png)

## Installation

```
go get -u github.com/rakyll/go-test-trace
```

## Usage

You can use go-test-trace as a drop-in replacement for go test.
It will generate a distributed trace and export it to a collector
available at "127.0.0.1:55680".

```
$ go-test-trace ./example
=== RUN   TestStart
--- PASS: TestStart (0.50s)
=== RUN   TestStartWithOptions
--- PASS: TestStartWithOptions (1.00s)
=== RUN   TestFileParser
--- FAIL: TestFileParser (0.30s)
=== RUN   TestLoading
=== PAUSE TestLoading
=== RUN   TestLoading_abort
=== PAUSE TestLoading_abort
=== RUN   TestLoading_interrupt
=== PAUSE TestLoading_interrupt
=== CONT  TestLoading
=== CONT  TestLoading_abort
=== CONT  TestLoading_interrupt
--- PASS: TestLoading_interrupt (0.08s)
--- PASS: TestLoading (1.00s)
--- PASS: TestLoading_abort (2.50s)
FAIL
FAIL	github.com/rakyll/go-test-trace/example	4.823s
exit status 1
make: *** [default] Error 1
```

Alternatively, you can use -stdin option to parse
the output of go test. This option won't be as accurate
in terms of timing because it will generate trace spans
as it sees output in the stdin.

```
$ go test -v ./example | go-test-trace -stdin
=== RUN   TestStart
--- PASS: TestStart (0.50s)
=== RUN   TestStartWithOptions
--- PASS: TestStartWithOptions (1.00s)
=== RUN   TestFileParser
--- FAIL: TestFileParser (0.30s)
=== RUN   TestLoading
=== PAUSE TestLoading
=== RUN   TestLoading_abort
=== PAUSE TestLoading_abort
=== RUN   TestLoading_interrupt
=== PAUSE TestLoading_interrupt
=== CONT  TestLoading
=== CONT  TestLoading_abort
=== CONT  TestLoading_interrupt
--- PASS: TestLoading_interrupt (0.08s)
--- PASS: TestLoading (1.00s)
--- PASS: TestLoading_abort (2.50s)
FAIL
FAIL	github.com/rakyll/go-test-trace/example	4.823s
exit status 1
make: *** [default] Error 1
```

You can export to any collector by using the endpoint option:

```
$ go-test-trace ./example -endpoint=my-otel-collector.io:9090
...
```

## Running the collector

An example collector configuration is available at example/collector.yaml.
Please edit the write key and data set before use.
Then, you can run the collector locally by the following command
and export the traces to Honeycomb:

```
$ docker run --rm -p 4317:4317 -p 55680:55680 -p 8888:8888 \
    -v "${PWD}/example/collector.yaml":/collector.yaml \
    --name awscollector public.ecr.aws/aws-observability/aws-otel-collector:latest \
    --config collector.yaml;
```

You can use any configuration supported by [ADOT](https://github.com/aws-observability/aws-otel-collector)
or export to any other OpenTelemetry collector.
