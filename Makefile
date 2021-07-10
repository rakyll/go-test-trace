all:
	go test -a -v -count=1 ./example | go run main.go