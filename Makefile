all: install protoc binaries

install:
	go get ./...

protoc:
	protoc -I rpc rpc/defs.proto --go_out=plugins=grpc:rpc

binaries: backends_bin frontend_bin detailer_bin
	
run-microservices: binaries
	./bin/detailer_mu &
	./bin/backends_mu &
	./bin/frontend_mu &

kill-microservices:
	sudo pkill detailer_mu backends_mu frontend_mu
	
detailer_bin:
	go build -o ./bin/detailer_mu ./detailer

backends_bin:
	go build -o bin/backends_mu  ./backends

frontend_bin:
	go build -o bin/frontend_mu  .

build-microservices:
	CGO_ENABLED=0 GOOS=linux go build -o ./bin/detailer_mu_linux ./detailer
	CGO_ENABLED=0 GOOS=linux go build -o ./bin/backends_mu_linux ./backends
	CGO_ENABLED=0 GOOS=linux go build -o ./bin/frontend_mu_linux .

clean:
	rm -rf bin/
