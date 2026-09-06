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
#ifndef TCP_ESTABLISHED
#define TCP_ESTABLISHED 1
#endif
#ifndef TCP_CLOSE
#define TCP_CLOSE 7
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
    __u64 socket_id;
    __u64 started_ns;
    __u64 handshake_ns;
};

/* Connection key - packed to avoid padding issues */
struct connection_key {
    __u8 src_ip[16];
    __u8 dst_ip[16];
    __u16 src_port;
    __u16 dst_port;
    __u8 protocol;
    __u64 socket_id;
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

/* Events discarded because userspace did not drain the ring buffer in time. */
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 3);
    __type(key, __u32);
    __type(value, __u64);
} event_drops SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 256);
    __type(key, __u16);
    __type(value, __u8);
} filter_ports SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u8);
} filter_config SEC(".maps");

static __always_inline bool port_is_tracked(__u16 sport, __u16 dport)
{
    __u32 zero = 0;
    __u8 *enabled = bpf_map_lookup_elem(&filter_config, &zero);
    if (!enabled || !*enabled)
        return true;
    return bpf_map_lookup_elem(&filter_ports, &sport) ||
           bpf_map_lookup_elem(&filter_ports, &dport);
}

#define DROP_RINGBUF_FULL 0
#define DROP_CONNECTIONS_MAP_FULL 1
#define DROP_PENDING_MAP_FULL 2

static __always_inline void count_drop(__u32 reason)
{
    __u64 *drops = bpf_map_lookup_elem(&event_drops, &reason);
    if (drops)
        (*drops)++;
}

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_CONNECTIONS);
    __type(key, struct connection_key);
    __type(value, struct connection_entry);
} connections SEC(".maps");

/* Socket saved across tcp_v4_connect entry/return so the return probe sees
 * the final ephemeral source port assigned by the kernel. */
struct pending_outgoing_meta {
    __u64 timestamp_ns;
    __u64 pid_tgid;
    char comm[TASK_COMM_LEN];
};

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 16384);
    __type(key, __u64);
    __type(value, struct pending_outgoing_meta);
} pending_outgoing SEC(".maps");

volatile const bool track_incoming = true;
volatile const bool track_outgoing = true;
volatile const bool track_closes = true;

static __always_inline void submit_event(struct connection_event *evt)
{
    struct connection_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        count_drop(DROP_RINGBUF_FULL);
        return;
    }
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

    /* Values are stored in network byte order; copying their memory bytes
     * preserves dotted-quad order on little- and big-endian hosts. */
    __builtin_memcpy(&saddr[12], &saddr4, sizeof(saddr4));
    __builtin_memcpy(&daddr[12], &daddr4, sizeof(daddr4));
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
    __u16 sport, dport;

    extract_ipv4_addrs(sk, key->src_ip, key->dst_ip);
    extract_ports(sk, &sport, &dport);
    key->src_port = sport;
    key->dst_port = dport;
    key->protocol = IPPROTO_TCP;
    key->socket_id = (__u64)sk;
}

