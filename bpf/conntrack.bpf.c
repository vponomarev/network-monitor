// SPDX-License-Identifier: GPL-2.0 OR BSD-3-Clause
// Copyright (c) 2024 Network Monitor Contributors

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>
#include "conntrack.bpf.h"

/*
 * eBPF Connection Tracker - kprobe/kretprobe based
 *
 * Uses kprobe/kretprobe for maximum kernel compatibility:
 * - kprobe/tcp_connect: исходящие соединения (SYN_SENT)
 * - kretprobe/tcp_v4_accept: входящие IPv4 (ESTABLISHED)
 * - kretprobe/tcp_v6_accept: входящие IPv6 (ESTABLISHED)
 * - kretprobe/tcp_close: закрытие соединений
 *
 * Supported kernels:
 * - 4.19.x+ (Debian 10/11, Ubuntu 18.04+)
 * - 5.15.x (Ubuntu 22.04 GA)
 * - 6.x+ (All modern kernels)
 */

/* Connection event - sent to userspace via ring buffer */
struct connection_event {
    __u64 timestamp_ns;
    __u64 pid_tgid;       // Combined PID/TGID from bpf_get_current_pid_tgid()
    __u32 pid;            // Thread group ID (process)
    __u32 tid;            // Thread ID
    __u8 src_ip[16];      // IPv4-mapped IPv6 or IPv6
    __u8 dst_ip[16];
    __u16 src_port;
    __u16 dst_port;
    __u8 protocol;
    __u8 direction;       // DIR_INCOMING or DIR_OUTGOING
    __u8 state;           // CONN_STATE_*
    __u8 event_type;      // CONN_EVENT_*
    __u8 tcp_flags;
    char comm[TASK_COMM_LEN];
};

/* Connection key for tracking in hash map */
struct connection_key {
    __u8 src_ip[16];
    __u8 dst_ip[16];
    __u16 src_port;
    __u16 dst_port;
    __u8 protocol;
};

/* Connection tracking entry */
struct connection_entry {
    __u64 timestamp_ns;
    __u32 pid;
    __u8 direction;
    __u8 state;
    __u8 tcp_flags;
    char comm[TASK_COMM_LEN];
};

/* Ring buffer for events */
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} events SEC(".maps");

/* Track active connections: key -> connection_entry */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_CONNECTIONS);
    __type(key, struct connection_key);
    __type(value, struct connection_entry);
} connections SEC(".maps");

/* Configuration - set from userspace */
volatile const bool track_incoming = true;
volatile const bool track_outgoing = true;
volatile const bool track_closes = true;

/* Generate connection key from IPs and ports */
static __always_inline __u64 hash_connection_key(const struct connection_key *key)
{
    __u64 hash = 0;

    #pragma unroll
    for (int i = 0; i < 8; i++) {
        hash ^= ((__u64)key->src_ip[i]) << (i * 8);
        hash ^= ((__u64)key->dst_ip[8 - i - 1]) << (i * 8);
    }
    hash ^= ((__u64)key->src_port) << 32;
    hash ^= ((__u64)key->dst_port) << 48;
    hash ^= ((__u64)key->protocol) << 56;

    return hash;
}

/* Submit event to ring buffer */
static __always_inline void submit_event(struct connection_event *evt)
{
    struct connection_event *event;

    event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return;

    *event = *evt;
    bpf_ringbuf_submit(event, 0);
}

