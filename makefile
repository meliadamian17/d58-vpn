clean:
	rm -rf bin/ server.key server.crt

build:
	GOOS=linux GOARCH=amd64 go build -o bin/server cmd/server/main.go
	GOOS=linux GOARCH=amd64 go build -o bin/client cmd/client/main.go