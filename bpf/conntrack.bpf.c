// SPDX-License-Identifier: GPL-2.0 OR BSD-3-Clause
// Copyright (c) 2024 Network Monitor Contributors

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>
#include "conntrack.bpf.h"

/* TCP state constant - from Linux UAPI */
#ifndef TCP_SYN_SENT
#define TCP_SYN_SENT 2
#endif

/*
 * eBPF Connection Tracker - tracepoint based for outgoing connections
 *
 * Uses:
 * - tracepoint/sock/inet_sock_set_state: outgoing connections (SYN_SENT)
 * - kretprobe/inet_csk_accept: incoming connections (ESTABLISHED)
 * - kprobe/tcp_close: connection closes
 *
 * Supported kernels: 5.14+ (tracepoint/sock/inet_sock_set_state stable)
 *
 * Limitations:
 * - Only IPv4 TCP connections are tracked (AF_INET + IPPROTO_TCP)
 * - IPv6 support requires additional implementation
 */

struct connection_event {
    __u64 timestamp_ns;
    __u64 pid_tgid;
    __u32 pid;
    __u32 tid;
    __u8 src_ip[16];
    __u8 dst_ip[16];
    __u16 src_port;
    __u16 dst_port;
    __u8 protocol;
    __u8 direction;
    __u8 state;
    __u8 event_type;
    __u8 tcp_flags;
    __u8 _pad[7];              /* Explicit padding for 8-byte alignment */
    char comm[TASK_COMM_LEN];  /* Aligned at offset 72 */
};

/* Connection key - packed to avoid padding issues */
struct connection_key {
    __u8 src_ip[16];
    __u8 dst_ip[16];
    __u16 src_port;
    __u16 dst_port;
    __u8 protocol;
} __attribute__((packed));

struct connection_entry {
    __u64 timestamp_ns;   /* offset 0, size 8 */
    __u32 pid;            /* offset 8, size 4 */
    __u8 direction;       /* offset 12, size 1 */
    __u8 state;           /* offset 13, size 1 */
    __u8 tcp_flags;       /* offset 14, size 1 */
    __u8 _pad;            /* offset 15, size 1 (align comm to 4-byte boundary) */
    char comm[TASK_COMM_LEN];  /* offset 16, size 16 */
};                        /* total: 32 bytes (naturally aligned to 8) */

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_CONNECTIONS);
    __type(key, struct connection_key);
    __type(value, struct connection_entry);
} connections SEC(".maps");

volatile const bool track_incoming = true;
volatile const bool track_outgoing = true;
volatile const bool track_closes = true;

static __always_inline void submit_event(struct connection_event *evt)
{
    struct connection_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return;
    *event = *evt;
    bpf_ringbuf_submit(event, 0);
}

/* Extract IPv4 addresses from sock using BPF_CORE_READ
 * Uses skc_rcv_saddr/skc_daddr for all cases
 * Note: For outgoing connections before bind(), src_ip will be 0.0.0.0
 * This is expected behavior - userspace should handle this case
 *
 * IMPORTANT: skc_rcv_saddr/skc_daddr are in NETWORK byte order (big-endian).
 * We copy bytes directly to IPv4-mapped format without byte swap.
 */
static __always_inline void extract_ipv4_addrs(struct sock *sk, __u8 *saddr, __u8 *daddr)
{
    __u32 saddr4, daddr4;

    // Use skc_rcv_saddr/skc_daddr for all cases
    saddr4 = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
    daddr4 = BPF_CORE_READ(sk, __sk_common.skc_daddr);

    // Convert to IPv4-mapped IPv6 format
    // Network byte order: byte[0] is MSB, byte[3] is LSB
    __builtin_memset(saddr, 0, 16);
    __builtin_memset(daddr, 0, 16);
    saddr[10] = 0xff;
    saddr[11] = 0xff;
    daddr[10] = 0xff;
    daddr[11] = 0xff;

    // Copy bytes directly from network-order __u32
    // saddr4 = [byte0][byte1][byte2][byte3] in big-endian
    saddr[12] = (__u8)((saddr4 >> 24) & 0xFF);  // byte0 (MSB)
    saddr[13] = (__u8)((saddr4 >> 16) & 0xFF);  // byte1
    saddr[14] = (__u8)((saddr4 >> 8) & 0xFF);   // byte2
    saddr[15] = (__u8)(saddr4 & 0xFF);          // byte3 (LSB)

    daddr[12] = (__u8)((daddr4 >> 24) & 0xFF);
    daddr[13] = (__u8)((daddr4 >> 16) & 0xFF);
    daddr[14] = (__u8)((daddr4 >> 8) & 0xFF);
    daddr[15] = (__u8)(daddr4 & 0xFF);
}

