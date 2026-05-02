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
 * - kretprobe/tcp_close: закрытие соединений
 *
 * Supported kernels:
 * - 4.19.x+ (Debian 10/11, Ubuntu 18.04+)
 * - 5.15.x (Ubuntu 22.04 GA)
 * - 6.x+ (All modern kernels)
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
    char comm[TASK_COMM_LEN];
};

struct connection_key {
    __u8 src_ip[16];
    __u8 dst_ip[16];
    __u16 src_port;
    __u16 dst_port;
    __u8 protocol;
};

struct connection_entry {
    __u64 timestamp_ns;
    __u32 pid;
    __u8 direction;
    __u8 state;
    __u8 tcp_flags;
    char comm[TASK_COMM_LEN];
};

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

static __always_inline void extract_ipv4_addrs(struct sock *sk, __u8 *saddr, __u8 *daddr)
{
    __u32 saddr4, daddr4;
    bpf_probe_read_kernel(&saddr4, sizeof(saddr4), &sk->__sk_common.skc_rcv_saddr);
    bpf_probe_read_kernel(&daddr4, sizeof(daddr4), &sk->__sk_common.skc_daddr);

    __builtin_memset(saddr, 0, 16);
    __builtin_memset(daddr, 0, 16);
    saddr[10] = 0xff;
    saddr[11] = 0xff;
    daddr[10] = 0xff;
    daddr[11] = 0xff;

    saddr[12] = (__u8)((saddr4 >> 24) & 0xFF);
    saddr[13] = (__u8)((saddr4 >> 16) & 0xFF);
    saddr[14] = (__u8)((saddr4 >> 8) & 0xFF);
    saddr[15] = (__u8)(saddr4 & 0xFF);

    daddr[12] = (__u8)((daddr4 >> 24) & 0xFF);
    daddr[13] = (__u8)((daddr4 >> 16) & 0xFF);
    daddr[14] = (__u8)((daddr4 >> 8) & 0xFF);
    daddr[15] = (__u8)(daddr4 & 0xFF);
}

static __always_inline void extract_ports(struct sock *sk, __u16 *sport, __u16 *dport)
{
    bpf_probe_read_kernel(sport, sizeof(*sport), &sk->__sk_common.skc_num);
    bpf_probe_read_kernel(dport, sizeof(*dport), &sk->__sk_common.skc_dport);
    *dport = bpf_ntohs(*dport);
}

static __always_inline void make_key_from_sock(struct sock *sk, struct connection_key *key)
{
    extract_ipv4_addrs(sk, key->src_ip, key->dst_ip);
    extract_ports(sk, &key->src_port, &key->dst_port);
    key->protocol = IPPROTO_TCP;
}

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

    make_key_from_sock(sk, &key);
    extract_ipv4_addrs(sk, evt.src_ip, evt.dst_ip);
    extract_ports(sk, &evt.src_port, &evt.dst_port);
    evt.protocol = IPPROTO_TCP;

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

SEC("kretprobe/tcp_v4_accept")
int BPF_KRETPROBE(tcp_v4_accept, struct sock *ret_sk)
{
    if (!track_incoming)
        return 0;

    if (!ret_sk)
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

    extract_ipv4_addrs(ret_sk, evt.src_ip, evt.dst_ip);
    extract_ports(ret_sk, &evt.src_port, &evt.dst_port);

    // Swap: src=client, dst=server
    __u8 tmp_ip[16];
    __builtin_memcpy(tmp_ip, evt.src_ip, 16);
    __builtin_memcpy(evt.src_ip, evt.dst_ip, 16);
    __builtin_memcpy(evt.dst_ip, tmp_ip, 16);

    __u16 tmp_port = evt.src_port;
    evt.src_port = evt.dst_port;
    evt.dst_port = tmp_port;

    make_key_from_sock(ret_sk, &key);

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

    make_key_from_sock(sk, &key);
    extract_ipv4_addrs(sk, evt.src_ip, evt.dst_ip);
    extract_ports(sk, &evt.src_port, &evt.dst_port);
    evt.protocol = IPPROTO_TCP;

    struct connection_entry *entry;
    entry = bpf_map_lookup_elem(&connections, &key);
    if (entry) {
        evt.direction = entry->direction;
        __builtin_memcpy(evt.comm, entry->comm, TASK_COMM_LEN);
        bpf_map_delete_elem(&connections, &key);
    }

    submit_event(&evt);
    return 0;
}

char LICENSE[] SEC("license") = "Dual BSD/GPL";
