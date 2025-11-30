#!/bin/bash
set -e

# Start OVSDB
mkdir -p /var/run/openvswitch
mkdir -p /etc/openvswitch
if [ ! -f /etc/openvswitch/conf.db ]; then
    ovsdb-tool create /etc/openvswitch/conf.db /usr/share/openvswitch/vswitch.ovsschema
fi

# Start ovsdb-server
ovsdb-server --remote=punix:/var/run/openvswitch/db.sock \
             --remote=db:Open_vSwitch,Open_vSwitch,manager_options \
             --pidfile --detach --log-file

# Initialize DB
ovs-vsctl --no-wait init

ovs-vswitchd --pidfile --detach --log-file

# Wait for OVS
echo "Waiting for OVS to be ready..."
for i in {1..10}; do
    if ovs-vsctl show > /dev/null 2>&1; then
        echo "OVS is running."
        break
    fi
    sleep 1
done

# Ensure controller service is stopped (Mininet needs to start it)
service openvswitch-testcontroller stop > /dev/null 2>&1 || true

exec "$@"