/* Extract ports from sock */
static __always_inline void extract_ports(struct sock *sk, __u16 *sport, __u16 *dport)
{
    *sport = BPF_CORE_READ(sk, __sk_common.skc_num);
    *dport = BPF_CORE_READ(sk, __sk_common.skc_dport);
    *dport = bpf_ntohs(*dport);
}

/* Create connection key from sock - RAW socket values (no swap) */
static __always_inline void make_key_from_sock(struct sock *sk, struct connection_key *key)
{
    extract_ipv4_addrs(sk, key->src_ip, key->dst_ip);
    extract_ports(sk, &key->src_port, &key->dst_port);
    key->protocol = IPPROTO_TCP;
}

/* -------------------------------------------------------------------------
 * kprobe/tcp_connect — outgoing connections (fallback for kernels < 5.14).
 *
 * This is used when tracepoint/sock/inet_sock_set_state is not available.
 * Note: Some kernels (e.g., Ubuntu 22.04 with 5.15) may block kprobe/tcp_connect
 * for userspace processes due to security restrictions.
 * ---------------------------------------------------------------------- */
SEC("kprobe/tcp_connect")
int BPF_KPROBE(tcp_connect, struct sock *sk)
{
    if (!track_outgoing)
        return 0;

    // Check socket family - only IPv4 supported
    __u16 family = BPF_CORE_READ(sk, __sk_common.skc_family);
    if (family != AF_INET)
        return 0;

    struct connection_event evt = {};
    struct connection_key key = {};

    evt.timestamp_ns = bpf_ktime_get_ns();
    evt.pid_tgid = bpf_get_current_pid_tgid();
    evt.pid = evt.pid_tgid >> 32;
    evt.tid = evt.pid_tgid & 0xFFFFFFFF;
    evt.direction = DIR_OUTGOING;
    evt.state = CONN_STATE_SYN_SENT;
    evt.event_type = CONN_EVENT_NEW;
    evt.tcp_flags = TCP_SYN;

    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));

    make_key_from_sock(sk, &key);

    /* Filter out invalid connections (qemu-ga, etc.) */
    __u32 src_ip4 = ((__u32)key.src_ip[12] << 24) | ((__u32)key.src_ip[13] << 16) |
                    ((__u32)key.src_ip[14] << 8) | (__u32)key.src_ip[15];
    __u32 dst_ip4 = ((__u32)key.dst_ip[12] << 24) | ((__u32)key.dst_ip[13] << 16) |
                    ((__u32)key.dst_ip[14] << 8) | (__u32)key.dst_ip[15];

    if (src_ip4 == 0 && dst_ip4 == 0) {
        return 0;
    }

    __builtin_memcpy(evt.src_ip, key.src_ip, 16);
    __builtin_memcpy(evt.dst_ip, key.dst_ip, 16);
    evt.src_port = key.src_port;
    evt.dst_port = key.dst_port;
    evt.protocol = key.protocol;

    struct connection_entry entry = {};
    entry.timestamp_ns = evt.timestamp_ns;
    entry.pid = evt.pid;
    entry.direction = DIR_OUTGOING;
    entry.state = CONN_STATE_SYN_SENT;
    entry.tcp_flags = TCP_SYN;
    __builtin_memcpy(entry.comm, evt.comm, TASK_COMM_LEN);

    bpf_map_update_elem(&connections, &key, &entry, BPF_ANY);
    submit_event(&evt);
    return 0;
}

/* -------------------------------------------------------------------------
 * tracepoint/sock/inet_sock_set_state — outgoing connections (PRIMARY for 5.14+).
 *
 * Catches the TCP_SYN_SENT state transition which is equivalent to the
 * moment tcp_connect() is called. PID context is correct here because
 * the transition happens synchronously in the calling process.
 * ---------------------------------------------------------------------- */
