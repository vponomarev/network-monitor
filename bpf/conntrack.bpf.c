// SPDX-License-Identifier: GPL-2.0 OR BSD-3-Clause
// Copyright (c) 2024 Network Monitor Contributors

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>
#include "conntrack.bpf.h"

/*
 * Universal eBPF Connection Tracker
 * 
 * Uses fentry/fexit for kernel 5.5+ with BTF support
 * Falls back to kprobe for older kernels (compile-time switch)
 * 
 * Supported kernels:
 * - 5.15.x (Ubuntu 22.04 GA)
 * - 6.2.x - 6.5.x (Ubuntu HWE)
 * - 6.8.x+ (Debian 12, Proxmox 8.4)
 * - 6.10.x - 6.17.x (Latest mainline)
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

/* Extract IPv4 addresses from sock */
static __always_inline void extract_ipv4_addrs(struct sock *sk, __u8 *saddr, __u8 *daddr)
{
    __u32 saddr4, daddr4;

    // Read source and dest IPv4 addresses
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

    // Extract bytes from host-order __u32
    // For 192.168.5.214 in host order on little-endian: stored as 0xD605A8C0
    // We want: [12]=192, [13]=168, [14]=5, [15]=214
    saddr[12] = (__u8)(saddr4 & 0xFF);
    saddr[13] = (__u8)((saddr4 >> 8) & 0xFF);
    saddr[14] = (__u8)((saddr4 >> 16) & 0xFF);
    saddr[15] = (__u8)((saddr4 >> 24) & 0xFF);
    
    daddr[12] = (__u8)(daddr4 & 0xFF);
    daddr[13] = (__u8)((daddr4 >> 8) & 0xFF);
    daddr[14] = (__u8)((daddr4 >> 16) & 0xFF);
    daddr[15] = (__u8)((daddr4 >> 24) & 0xFF);
}

