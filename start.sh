#!/bin/sh
set -e

BASE_DIR="/mnt/onboard/.adds/openclaw"
cd "$BASE_DIR"

mkdir -p logs

killall -STOP nickel || true

dd if=/dev/fb0 of=.nickel_screen.raw 2>/dev/null || true

./enable-wifi.sh

./openclaw-node-kobo 2>> logs/crash.log

./disable-wifi.sh

cat .nickel_screen.raw > /dev/fb0 2>/dev/null || true
rm -f .nickel_screen.raw

killall -CONT nickel || true
