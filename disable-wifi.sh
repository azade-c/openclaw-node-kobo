#!/bin/sh
set -e

WIFI_IF="eth0"
if [ -e /sys/class/net/wlan0 ]; then
	WIFI_IF="wlan0"
fi

killall wpa_supplicant 2>/dev/null || true
killall udhcpc 2>/dev/null || true

ifconfig "$WIFI_IF" down || true

rmmod dhd 2>/dev/null || true
rmmod sdio_wifi_pwr 2>/dev/null || true
