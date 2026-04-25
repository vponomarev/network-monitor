// SPDX-License-Identifier: GPL-2.0 OR BSD-3-Clause
/* Copyright (c) 2024 Network Monitor Contributors */

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define TASK_COMM_LEN 16
#define MAX_CONNECTIONS 10240

// Connection event structure
struct connection_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u8 source_ip[16];
    __u8 dest_ip[16];
    __u16 source_port;
    __u16 dest_port;
    __u8 protocol;
    __u8 direction;  // 0 = incoming, 1 = outgoing
    char comm[TASK_COMM_LEN];
};

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} events SEC(".maps");

// Track active connections
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_CONNECTIONS);
    __type(key, __u64);  // connection key
    __type(value, struct connection_event);
} connections SEC(".maps");

// Configuration
volatile const bool track_incoming = true;
volatile const bool track_outgoing = true;

// Generate connection key from IPs and ports
static __always_inline __u64 make_connection_key(
    const __u8 *saddr, const __u8 *daddr,
    __u16 sport, __u16 dport)
{
    __u64 key = 0;
    
    // Simple hash combining IPs and ports
    #pragma unroll
    for (int i = 0; i < 4; i++) {
        key ^= ((__u64)saddr[i]) << (i * 8);
        key ^= ((__u64)daddr[i]) << ((7 - i) * 8);
    }
    key ^= ((__u64)sport) << 32;
    key ^= ((__u64)dport) << 48;
    
    return key;
}

// Submit connection event to ring buffer
static __always_inline void submit_event(struct connection_event *evt)
{
    struct connection_event *event;
    
    event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return;
    
    *event = *evt;
    bpf_ringbuf_submit(event, 0);
}

// Trace socket connect (outgoing connections)
SEC("kprobe/tcp_connect")
int BPF_KPROBE(tcp_connect, struct sock *sk)
{
    if (!track_outgoing)
        return 0;
    
    struct connection_event evt = {};
    evt.timestamp = bpf_ktime_get_ns();
    evt.tid = bpf_get_current_pid_tgid();
    evt.pid = evt.tid >> 32;
    evt.direction = 1;  // outgoing
    
    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));
    
    // Extract socket info (simplified - real impl would use BTF)
    // evt.source_port = ...
    // evt.dest_port = ...
    // evt.protocol = ...
    
    submit_event(&evt);
    return 0;
}

// Trace socket accept (incoming connections)
SEC("kprobe/tcp_v4_accept")
int BPF_KPROBE(tcp_v4_accept, struct sock *sk, struct sk_buff *skb)
{
    if (!track_incoming)
        return 0;
    
    struct connection_event evt = {};
    evt.timestamp = bpf_ktime_get_ns();
    evt.tid = bpf_get_current_pid_tgid();
    evt.pid = evt.tid >> 32;
    evt.direction = 0;  // incoming
    
    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));
    
    submit_event(&evt);
    return 0;
}

// Trace socket close
SEC("kprobe/tcp_close")
int BPF_KPROBE(tcp_close, struct sock *sk)
{
    struct connection_event evt = {};
    evt.timestamp = bpf_ktime_get_ns();
    evt.tid = bpf_get_current_pid_tgid();
    evt.pid = evt.tid >> 32;
    
    bpf_get_current_comm(&evt.comm, sizeof(evt.comm));
    
    submit_event(&evt);
    return 0;
}

char LICENSE[] SEC("license") = "Dual BSD/GPL";
