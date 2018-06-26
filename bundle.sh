#!/bin/sh

# Generate the Caddy binary
mediaSearchDir=$(pwd)
binDir=$mediaSearchDir/bin
caddyDir=$GOPATH/src/github.com/mholt/caddy 
cd $caddyDir
CGO_ENABLED=0 GOOS=linux go build -o $binDir/caddy_linux ./caddy
cd $mediaSearchDir
make build-microservices

# Now zip the microservices
outZip=deployment.zip
zip -r $outZip bin/*_linux
zip -r $outZip prometheus.yml static README.md
zip -r $outZip clients