SEC("tracepoint/sock/inet_sock_set_state")
int trace_outgoing(struct trace_event_raw_inet_sock_set_state *ctx)
{
    if (!track_outgoing)
        return 0;

    /* Only IPv4 TCP transitioning to SYN_SENT */
    if (BPF_CORE_READ(ctx, protocol) != IPPROTO_TCP)
        return 0;
    if (BPF_CORE_READ(ctx, newstate) != TCP_SYN_SENT)
        return 0;
    if (BPF_CORE_READ(ctx, family) != AF_INET)
        return 0;

    bpf_printk("conntrack: tracepoint fired, protocol=%d, newstate=%d",
               BPF_CORE_READ(ctx, protocol), BPF_CORE_READ(ctx, newstate));

    struct connection_event evt = {};
    evt.timestamp_ns = bpf_ktime_get_ns();
    evt.pid_tgid = bpf_get_current_pid_tgid();
    evt.pid = (__u32)(evt.pid_tgid >> 32);
    evt.tid = (__u32)(evt.pid_tgid & 0xFFFFFFFF);
    evt.direction = DIR_OUTGOING;
    evt.state = CONN_STATE_SYN_SENT;
    evt.event_type = CONN_EVENT_NEW;
    evt.tcp_flags = TCP_SYN;
    evt.protocol = IPPROTO_TCP;

    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));

    /* ctx->saddr / ctx->daddr are __u8[4] in network byte order (big-endian)
     * Reconstruct __u32 from bytes: byte[0] is MSB, byte[3] is LSB
     * Use bpf_core_read() to correctly access array elements
     * NO byte swap needed - we copy bytes directly to IPv4-mapped format
     */
    __u8 saddr_bytes[4], daddr_bytes[4];
    if (bpf_core_read(&saddr_bytes, sizeof(saddr_bytes), &ctx->saddr) != 0)
        return 0;
    if (bpf_core_read(&daddr_bytes, sizeof(daddr_bytes), &ctx->daddr) != 0)
        return 0;

    bpf_printk("conntrack: tracepoint fired, sport=%d, dport=%d",
               (__u32)ctx->sport, (__u32)ctx->dport);

    __builtin_memset(evt.src_ip, 0, 16);
    __builtin_memset(evt.dst_ip, 0, 16);
    evt.src_ip[10] = 0xff; evt.src_ip[11] = 0xff;
    evt.dst_ip[10] = 0xff; evt.dst_ip[11] = 0xff;

    /* Copy bytes directly - network order preserved */
    evt.src_ip[12] = saddr_bytes[0];
    evt.src_ip[13] = saddr_bytes[1];
    evt.src_ip[14] = saddr_bytes[2];
    evt.src_ip[15] = saddr_bytes[3];

    evt.dst_ip[12] = daddr_bytes[0];
    evt.dst_ip[13] = daddr_bytes[1];
    evt.dst_ip[14] = daddr_bytes[2];
    evt.dst_ip[15] = daddr_bytes[3];

    evt.src_port = ctx->sport;
    evt.dst_port = ctx->dport;

    /* Filter out invalid connections (qemu-ga, etc.) */
    __u32 saddr4 = ((__u32)saddr_bytes[0] << 24) | ((__u32)saddr_bytes[1] << 16) |
                   ((__u32)saddr_bytes[2] << 8) | (__u32)saddr_bytes[3];
    __u32 daddr4 = ((__u32)daddr_bytes[0] << 24) | ((__u32)daddr_bytes[1] << 16) |
                   ((__u32)daddr_bytes[2] << 8) | (__u32)daddr_bytes[3];

    if (saddr4 == 0 && daddr4 == 0) {
        bpf_printk("conntrack: filtering out (both IPs zero)");
        return 0;
    }

    struct connection_key key = {};
    __builtin_memcpy(key.src_ip, evt.src_ip, 16);
    __builtin_memcpy(key.dst_ip, evt.dst_ip, 16);
    key.src_port = evt.src_port;
    key.dst_port = evt.dst_port;
    key.protocol = IPPROTO_TCP;

    struct connection_entry entry = {};
    entry.timestamp_ns = evt.timestamp_ns;
    entry.pid = evt.pid;
    entry.direction = DIR_OUTGOING;
    entry.state = CONN_STATE_SYN_SENT;
    entry.tcp_flags = TCP_SYN;
    __builtin_memcpy(entry.comm, evt.comm, TASK_COMM_LEN);

    bpf_map_update_elem(&connections, &key, &entry, BPF_ANY);
    bpf_printk("conntrack: tracepoint submitting event");
    submit_event(&evt);
    return 0;
}

/* -------------------------------------------------------------------------
 * kretprobe/inet_csk_accept — incoming connections.
 *
 * Fires after the kernel has dequeued a fully established connection from
 * the accept queue. The returned sock is in ESTABLISHED state.
 *
 * Key is stored in socket-native format (local=src, remote=dst).
 * Event is emitted in user-facing format (src=client, dst=server) — ports
 * and IPs are swapped relative to the key.
 *
 * evt.comm semantics: name of the process calling accept() (the server).
 * May occasionally be a kernel thread name if the scheduler context
 * switches between accept() and the kretprobe firing — this is a known
 * limitation. Userspace should fall back to /proc/{pid}/comm if needed.
 *
 * tcp_flags = TCP_SYN|TCP_ACK is symbolic — the handshake is already done.
 * ---------------------------------------------------------------------- */
