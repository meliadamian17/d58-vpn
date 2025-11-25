.PHONY: all server client clean cert

all: server client

server:
	go build -o bin/server cmd/server/main.go

client:
	go build -o bin/client cmd/client/main.go

cert:
	@echo "Generating self-signed certificate..."
	openssl req -new -newkey rsa:2048 -days 365 -nodes -x509 \
		-keyout server.key -out server.crt \
		-subj "/C=US/ST=State/L=City/O=Organization/OU=Unit/CN=localhost"

clean:
	rm -rf bin/ server.key server.crt
