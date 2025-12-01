#!/bin/bash

if [[ "$(docker images -q vpn-mininet 2> /dev/null)" == "" ]]; then
  echo "Building Docker image..."
  docker build -t vpn-mininet .
fi

echo "Starting Portable Mininet & VPN Environment..."
echo "Binaries are located in /usr/local/bin/ (vpn-client, vpn-server)"

docker run --privileged -it --rm --name vpn-mininet \
  -p 443:443 \
  vpn-mininet