/* -------------------------------------------------------------------------
 * kprobe/tcp_connect — outgoing connections (fallback for kernels < 5.14).
 *
 * Outgoing connections are correlated through inet_sock_set_state below.
 * ---------------------------------------------------------------------- */
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
    if (!track_outgoing && !track_incoming && !track_closes)
        return 0;

    /* Correlate SYN_SENT (process context) with ESTABLISHED (complete tuple). */
    if (BPF_CORE_READ(ctx, protocol) != IPPROTO_TCP)
        return 0;
    if (BPF_CORE_READ(ctx, family) != AF_INET)
        return 0;

    __u32 oldstate = BPF_CORE_READ(ctx, oldstate);
    __u32 newstate = BPF_CORE_READ(ctx, newstate);
    __u64 skaddr = (__u64)BPF_CORE_READ(ctx, skaddr);
    __u16 filter_sport = BPF_CORE_READ(ctx, sport);
    __u16 filter_dport = BPF_CORE_READ(ctx, dport);
    if (!port_is_tracked(filter_sport, filter_dport))
        return 0;

    bool starting = track_outgoing && newstate == TCP_SYN_SENT;
    bool established = track_outgoing && oldstate == TCP_SYN_SENT && newstate == TCP_ESTABLISHED;
    bool closed = newstate == TCP_CLOSE;
    if (!starting && !established && !closed)
        return 0;
    if (starting) {
        struct pending_outgoing_meta initial = {};
        initial.timestamp_ns = bpf_ktime_get_ns();
        initial.pid_tgid = bpf_get_current_pid_tgid();
        bpf_get_current_comm(&initial.comm, sizeof(initial.comm));
        if (bpf_map_update_elem(&pending_outgoing, &skaddr, &initial, BPF_ANY) != 0)
            count_drop(DROP_PENDING_MAP_FULL);
    }
    struct pending_outgoing_meta *meta = bpf_map_lookup_elem(&pending_outgoing, &skaddr);
    bool failed = closed && meta;

    struct connection_event evt = {};
    evt.timestamp_ns = bpf_ktime_get_ns();
    evt.socket_id = skaddr;
    evt.started_ns = meta ? meta->timestamp_ns : evt.timestamp_ns;
    evt.handshake_ns = established && meta ? evt.timestamp_ns - meta->timestamp_ns : 0;
    evt.pid_tgid = meta ? meta->pid_tgid : bpf_get_current_pid_tgid();
    evt.pid = (__u32)(evt.pid_tgid >> 32);
    evt.tid = (__u32)(evt.pid_tgid & 0xFFFFFFFF);
    evt.direction = (starting || established || failed) ? DIR_OUTGOING : DIR_UNKNOWN;
    evt.state = starting ? CONN_STATE_SYN_SENT : (established ? CONN_STATE_ESTABLISHED : CONN_STATE_CLOSED);
    evt.event_type = starting ? CONN_EVENT_NEW : (failed ? CONN_EVENT_FAILED : (established ? CONN_EVENT_ESTABLISHED : CONN_EVENT_CLOSED));
    evt.tcp_flags = established ? (TCP_SYN | TCP_ACK) : TCP_FIN;
    evt.protocol = IPPROTO_TCP;

    if (meta)
        __builtin_memcpy(evt.comm, meta->comm, TASK_COMM_LEN);
    else
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

    evt.src_port = BPF_CORE_READ(ctx, sport);
    evt.dst_port = BPF_CORE_READ(ctx, dport);

    /* Filter out invalid connections (qemu-ga, etc.) */
    __u32 saddr4 = ((__u32)saddr_bytes[0] << 24) | ((__u32)saddr_bytes[1] << 16) |
                   ((__u32)saddr_bytes[2] << 8) | (__u32)saddr_bytes[3];
    __u32 daddr4 = ((__u32)daddr_bytes[0] << 24) | ((__u32)daddr_bytes[1] << 16) |
                   ((__u32)daddr_bytes[2] << 8) | (__u32)daddr_bytes[3];

    if (saddr4 == 0 && daddr4 == 0) {
        return 0;
    }

    struct connection_key key = {};
    __builtin_memcpy(key.src_ip, evt.src_ip, 16);
    __builtin_memcpy(key.dst_ip, evt.dst_ip, 16);
    key.src_port = evt.src_port;
    key.dst_port = evt.dst_port;
    key.protocol = IPPROTO_TCP;
    key.socket_id = skaddr;

    if (starting || failed) {
        if (failed)
            bpf_map_delete_elem(&pending_outgoing, &skaddr);
        submit_event(&evt);
        return 0;
    }

    if (established) {
        struct connection_entry entry = {};
        entry.timestamp_ns = evt.started_ns;
        entry.pid = evt.pid;
        entry.direction = DIR_OUTGOING;
        entry.state = CONN_STATE_ESTABLISHED;
        entry.tcp_flags = TCP_SYN | TCP_ACK;
        __builtin_memcpy(entry.comm, evt.comm, TASK_COMM_LEN);
        if (bpf_map_update_elem(&connections, &key, &entry, BPF_ANY) != 0)
            count_drop(DROP_CONNECTIONS_MAP_FULL);
        bpf_map_delete_elem(&pending_outgoing, &skaddr);
    } else {
        struct connection_entry *entry = bpf_map_lookup_elem(&connections, &key);
        if (entry) {
            evt.started_ns = entry->timestamp_ns;
            evt.direction = entry->direction;
            evt.pid = entry->pid;
            __builtin_memcpy(evt.comm, entry->comm, TASK_COMM_LEN);

            if (entry->direction == DIR_INCOMING) {
                __u8 tmp_ip[16];
                __u16 tmp_port;
                __builtin_memcpy(tmp_ip, evt.src_ip, sizeof(tmp_ip));
                __builtin_memcpy(evt.src_ip, evt.dst_ip, sizeof(evt.src_ip));
                __builtin_memcpy(evt.dst_ip, tmp_ip, sizeof(evt.dst_ip));
                tmp_port = evt.src_port;
                evt.src_port = evt.dst_port;
                evt.dst_port = tmp_port;
            }
            bpf_map_delete_elem(&connections, &key);
        } else {
            /* Ignore duplicate closes and sockets that predate the tracker. */
            return 0;
        }
    }

    if (!closed || track_closes)
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
 * limitation. Userspace reports "unknown" when comm is unavailable and never
 * blocks the ring-buffer consumer on /proc I/O.
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
    evt.socket_id = (__u64)ret_sk;
    evt.started_ns = evt.timestamp_ns;
    evt.direction = DIR_INCOMING;
    evt.state = CONN_STATE_ESTABLISHED;
    evt.event_type = CONN_EVENT_ESTABLISHED;
    evt.tcp_flags = TCP_SYN | TCP_ACK;  // Conditional: connection already ESTABLISHED
    evt.protocol = IPPROTO_TCP;

    // Get process name once
    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));

    // Create key FIRST from raw socket values (local=src, remote=dst)
    make_key_from_sock(ret_sk, &key);
    if (!port_is_tracked(key.src_port, key.dst_port))
        return 0;

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
    entry.timestamp_ns = evt.started_ns;
    entry.pid = evt.pid;
    entry.direction = DIR_INCOMING;
    entry.state = CONN_STATE_ESTABLISHED;
    entry.tcp_flags = TCP_SYN | TCP_ACK;
    __builtin_memcpy(entry.comm, evt.comm, TASK_COMM_LEN);

    if (bpf_map_update_elem(&connections, &key, &entry, BPF_ANY) != 0)
        count_drop(DROP_CONNECTIONS_MAP_FULL);

    submit_event(&evt);
    return 0;
}

char LICENSE[] SEC("license") = "Dual BSD/GPL";
