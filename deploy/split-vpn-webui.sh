#!/bin/sh
/usr/bin/systemctl enable split-vpn-webui.service >/dev/null 2>&1
/usr/bin/systemctl restart split-vpn-webui.service >/dev/null 2>&1