/* Extract IPv4 addresses from sock (host byte order) */
static __always_inline void extract_ipv4_addrs(struct sock *sk, __u8 *saddr, __u8 *daddr)
{
    __u32 saddr4, daddr4;

    // Kernel stores these in host byte order in skc_rcv_saddr/skc_daddr
    bpf_probe_read_kernel(&saddr4, sizeof(saddr4), &sk->__sk_common.skc_rcv_saddr);
    bpf_probe_read_kernel(&daddr4, sizeof(daddr4), &sk->__sk_common.skc_daddr);

    // Convert to IPv4-mapped IPv6 format
    __builtin_memset(saddr, 0, 16);
    __builtin_memset(daddr, 0, 16);
    saddr[10] = 0xff;
    saddr[11] = 0xff;
    daddr[10] = 0xff;
    daddr[11] = 0xff;

    // Extract bytes from host-order __u32 (little-endian)
    // For 192.168.5.214 in host order: 0xC0A805D6
    // We want: [12]=192, [13]=168, [14]=5, [15]=214
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
    // skc_num is in host byte order
    // skc_dport is in network byte order
    bpf_probe_read_kernel(sport, sizeof(*sport), &sk->__sk_common.skc_num);
    bpf_probe_read_kernel(dport, sizeof(*dport), &sk->__sk_common.skc_dport);
    *dport = bpf_ntohs(*dport);
}

/* Create connection key from sock */
static __always_inline void make_key_from_sock(struct sock *sk, struct connection_key *key)
{
    extract_ipv4_addrs(sk, key->src_ip, key->dst_ip);
    extract_ports(sk, &key->src_port, &key->dst_port);
    key->protocol = IPPROTO_TCP;
}

/* Trace tcp_connect - outgoing connection initiation (SYN sent)
 * kprobe: called when tcp_connect() is entered
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

    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));

    // Extract connection info
    make_key_from_sock(sk, &key);
    extract_ipv4_addrs(sk, evt.src_ip, evt.dst_ip);
    extract_ports(sk, &evt.src_port, &evt.dst_port);
    evt.protocol = IPPROTO_TCP;

    // Store in connections map
    struct connection_entry entry = {};
    entry.timestamp_ns = evt.timestamp_ns;
    entry.pid = evt.pid;
    entry.direction = DIR_OUTGOING;
    entry.state = CONN_STATE_SYN_SENT;
    entry.tcp_flags = TCP_SYN;
    bpf_get_current_comm(&entry.comm, sizeof(entry.comm));

    bpf_map_update_elem(&connections, &key, &entry, BPF_ANY);

    submit_event(&evt);
    return 0;
}

/* Trace tcp_v4_accept - server accepts incoming IPv4 connection
 * kretprobe: called when tcp_v4_accept() returns
 * Returns: struct sock * (new connection socket)
 */
SEC("kretprobe/tcp_v4_accept")
int BPF_KRETPROBE(tcp_v4_accept, struct sock *ret_sk)
{
    if (!track_incoming)
        return 0;

    // Check if socket is valid
    if (ret_sk == NULL)
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
    evt.tcp_flags = TCP_SYN | TCP_ACK;

    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));

    // Extract connection info from the NEW socket
    // For incoming connection:
    // - skc_rcv_saddr = local IP (server)
    // - skc_daddr = remote IP (client)
    // - skc_num = local port (server, e.g., 22)
    // - skc_dport = remote port (client, ephemeral)
    extract_ipv4_addrs(ret_sk, evt.src_ip, evt.dst_ip);
    extract_ports(ret_sk, &evt.src_port, &evt.dst_port);
    evt.protocol = IPPROTO_TCP;

    // For incoming: src is client, dst is server
    // But extract_ipv4_addrs gives us: src=local(server), dst=remote(client)
    // We need to swap for consistent event format
    __u8 tmp_ip[16];
    __builtin_memcpy(tmp_ip, evt.src_ip, 16);
    __builtin_memcpy(evt.src_ip, evt.dst_ip, 16);
    __builtin_memcpy(evt.dst_ip, tmp_ip, 16);

    __u16 tmp_port = evt.src_port;
    evt.src_port = evt.dst_port;
    evt.dst_port = tmp_port;

    // Create key for this connection
    make_key_from_sock(ret_sk, &key);

    // Store in connections map
    struct connection_entry entry = {};
    entry.timestamp_ns = evt.timestamp_ns;
    entry.pid = evt.pid;
    entry.direction = DIR_INCOMING;
    entry.state = CONN_STATE_ESTABLISHED;
    entry.tcp_flags = TCP_SYN | TCP_ACK;
    bpf_get_current_comm(&entry.comm, sizeof(entry.comm));

    bpf_map_update_elem(&connections, &key, &entry, BPF_ANY);

    submit_event(&evt);
    return 0;
}

