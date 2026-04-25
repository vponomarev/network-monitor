# TCP Retransmit Trace Data

This directory contains sample trace_pipe data for testing.

## Files

- `trace_pipe_sample.txt` - Real kernel trace output from production server

## Collection

To collect new data:

```bash
# Using the script (recommended)
./scripts/collect_trace_data.sh

# Or manually (30 seconds capture)
ssh -o StrictHostKeyChecking=no -i ~/.ssh/id_rsa_svc_s3aas_ci \
    svc_s3aas_ci@ix-m3-sm9-s3-dwh05-0201.srv.hwaas.tcsbank.ru \
    "sudo timeout 30 cat /sys/kernel/tracing/trace_pipe" \
    > testdata/trace_pipe_sample.txt
```

## Format

Example trace_pipe output:
```
          <...>-12345 [001] d.H. 12345.678901: tcp_retransmit_skb: addr=0xffff888012345678 sk=0xffff888012345678 saddr=192.168.1.10 daddr=192.168.1.20 seq=123456789
          <...>-12346 [002] d.H. 12346.789012: tcp_connect: saddr=192.168.1.10 daddr=192.168.1.20
```

## Usage in Tests

```go
// Use in tests
collector := NewTracePipeCollector("testdata/trace_pipe_sample.txt", exporter, logger)
```
