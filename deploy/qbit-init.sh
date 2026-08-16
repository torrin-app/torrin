#!/bin/sh
set -e
CONF=/config/qBittorrent/qBittorrent.conf
[ -f "$CONF" ] && grep -q "AuthSubnetWhitelistEnabled=true" "$CONF" && exit 0

WHITELIST="${QBIT_WEBUI_WHITELIST:-172.16.0.0/12}"
PORT="${QBIT_TORRENT_PORT:-6881}"
mkdir -p "$(dirname "$CONF")"
cat > "$CONF" <<EOF
[BitTorrent]
Session\Port=$PORT
Session\GlobalMaxRatio=0
Session\MaxRatioAction=0
Session\QueueingSystemEnabled=true
Session\MaxActiveDownloads=5
Session\MaxActiveUploads=3
Session\MaxActiveTorrents=10
Session\IgnoreSlowTorrentsForQueueing=true
Session\MaxConnections=500
Session\MaxConnectionsPerTorrent=100
Session\MaxUploads=8
Session\MaxUploadsPerTorrent=4
Session\DHTEnabled=true
Session\PeXEnabled=true
Session\LSDEnabled=true
Session\Preallocation=false
Session\DefaultSavePath=/downloads

[LegalNotice]
Accepted=true

[Meta]
MigrationVersion=8

[Preferences]
WebUI\Port=8080
WebUI\Address=*
WebUI\HostHeaderValidation=false
WebUI\CSRFProtection=false
WebUI\AuthSubnetWhitelistEnabled=true
WebUI\AuthSubnetWhitelist=$WHITELIST
EOF
