build:
	go build -o ./bin/blockchainProject

run: build
	./bin/blockchainProject

test:
	go test ./...