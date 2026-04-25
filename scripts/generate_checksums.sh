#!/usr/bin/env bash
#
# Generate checksums for release binaries
#

set -euo pipefail

OUTPUT_FILE="${1:-checksums.txt}"
ARTIFACTS_DIR="${2:-./artifacts}"

echo "Generating checksums for release binaries..."

cd "$ARTIFACTS_DIR"

# Generate SHA256 checksums
sha256sum netmon-* 2>/dev/null | grep -v '.sha256' > "$OUTPUT_FILE" || true

# Display checksums
echo ""
echo "Generated checksums:"
cat "$OUTPUT_FILE"
echo ""
echo "Checksums saved to: $OUTPUT_FILE"
