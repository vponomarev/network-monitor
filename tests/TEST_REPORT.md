# Conntrack Testing Report

## Summary

Testing of conntrack with tracepoint fallback mode across multiple kernel versions.

## Test Results

### Debian 13 (6.12.85) - 192.168.5.214
**Status:** ⚠️ Partial - eBPF attaches but events not received

| Component | Status | Notes |
|-----------|--------|-------|
| eBPF Build | ✅ | Compiles with `USE_SOCK_SET_STATE_FALLBACK` |
| CO-RE | ✅ | Fixed `__u8[4]` for saddr/daddr per kernel BTF |
| Program Load | ✅ | All 4 programs load successfully |
| Attach | ✅ | tracepoint + kretprobe + kprobe attach |
| Events | ❌ | No events received in ringbuf |
| run_count | 0 | Programs not triggering |

**Issues:**
- tracepoint/sock/inet_sock_set_state attaches but doesn't fire
- kprobe/tcp_connect also doesn't fire
- Tested with localhost and external connections
- bpf_printk not appearing in trace_pipe

### Proxmox 8.4 (6.8.12-20-pve) - 192.168.5.99
**Status:** ⏹️ Not tested - network restrictions

| Component | Status | Notes |
|-----------|--------|-------|
| Go Version | 1.19.8 | Too old for cilium/ebpf v0.15.0 |
| Network | ❌ | Cannot download dependencies |

### Debian 12 (6.1.0-45) - 192.168.5.193
**Status:** ⏹️ Not tested - network restrictions

### Ubuntu 22.04 (5.15.0-177) - 192.168.5.217
**Status:** ⏹️ Not tested - Go 1.18 too old

## Changes in v1.7.0

### Makefile
- Enabled `USE_SOCK_SET_STATE_FALLBACK` by default
- tracepoint is now the PRIMARY method for outgoing connections

### eBPF Code
- Fixed CO-RE for tracepoint: `__u8[4]` for saddr/daddr (matches kernel BTF)
- Added bpf_printk debug messages
- Updated vmlinux.h for Debian 13

### Go Code
- Changed attach logic: tracepoint FIRST, kprobe as fallback only
- Removed "dual mode" (attaching both simultaneously)

## Known Issues

1. **Debian 13 (6.12.85)**: tracepoint doesn't fire
   - Possible causes:
     - Kernel 6.12 changed tracepoint behavior
     - tracepoint requires BTF info that's missing
     - Localhost connections don't trigger tracepoint
   
2. **Go Version Compatibility**:
   - cilium/ebpf v0.15.0 requires Go 1.21+
   - Older distributions have Go 1.18-1.19
   - Need to either:
     - Use older cilium/ebpf (v0.12.x)
     - Install newer Go manually

## Recommendations

1. **For Debian 13**: Need to investigate why tracepoint doesn't fire
   - Check if tracepoint is enabled: `cat /sys/kernel/debug/tracing/events/sock/inet_sock_set_state/enable`
   - Try with non-localhost connections
   - Check kernel config for BPF_LSM, CONFIG_BPF_EVENTS

2. **For older kernels**: Test on Ubuntu 22.04 (5.15) where kprobe should work
   - May need to install Go 1.21+ manually

3. **For production**: Consider using kprobe as primary on kernels < 6.8
   - tracepoint is more stable on newer kernels
   - kprobe works better on older kernels

## Next Steps

1. Test on Ubuntu 22.04 with manually installed Go 1.21+
2. Investigate tracepoint behavior on Debian 13
3. Consider kernel-specific attach strategy:
   - Kernel >= 6.8: tracepoint primary
   - Kernel < 6.8: kprobe primary
