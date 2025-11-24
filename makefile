server:
	go build -o server server/main.go

client:
	go build -o client client/main.go

both:
	go build -o server server/main.go
	go build -o client client/main.go

clean:
	rm -f server client
