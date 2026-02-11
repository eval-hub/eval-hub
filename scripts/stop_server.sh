#!/bin/bash

pid=$(cat "$1")

# this is to tidy up after the tests
rm -f /tmp/db_password

# SIGTERM should trigger the clean shutdown (this mirrors what Kubernetes does)
echo "kill -15 ${pid}"
kill -15 "${pid}"
# this is here to allow the service to exit
sleep 5
