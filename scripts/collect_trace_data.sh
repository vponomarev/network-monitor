#!/usr/bin/env bash
#
# Script to collect real trace_pipe data from a Linux server
# Saves output to testdata/trace_pipe_sample.txt
#

set -euo pipefail

# Configuration
SSH_KEY="$HOME/.ssh/id_rsa_svc_s3aas_ci"
SSH_USER="svc_s3aas_ci"
SSH_HOST="ix-m3-sm9-s3-dwh05-0201.srv.hwaas.tcsbank.ru"
OUTPUT_DIR="testdata"
OUTPUT_FILE="$OUTPUT_DIR/trace_pipe_sample.txt"
DURATION=30  # seconds to capture

# Create output directory
mkdir -p "$OUTPUT_DIR"

echo "🔍 Collecting trace_pipe data from $SSH_HOST..."
echo "⏱️  Duration: ${DURATION}s"
echo "📁 Output: $OUTPUT_FILE"
echo ""

# SSH command with timeout
ssh -o StrictHostKeyChecking=no \
    -o ConnectTimeout=10 \
    -i "$SSH_KEY" \
    "${SSH_USER}@${SSH_HOST}" \
    "sudo timeout ${DURATION} cat /sys/kernel/tracing/trace_pipe" \
    > "$OUTPUT_FILE" 2>&1 || true

# Check results
if [ -f "$OUTPUT_FILE" ]; then
    LINES=$(wc -l < "$OUTPUT_FILE")
    SIZE=$(du -h "$OUTPUT_FILE" | cut -f1)
    echo ""
    echo "✅ Collection complete!"
    echo "   Lines: $LINES"
    echo "   Size: $SIZE"
    echo ""
    
    # Show first few lines as preview
    echo "📋 Preview (first 10 lines):"
    echo "─────────────────────────────────────────"
    head -10 "$OUTPUT_FILE" || true
    echo "─────────────────────────────────────────"
    echo ""
    
    # Count tcp_retransmit_skb events
    RETRANSMITS=$(grep -c "tcp_retransmit_skb" "$OUTPUT_FILE" || echo "0")
    echo "📊 TCP retransmit events found: $RETRANSMITS"
else
    echo "❌ Failed to collect data"
    exit 1
fi
