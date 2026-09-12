#!/bin/sh
set -eu

envsubst '${XRAY_UUID}' \
  < /etc/xray/config.json.template \
  > /etc/xray/config.json

exec /usr/local/bin/xray run -c /etc/xray/config.json
