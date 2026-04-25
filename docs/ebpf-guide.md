# eBPF Development Guide

This guide covers developing and debugging eBPF programs for Network Monitor.

## Prerequisites

- Linux kernel 5.8+ (5.15+ recommended)
- Clang 12+
- LLVM 12+
- libbpf
- bpftool

## Building eBPF Programs

### Build Command

```bash
make build-ebpf
```

This compiles `bpf/conntrack.bpf.c` to `internal/conntrack/conntrack.bpf.o`.

### Manual Build

```bash
cd bpf
clang -g -O2 -Wall -target bpf \
    -D__TARGET_ARCH_x86 \
    -I/usr/include \
    -c conntrack.bpf.c -o conntrack.bpf.o
```

### Architecture-specific

```bash
# x86_64
clang -D__TARGET_ARCH_x86 ...

# ARM64
clang -D__TARGET_ARCH_arm64 ...
```

## eBPF Program Types

### kprobe

Attaches to kernel function entry:

```c
SEC("kprobe/tcp_connect")
int BPF_KPROBE(tcp_connect, struct sock *sk) {
    // Called when tcp_connect() is entered
    return 0;
}
```

### kretprobe

Attaches to kernel function return:

```c
SEC("kretprobe/tcp_connect")
int BPF_KRETPROBE(tcp_connect_ret, int ret) {
    // Called when tcp_connect() returns
    return 0;
}
```

### Tracepoint

Attaches to kernel tracepoints:

```c
SEC("tracepoint/syscalls/sys_enter_connect")
int trace_connect(struct trace_event_raw_sys_enter *ctx) {
    // Called on connect() syscall
    return 0;
}
```

## Data Structures

### Ring Buffer

For sending events to userspace:

```c
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} events SEC(".maps");
```

Submit event:

```c
struct event *e;
e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
if (e) {
    // Fill event data
    bpf_ringbuf_submit(e, 0);
}
```

### Hash Map

For tracking state:

```c
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u64);
    __type(value, struct connection);
} connections SEC(".maps");
```

## Reading Data

### Process Information

```c
char comm[TASK_COMM_LEN];
bpf_get_current_comm(&comm, sizeof(comm));

__u32 pid = bpf_get_current_pid_tgid() >> 32;
__u32 tid = bpf_get_current_pid_tgid();
```

### Socket Information

```c
// Read socket family
__u16 family;
bpf_probe_read_kernel(&family, sizeof(family), &sk->__sk_common.skc_family);

// Read ports
__u16 sport, dport;
bpf_probe_read_kernel(&sport, sizeof(sport), &inet->inet_sport);
bpf_probe_read_kernel(&dport, sizeof(dport), &inet->inet_dport);

// Read IP addresses
__u32 saddr, daddr;
bpf_probe_read_kernel(&saddr, sizeof(saddr), &inet->inet_saddr);
bpf_probe_read_kernel(&daddr, sizeof(daddr), &inet->inet_daddr);
```

## Debugging

### Verifier Output

```bash
# Load with verbose output
bpftool prog load conntrack.bpf.o /sys/fs/bpf/conntrack type kprobe
```

### Check Loaded Programs

```bash
bpftool prog list
bpftool prog show id <ID>
```

### Trace eBPF Events

```bash
# Using trace_pipe
cat /sys/kernel/tracing/trace_pipe

# Using bpftool
bpftool prog tracelog
```

### BPF Printk

```c
bpf_printk("Connection from %pI4:%d\n", &saddr, sport);
```

View output:
```bash
cat /sys/kernel/tracing/trace_pipe
```

## CO-RE (Compile Once - Run Everywhere)

CO-RE allows eBPF programs to work across kernel versions.

### Requirements

- Kernel BTF information
- clang 11+
- libbpf 0.3+

### Enable CO-RE

```c
#include <bpf/bpf_core_read.h>

// Use BPF_CORE_READ for safe field access
__u16 family = BPF_CORE_READ(sk, __sk_common.skc_family);
```

### Generate BTF

```bash
# Extract BTF from running kernel
bpftool btf dump file /sys/kernel/btf/vmlinux format c > vmlinux.h
```

## Common Issues

### "Invalid memory access"

Ensure you're using `bpf_probe_read_kernel()` for kernel memory:

```c
// Wrong
__u16 port = sk->sk_port;

// Correct
__u16 port;
bpf_probe_read_kernel(&port, sizeof(port), &sk->sk_port);
```

### "Loop detected"

eBPF doesn't allow unbounded loops. Use `#pragma unroll`:

```c
#pragma unroll
for (int i = 0; i < 4; i++) {
    // Loop body
}
```

### "Map lookup failed"

Check if key exists before using value:

```c
struct connection *conn;
conn = bpf_map_lookup_elem(&connections, &key);
if (!conn)
    return 0;
```

## Testing

### Unit Testing eBPF

Use bpf_linker and Go tests:

```go
func TestEBPFProgram(t *testing.T) {
    spec, err := ebpf.LoadCollectionSpec("conntrack.bpf.o")
    require.NoError(t, err)
    
    coll, err := ebpf.NewCollection(spec)
    require.NoError(t, err)
    defer coll.Close()
}
```

### Integration Testing

```bash
# Start monitor
sudo ./bin/conntrack &

# Generate traffic
curl https://example.com

# Check metrics
curl http://localhost:9090/metrics | grep conntrack
```

## Resources

- [eBPF Documentation](https://ebpf.io/)
- [libbpf Documentation](https://libbpf.readthedocs.io/)
- [Cilium eBPF Guide](https://ebpf.io/what-is-ebpf/)
- [BPF Compiler Collection](https://github.com/llvm/llvm-project)