/* Trace tcp_v6_accept - server accepts incoming IPv6 connection
 * kretprobe: called when tcp_v6_accept() returns
 * Returns: struct sock * (new connection socket)
 */
SEC("kretprobe/tcp_v6_accept")
int BPF_KRETPROBE(tcp_v6_accept, struct sock *ret_sk)
{
    if (!track_incoming)
        return 0;

    // Check if socket is valid
    if (ret_sk == NULL)
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
    evt.tcp_flags = TCP_SYN | TCP_ACK;
    evt.protocol = IPPROTO_TCP;

    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));

    // Extract IPv6 addresses from sock
    // skc_v6_daddr = remote (client)
    // skc_v6_rcv_saddr = local (server)
    struct in6_addr saddr6, daddr6;
    bpf_probe_read_kernel(&saddr6, sizeof(saddr6), &ret_sk->__sk_common.skc_v6_rcv_saddr);
    bpf_probe_read_kernel(&daddr6, sizeof(daddr6), &ret_sk->__sk_common.skc_v6_daddr);

    // Copy addresses (swap for consistent format: src=client, dst=server)
    __builtin_memcpy(evt.src_ip, daddr6.s6_addr, 16);
    __builtin_memcpy(evt.dst_ip, saddr6.s6_addr, 16);
    __builtin_memcpy(key.src_ip, daddr6.s6_addr, 16);
    __builtin_memcpy(key.dst_ip, saddr6.s6_addr, 16);

    // Extract ports
    extract_ports(ret_sk, &evt.src_port, &evt.dst_port);
    key.src_port = evt.src_port;
    key.dst_port = evt.dst_port;
    key.protocol = IPPROTO_TCP;

    // Store in connections map
    struct connection_entry entry = {};
    entry.timestamp_ns = evt.timestamp_ns;
    entry.pid = evt.pid;
    entry.direction = DIR_INCOMING;
    entry.state = CONN_STATE_ESTABLISHED;
    entry.tcp_flags = TCP_SYN | TCP_ACK;
    bpf_get_current_comm(&entry.comm, sizeof(entry.comm));

    bpf_map_update_elem(&connections, &key, &entry, BPF_ANY);

    submit_event(&evt);
    return 0;
}

/* Trace tcp_close - connection closing
 * kretprobe: called when tcp_close() returns
 */
SEC("kretprobe/tcp_close")
int BPF_KRETPROBE(tcp_close, struct sock *sk)
{
    if (!track_closes)
        return 0;

    struct connection_event evt = {};
    struct connection_key key = {};

    evt.timestamp_ns = bpf_ktime_get_ns();
    evt.pid_tgid = bpf_get_current_pid_tgid();
    evt.pid = evt.pid_tgid >> 32;
    evt.tid = evt.pid_tgid & 0xFFFFFFFF;
    evt.state = CONN_STATE_CLOSED;
    evt.event_type = CONN_EVENT_CLOSED;

    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));

    // Extract connection info
    make_key_from_sock(sk, &key);
    extract_ipv4_addrs(sk, evt.src_ip, evt.dst_ip);
    extract_ports(sk, &evt.src_port, &evt.dst_port);
    evt.protocol = IPPROTO_TCP;

    // Look up stored connection to get original direction
    struct connection_entry *entry;
    entry = bpf_map_lookup_elem(&connections, &key);
    if (entry) {
        evt.direction = entry->direction;
        __builtin_memcpy(evt.comm, entry->comm, TASK_COMM_LEN);

        // Remove from tracking map
        bpf_map_delete_elem(&connections, &key);
    }

    submit_event(&evt);
    return 0;
}

char LICENSE[] SEC("license") = "Dual BSD/GPL";