/* Extract ports from sock */
static __always_inline void extract_ports(struct sock *sk, __u16 *sport, __u16 *dport)
{
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

/* Trace tcp_connect - outgoing connection initiation (SYN sent) */
SEC("fentry/tcp_connect")
int BPF_PROG(tcp_connect, struct sock *sk)
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

/* Trace tcp_v4_rcv - check for incoming SYN using fentry */
SEC("fentry/tcp_v4_rcv")
int BPF_PROG(tcp_v4_rcv, struct sk_buff *skb)
{
    if (!track_incoming)
        return 0;

    struct connection_event evt = {};
    struct connection_key key = {};

    evt.timestamp_ns = bpf_ktime_get_ns();
    evt.pid_tgid = bpf_get_current_pid_tgid();
    evt.pid = evt.pid_tgid >> 32;
    evt.tid = evt.pid_tgid & 0xFFFFFFFF;
    evt.protocol = IPPROTO_TCP;

    // Read IP header directly from skb->data using BPF_CORE_READ
    void *data = (void *)BPF_CORE_READ(skb, data);
    if (!data)
        return 0;

    struct iphdr *iph = (struct iphdr *)data;

    // Read source and dest IP from IP header as bytes
    // Network byte order: [byte0][byte1][byte2][byte3] = [192][168][5][165]
    __u8 saddr_bytes[4], daddr_bytes[4];
    bpf_probe_read_kernel(&saddr_bytes, sizeof(saddr_bytes), &iph->saddr);
    bpf_probe_read_kernel(&daddr_bytes, sizeof(daddr_bytes), &iph->daddr);

    // Copy to IPv4-mapped IPv6 format
    // saddr_bytes[0] = 192, [1] = 168, [2] = 5, [3] = 165
    evt.src_ip[10] = 0xff;
    evt.src_ip[11] = 0xff;
    evt.src_ip[12] = saddr_bytes[0];
    evt.src_ip[13] = saddr_bytes[1];
    evt.src_ip[14] = saddr_bytes[2];
    evt.src_ip[15] = saddr_bytes[3];

    evt.dst_ip[10] = 0xff;
    evt.dst_ip[11] = 0xff;
    evt.dst_ip[12] = daddr_bytes[0];
    evt.dst_ip[13] = daddr_bytes[1];
    evt.dst_ip[14] = daddr_bytes[2];
    evt.dst_ip[15] = daddr_bytes[3];

    // Read TCP header (IP header is 20 bytes for IPv4 without options)
    struct tcphdr *th = (struct tcphdr *)(data + sizeof(struct iphdr));
    if (!th)
        return 0;

    // Read TCP flags
    __u8 tcp_flags;
    bpf_probe_read_kernel(&tcp_flags, sizeof(tcp_flags), &th->flags);

    // Only interested in SYN or SYN+ACK
    if ((tcp_flags & TCP_SYN) == 0)
        return 0;

    evt.tcp_flags = tcp_flags;

    // Read ports from TCP header
    bpf_probe_read_kernel(&evt.src_port, sizeof(evt.src_port), &th->source);
    bpf_probe_read_kernel(&evt.dst_port, sizeof(evt.dst_port), &th->dest);
    evt.src_port = bpf_ntohs(evt.src_port);
    evt.dst_port = bpf_ntohs(evt.dst_port);

    // Determine if this is incoming SYN or SYN+ACK
    if (tcp_flags & TCP_ACK) {
        // SYN+ACK: response to our outgoing SYN
        evt.direction = DIR_OUTGOING;
        evt.state = CONN_STATE_ESTABLISHED;
        evt.event_type = CONN_EVENT_ESTABLISHED;
    } else {
        // Pure SYN: incoming connection attempt
        evt.direction = DIR_INCOMING;
        evt.state = CONN_STATE_SYN_RECEIVED;
        evt.event_type = CONN_EVENT_NEW;

        // Create key for this connection
        __builtin_memcpy(key.src_ip, evt.src_ip, 16);
        __builtin_memcpy(key.dst_ip, evt.dst_ip, 16);
        key.src_port = evt.src_port;
        key.dst_port = evt.dst_port;
        key.protocol = IPPROTO_TCP;

        // Store connection
        struct connection_entry entry = {};
        entry.timestamp_ns = evt.timestamp_ns;
        entry.direction = DIR_INCOMING;
        entry.state = CONN_STATE_SYN_RECEIVED;
        entry.tcp_flags = tcp_flags;
        __builtin_memcpy(entry.comm, "unknown", 8);

        bpf_map_update_elem(&connections, &key, &entry, BPF_ANY);
    }

    submit_event(&evt);
    return 0;
}

/* Trace tcp_v4_accept - server accepts incoming connection
 * NOTE: Disabled - tcp_v4_rcv + inet_sock_set_state provide equivalent functionality
 * kprobe not supported on kernel 6.8+, fentry not available for this function
 */
#if 0
SEC("kprobe/tcp_v4_accept")
int BPF_KPROBE(tcp_v4_accept, struct sock *sk, struct sk_buff *skb)
{
    if (!track_incoming)
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

    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));

    // Extract connection info
    make_key_from_sock(sk, &key);
    extract_ipv4_addrs(sk, evt.src_ip, evt.dst_ip);
    extract_ports(sk, &evt.src_port, &evt.dst_port);
    evt.protocol = IPPROTO_TCP;

    // Update connection entry to established
    struct connection_entry *entry;
    entry = bpf_map_lookup_elem(&connections, &key);
    if (entry) {
        entry->state = CONN_STATE_ESTABLISHED;
        entry->tcp_flags = TCP_SYN | TCP_ACK;
        __builtin_memcpy(entry->comm, evt.comm, TASK_COMM_LEN);
    }

    submit_event(&evt);
    return 0;
}
#endif