SEC("kretprobe/inet_csk_accept")
int BPF_KRETPROBE(inet_csk_accept, struct sock *ret_sk)
{
    if (!track_incoming)
        return 0;

    if (!ret_sk)
        return 0;

    // Check socket family - only IPv4 supported
    __u16 family = BPF_CORE_READ(ret_sk, __sk_common.skc_family);
    if (family != AF_INET)
        return 0;

    struct connection_event evt = {};
    struct connection_key key = {};

    evt.timestamp_ns = bpf_ktime_get_ns();
    evt.pid_tgid = bpf_get_current_pid_tgid();
    evt.pid = evt.pid_tgid >> 32;
    evt.tid = evt.pid_tgid & 0xFFFFFFFF;
    evt.direction = DIR_INCOMING;
    evt.state = CONN_STATE_ESTABLISHED;
    evt.event_type = CONN_EVENT_ESTABLISHED;
    evt.tcp_flags = TCP_SYN | TCP_ACK;  // Conditional: connection already ESTABLISHED
    evt.protocol = IPPROTO_TCP;

    // Get process name once
    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));

    // Create key FIRST from raw socket values (local=src, remote=dst)
    make_key_from_sock(ret_sk, &key);

    // Copy to event
    __builtin_memcpy(evt.src_ip, key.src_ip, 16);
    __builtin_memcpy(evt.dst_ip, key.dst_ip, 16);
    evt.src_port = key.src_port;
    evt.dst_port = key.dst_port;

    // Swap for user-facing format: src=client (remote), dst=server (local)
    __u8 tmp_ip[16];
    __builtin_memcpy(tmp_ip, evt.src_ip, 16);
    __builtin_memcpy(evt.src_ip, evt.dst_ip, 16);
    __builtin_memcpy(evt.dst_ip, tmp_ip, 16);

    __u16 tmp_port = evt.src_port;
    evt.src_port = evt.dst_port;
    evt.dst_port = tmp_port;

    // Store in connections map (key is in raw socket format)
    struct connection_entry entry = {};
    entry.timestamp_ns = evt.timestamp_ns;
    entry.pid = evt.pid;
    entry.direction = DIR_INCOMING;
    entry.state = CONN_STATE_ESTABLISHED;
    entry.tcp_flags = TCP_SYN | TCP_ACK;
    __builtin_memcpy(entry.comm, evt.comm, TASK_COMM_LEN);

    bpf_map_update_elem(&connections, &key, &entry, BPF_ANY);

    submit_event(&evt);
    return 0;
}

/* Trace tcp_close - connection closing
 * Use kprobe (not kretprobe) to get socket before it's freed
 * Signature: void tcp_close(struct sock *sk, long timeout)
 *
 * Note: tcp_close() is only called for TCP sockets (net/ipv4/tcp.c),
 * so protocol check is unnecessary. Only family check is needed.
 *
 * evt.comm semantics:
 *   - If connection found in map: comm = process that OPENED the connection
 *   - If not found: comm = process that is CLOSING the connection (fallback)
 */
SEC("kprobe/tcp_close")
int BPF_KPROBE(tcp_close, struct sock *sk)
{
    if (!track_closes)
        return 0;

    // Check socket family - only IPv4 supported
    // Note: tcp_close() is only called for TCP sockets, no protocol check needed
    __u16 family = BPF_CORE_READ(sk, __sk_common.skc_family);
    if (family != AF_INET)
        return 0;

    struct connection_event evt = {};
    struct connection_key key = {};

    evt.timestamp_ns = bpf_ktime_get_ns();
    evt.pid_tgid = bpf_get_current_pid_tgid();
    evt.pid = evt.pid_tgid >> 32;
    evt.tid = evt.pid_tgid & 0xFFFFFFFF;
    evt.state = CONN_STATE_CLOSED;
    evt.event_type = CONN_EVENT_CLOSED;

    // Get process name once
    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));

    // Create key from raw socket values (must match how it was stored)
    make_key_from_sock(sk, &key);

    // Look up stored connection to get original direction
    struct connection_entry *entry;
    entry = bpf_map_lookup_elem(&connections, &key);
    if (entry) {
        evt.direction = entry->direction;
        __builtin_memcpy(evt.comm, entry->comm, TASK_COMM_LEN);  // Process that opened
        __builtin_memcpy(evt.src_ip, key.src_ip, 16);
        __builtin_memcpy(evt.dst_ip, key.dst_ip, 16);
        evt.src_port = key.src_port;
        evt.dst_port = key.dst_port;
        evt.protocol = key.protocol;

        // Remove from tracking map
        bpf_map_delete_elem(&connections, &key);
    } else {
        // Connection not found - extract from socket anyway
        // Cases: A) Tracker started after connection opened (most common)
        //        B) Map overflow - entry was evicted
        //        C) Race condition during concurrent close
        // comm = process that is closing (already set above)
        extract_ipv4_addrs(sk, evt.src_ip, evt.dst_ip);
        extract_ports(sk, &evt.src_port, &evt.dst_port);
        evt.protocol = IPPROTO_TCP;  // tcp_close() only called for TCP
        evt.direction = DIR_UNKNOWN;  // Unknown: connection not tracked from start
    }

    submit_event(&evt);
    return 0;
}

char LICENSE[] SEC("license") = "Dual BSD/GPL";
