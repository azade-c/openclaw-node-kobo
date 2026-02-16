#!/bin/sh
set -e

WIFI_IF="eth0"
if [ -e /sys/class/net/wlan0 ]; then
	WIFI_IF="wlan0"
fi

if ! lsmod | grep -q "sdio_wifi_pwr"; then
	for mod in /drivers/ntx/wifi/sdio_wifi_pwr.ko /lib/modules/*/kernel/drivers/net/wireless/sdio_wifi_pwr.ko; do
		if [ -f "$mod" ]; then
			insmod "$mod" || true
			break
		fi
	done
fi

if ! lsmod | grep -q "dhd"; then
	for mod in /drivers/ntx/wifi/dhd.ko /lib/modules/*/kernel/drivers/net/wireless/dhd.ko; do
		if [ -f "$mod" ]; then
			insmod "$mod" || true
			break
		fi
	done
fi

ifconfig "$WIFI_IF" up || true

if ! pgrep wpa_supplicant >/dev/null 2>&1; then
	wpa_supplicant -B -i "$WIFI_IF" -c /etc/wpa_supplicant/wpa_supplicant.conf
fi

if ! pgrep udhcpc >/dev/null 2>&1; then
	udhcpc -i "$WIFI_IF" -n || true
fi