/* Trace tcp_close - connection closing */
SEC("fentry/tcp_close")
int BPF_PROG(tcp_close, struct sock *sk)
{
    if (!track_closes)
        return 0;

    struct connection_event evt = {};
    struct connection_key key = {};

    evt.timestamp_ns = bpf_ktime_get_ns();
    evt.pid_tgid = bpf_get_current_pid_tgid();
    evt.pid = evt.pid_tgid >> 32;
    evt.tid = evt.pid_tgid & 0xFFFFFFFF;
    evt.direction = DIR_OUTGOING;  // Will be updated from stored entry
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

/* Trace inet_sock_set_state - generic state changes */
SEC("tracepoint/sock/inet_sock_set_state")
int inet_sock_set_state(struct trace_event_raw_inet_sock_set_state *ctx)
{
    // Read fields using BPF_CORE_READ for CO-RE compatibility
    __u32 oldstate = BPF_CORE_READ(ctx, oldstate);
    __u32 newstate = BPF_CORE_READ(ctx, newstate);
    __u16 sport = BPF_CORE_READ(ctx, sport);
    __u16 dport = BPF_CORE_READ(ctx, dport);
    __u16 protocol = BPF_CORE_READ(ctx, protocol);
    __u32 saddr32 = BPF_CORE_READ(ctx, saddr);
    __u32 daddr32 = BPF_CORE_READ(ctx, daddr);

    if (protocol != IPPROTO_TCP)
        return 0;

    // Debug: print state transition
    bpf_printk("inet_sock: sport=%d dport=%d oldstate=%d newstate=%d", 
               sport, dport, oldstate, newstate);

    struct connection_event evt = {};
    struct connection_key key = {};

    evt.timestamp_ns = bpf_ktime_get_ns();
    evt.pid_tgid = bpf_get_current_pid_tgid();
    evt.pid = evt.pid_tgid >> 32;
    evt.tid = evt.pid_tgid & 0xFFFFFFFF;
    evt.src_port = bpf_ntohs(sport);
    evt.dst_port = bpf_ntohs(dport);
    evt.protocol = IPPROTO_TCP;

    // Convert from network byte order to host byte order
    __u32 saddr_host = bpf_ntohl(saddr32);
    __u32 daddr_host = bpf_ntohl(daddr32);

    // Debug: print host order IP values
    bpf_printk("inet_sock: saddr_host=0x%x daddr_host=0x%x", saddr_host, daddr_host);

    // Extract bytes from host-order __u32
    // For 192.168.5.165: saddr_host = 0xC0A805A5
    evt.src_ip[10] = 0xff;
    evt.src_ip[11] = 0xff;
    evt.src_ip[12] = (__u8)((saddr_host >> 24) & 0xFF);
    evt.src_ip[13] = (__u8)((saddr_host >> 16) & 0xFF);
    evt.src_ip[14] = (__u8)((saddr_host >> 8) & 0xFF);
    evt.src_ip[15] = (__u8)(saddr_host & 0xFF);

    evt.dst_ip[10] = 0xff;
    evt.dst_ip[11] = 0xff;
    evt.dst_ip[12] = (__u8)((daddr_host >> 24) & 0xFF);
    evt.dst_ip[13] = (__u8)((daddr_host >> 16) & 0xFF);
    evt.dst_ip[14] = (__u8)((daddr_host >> 8) & 0xFF);
    evt.dst_ip[15] = (__u8)(daddr_host & 0xFF);

    // Debug: print extracted IP bytes
    bpf_printk("inet_sock: src_ip=%d.%d.%d.%d dst_ip=%d.%d.%d.%d",
               evt.src_ip[12], evt.src_ip[13], evt.src_ip[14], evt.src_ip[15],
               evt.dst_ip[12], evt.dst_ip[13], evt.dst_ip[14], evt.dst_ip[15]);

    // Determine event type and direction based on state transition
    switch (newstate) {
        case TCP_ESTABLISHED:
            bpf_printk("inet_sock: TCP_ESTABLISHED");
            // Determine direction: if src_port is ephemeral (>1024), it's outgoing
            // If dst_port is well-known (<=1024), it's incoming
            if (evt.dst_port <= 1024 && evt.src_port > 1024) {
                evt.direction = DIR_INCOMING;
            } else {
                evt.direction = DIR_OUTGOING;
            }
            evt.state = CONN_STATE_ESTABLISHED;
            evt.event_type = CONN_EVENT_ESTABLISHED;
            break;
        case TCP_CLOSE:
        case TCP_CLOSE_WAIT:
            bpf_printk("inet_sock: TCP_CLOSE");
            // Determine direction same way
            if (evt.dst_port <= 1024 && evt.src_port > 1024) {
                evt.direction = DIR_INCOMING;
            } else {
                evt.direction = DIR_OUTGOING;
            }
            evt.state = CONN_STATE_CLOSED;
            evt.event_type = CONN_EVENT_CLOSED;
            break;
        case TCP_SYN_RECV:
            bpf_printk("inet_sock: TCP_SYN_RECV - incoming");
            // Incoming connection in SYN_RECV state
            evt.direction = DIR_INCOMING;
            evt.state = CONN_STATE_SYN_RECEIVED;
            evt.event_type = CONN_EVENT_NEW;
            break;
        case TCP_SYN_SENT:
            bpf_printk("inet_sock: TCP_SYN_SENT - outgoing");
            // Outgoing connection SYN sent (backup to tcp_connect)
            evt.direction = DIR_OUTGOING;
            evt.state = CONN_STATE_SYN_SENT;
            evt.event_type = CONN_EVENT_NEW;
            break;
        default:
            // Unknown state - skip
            bpf_printk("inet_sock: unknown state %d", newstate);
            return 0;
    }

    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));
    bpf_printk("inet_sock: submitting event for %s", evt.comm);
    submit_event(&evt);
    return 0;
}

char LICENSE[] SEC("license") = "Dual BSD/GPL";
