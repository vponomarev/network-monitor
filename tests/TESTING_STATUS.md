# Conntrack Testing Status

## Test Hosts

| Host | IP | Kernel | Status |
|------|-----|--------|--------|
| Ubuntu 22.04 | 192.168.5.217 | 5.15.0-177 | ⏹️ Not tested |
| Debian 13 | 192.168.5.214 | 6.12.85 | ❌ kprobe not working |
| Debian 12 | 192.168.5.193 | 6.1.0-45 | ⏹️ Not tested |
| Proxmox 8.4 | 192.168.5.99 | 6.8.12-20-pve | ⏹️ Not tested |

## Issue: kprobe not working on Debian 13 (6.12.85)

### Symptoms
- eBPF programs attach successfully (link.Kprobe returns no error)
- Programs NOT visible in `bpftool prog list`
- No events received in ringbuf reader
- kallsyms shows functions exist: `tcp_connect`, `inet_csk_accept`, `tcp_close`

### Root Cause
kprobe attachment fails silently on kernel 6.12.85. The cilium/ebpf library's `link.Kprobe()` creates an ephemeral link that doesn't register with the kernel's kprobe subsystem.

### Evidence
```bash
# Functions exist in kernel
grep tcp_connect /proc/kallsyms
# ffffffff9f31b090 T tcp_connect

# But kprobes list is empty
grep tcp_connect /sys/kernel/debug/kprobes/list
# (empty)

# bpftool doesn't show our programs
bpftool prog list | grep tcp
# (no conntrack programs)
```

### Solution
Use tracepoint/sock/inet_sock_set_state instead of kprobe/tcp_connect.

The fallback code exists but is only compiled with `USE_SOCK_SET_STATE_FALLBACK` define. Need to:
1. Enable tracepoint by default for all builds
2. Or detect kprobe failure and fallback at runtime

### Next Steps
1. Rebuild eBPF with `make fallback` to enable tracepoint
2. Test on Debian 13
3. If working, update default build to use tracepoint
4. Test on other kernels (Ubuntu 22.04, Debian 12, Proxmox)

## Test Commands

```bash
# Build with tracepoint fallback
cd bpf
make fallback

# Rebuild conntrack
cd ..
mkdir -p pkg/embedded/bpf
cp bpf/conntrack.bpf.o pkg/embedded/bpf/
go build -o conntrack ./cmd/conntrack

# Test
./conntrack --config /dev/null &
nc -l -p 19999 &
echo test | nc -w 1 127.0.0.1 19999
# Check logs for events
```
