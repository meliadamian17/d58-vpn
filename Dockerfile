FROM ubuntu:20.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    mininet \
    openvswitch-switch \
    openvswitch-testcontroller \
    python2 \
    python-is-python2 \
    iproute2 \
    iptables \
    iputils-ping \
    iputils-tracepath \
    traceroute \
    net-tools \
    tcpdump \
    curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

RUN touch /etc/network/interfaces

RUN ln -s /usr/bin/ovs-testcontroller /usr/bin/controller

COPY bin/server /usr/local/bin/vpn-server
COPY bin/client /usr/local/bin/vpn-client

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

COPY topology.py /app/topology.py

EXPOSE 443

ENTRYPOINT ["/entrypoint.sh"]

CMD ["/bin/bash"]
