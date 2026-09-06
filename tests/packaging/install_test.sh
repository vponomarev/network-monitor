#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
source packaging/install.sh
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
INSTALL_DIR="$work/bin"
CONFIG_DIR="$work/config"
mkdir -p "$INSTALL_DIR" "$CONFIG_DIR"
printf '#!/bin/sh\necho previous\n' > "$INSTALL_DIR/netmon"
chmod +x "$INSTALL_DIR/netmon"
previous=$(sha256sum "$INSTALL_DIR/netmon")
printf '#!/bin/sh\necho replacement\n' > "$work/asset"
asset_hash=$(sha256sum "$work/asset" | cut -d ' ' -f1)
mode=failed
fake_download() {
    [[ "$mode" != failed ]] || { printf partial; return 1; }
    if [[ "$1" == */checksums.txt ]]; then
        if [[ "$mode" == corrupt ]]; then printf '%064d  netmon-linux-amd64\n' 0
        else printf '%s  netmon-linux-amd64\n' "$asset_hash"; fi
    else cat "$work/asset"; fi
}
DOWNLOAD_CMD=fake_download
for mode in failed corrupt; do
    if install_binary v2.3.0; then echo "accepted $mode download"; exit 1; fi
    [[ "$(sha256sum "$INSTALL_DIR/netmon")" == "$previous" ]]
done
mode=success
install_binary v2.3.0
[[ "$("$INSTALL_DIR/netmon")" == replacement ]]
[[ "$("$INSTALL_DIR/netmon.previous")" == previous ]]
printf 'user configuration\n' > "$CONFIG_DIR/config.yaml"
install_config
[[ "$(cat "$CONFIG_DIR/config.yaml")" == 'user configuration' ]]
[[ -s "$CONFIG_DIR/roles.yaml" && -s "$CONFIG_DIR/locations.yaml" ]]
echo INSTALLER_FAILURE_PRESERVATION_PASS
