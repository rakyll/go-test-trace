default:
	go run main.go parser.go ./example

stdin:
	go test -v -count=1 ./example | go run main.go parser.go -stdin

