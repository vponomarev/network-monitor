// SPDX-License-Identifier: GPL-2.0 OR BSD-3-Clause
// Copyright (c) 2024 Network Monitor Contributors

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>
#include "conntrack.bpf.h"

/*
 * eBPF Connection Tracker - kprobe based
 *
 * Uses kprobe/kretprobe for maximum kernel compatibility:
 * - kprobe/tcp_connect: исходящие соединения (SYN_SENT)
 * - kretprobe/inet_csk_accept: входящие соединения (ESTABLISHED)
 * - kprobe/tcp_close: закрытие соединений
 *
 * Supported kernels: 4.19.x+ (Debian 10/11, Ubuntu 18.04+, all modern)
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
 * For tcp_connect: uses inet_saddr/daddr (set before connect)
 * For inet_csk_accept/tcp_close: uses skc_rcv_saddr/daddr
 */
static __always_inline void extract_ipv4_addrs(struct sock *sk, __u8 *saddr, __u8 *daddr)
{
    __u32 saddr4, daddr4;

    // Try skc_rcv_saddr/skc_daddr first (works for established sockets)
    saddr4 = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
    daddr4 = BPF_CORE_READ(sk, __sk_common.skc_daddr);

    // For tcp_connect, skc_rcv_saddr may be 0 (not bound yet)
    // Fall back to inet_saddr from inet_sock
    if (saddr4 == 0) {
        struct inet_sock *inet = (void *)sk;
        saddr4 = BPF_CORE_READ(inet, inet_saddr);
    }

    // Kernel stores these in NETWORK byte order (big-endian)
    // Convert to host byte order
    saddr4 = bpf_ntohl(saddr4);
    daddr4 = bpf_ntohl(daddr4);

    // Convert to IPv4-mapped IPv6 format
    __builtin_memset(saddr, 0, 16);
    __builtin_memset(daddr, 0, 16);
    saddr[10] = 0xff;
    saddr[11] = 0xff;
    daddr[10] = 0xff;
    daddr[11] = 0xff;

    // Extract bytes from host-order __u32
    saddr[12] = (__u8)((saddr4 >> 24) & 0xFF);
    saddr[13] = (__u8)((saddr4 >> 16) & 0xFF);
    saddr[14] = (__u8)((saddr4 >> 8) & 0xFF);
    saddr[15] = (__u8)(saddr4 & 0xFF);

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

/* Trace tcp_connect - outgoing connection initiation (SYN sent)
 * Note: No family check here - skc_family may not be set yet for outgoing.
 * IPv4/IPv6 filtering is done in extract_ipv4_addrs (returns 0.0.0.0 for IPv6).
 */
SEC("kprobe/tcp_connect")
int BPF_KPROBE(tcp_connect, struct sock *sk)
{
    if (!track_outgoing)
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

    // Get process name once
    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));

    // Create key and extract connection info
    make_key_from_sock(sk, &key);
    __builtin_memcpy(evt.src_ip, key.src_ip, 16);
    __builtin_memcpy(evt.dst_ip, key.dst_ip, 16);
    evt.src_port = key.src_port;
    evt.dst_port = key.dst_port;
    evt.protocol = key.protocol;

    // Store in connections map
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

/* Trace inet_csk_accept - server accepts incoming connection
 * Returns: struct sock * (new connection socket)
 *
 * Key consistency: key is created from RAW socket values (local=src, remote=dst)
 * Event format: src=client (remote), dst=server (local) - swapped for user-facing format
 *
 * Note: tcp_flags are conditional - connection is already ESTABLISHED at accept() time
 */
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
