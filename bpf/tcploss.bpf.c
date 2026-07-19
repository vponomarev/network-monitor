// SPDX-License-Identifier: GPL-2.0 OR BSD-3-Clause
// Copyright (c) 2026 Network Monitor Contributors

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

/*
 * eBPF TCP loss collector.
 *
 * Attaches to tracepoint/tcp/tcp_retransmit_skb and emits one structured event
 * per TCP retransmission (a proxy signal for packet loss on the path) into a
 * ring buffer consumed by userspace (internal/losscollector).
 *
 * This replaces the legacy text scraping of /sys/kernel/tracing/trace_pipe:
 * the ring buffer has a single owner, is lossless under backpressure and needs
 * no fragile text parsing.
 *
 * Supported kernels: any with the stable tcp_retransmit_skb tracepoint and BTF
 * (tested on 5.15, 6.1, 6.8, 6.12).
 *
 * Limitations:
 * - IPv4 only (AF_INET). IPv6 (AF_INET6) events are ignored — see family check.
 * - Handshake retransmits are intentionally ignored. Repeated SYN/SYN-ACK
 *   packets indicate connection-establishment failure, not loss on an
 *   established TCP flow.
 * - No bpf_printk in the hot path (unlike conntrack.bpf.c) to avoid trace_pipe
 *   pollution and per-event overhead.
 */

#ifndef AF_INET
#define AF_INET 2
#endif

/*
 * Userspace-facing event. Layout is FIXED and must byte-match the Go struct
 * bpfLossEvent in internal/losscollector (validated at runtime by size/offset).
 *
 *   timestamp_ns  offset 0   size 8
 *   src_ip[16]    offset 8   size 16   (IPv4-mapped IPv6, network-order in [12..15])
 *   dst_ip[16]    offset 24  size 16
 *   src_port      offset 40  size 2    (host byte order)
 *   dst_port      offset 42  size 2    (host byte order)
 *   family        offset 44  size 1    (AF_INET = 2)
 *   _pad[3]       offset 45  size 3    (align to 8)
 *   total: 48 bytes
 */
struct tcploss_event {
    __u64 timestamp_ns;
    __u8  src_ip[16];
    __u8  dst_ip[16];
    __u16 src_port;
    __u16 dst_port;
    __u8  family;
    __u8  _pad[3];
};

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} loss_events SEC(".maps");

/* Per-CPU counter of events discarded before they reached userspace. */
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u64);
} loss_drops SEC(".maps");

SEC("tracepoint/tcp/tcp_retransmit_skb")
int handle_tcp_retransmit(struct trace_event_raw_tcp_event_sk_skb *ctx)
{
    /* IPv4 only for now. */
    __u16 family = BPF_CORE_READ(ctx, family);
    if (family != AF_INET)
        return 0;

    /* tcp_retransmit_skb fires for handshake packets too. Do not report
     * repeated SYN or SYN-ACK packets as established-flow packet loss. */
    int state = BPF_CORE_READ(ctx, state);
    if (state == TCP_SYN_SENT || state == TCP_SYN_RECV ||
        state == TCP_NEW_SYN_RECV)
        return 0;

    struct tcploss_event *evt = bpf_ringbuf_reserve(&loss_events, sizeof(*evt), 0);
    if (!evt) {
        __u32 key = 0;
        __u64 *drops = bpf_map_lookup_elem(&loss_drops, &key);
        if (drops)
            (*drops)++;
        return 0;
    }

    __builtin_memset(evt, 0, sizeof(*evt));
    evt->timestamp_ns = bpf_ktime_get_ns();
    evt->family = (__u8)family;

    /* saddr/daddr are __u8[4] in network byte order. Copy to IPv4-mapped IPv6
     * format so userspace parses src_ip/dst_ip the same way as conntrack. */
    __u8 saddr[4], daddr[4];
    bpf_core_read(&saddr, sizeof(saddr), &ctx->saddr);
    bpf_core_read(&daddr, sizeof(daddr), &ctx->daddr);

    evt->src_ip[10] = 0xff;
    evt->src_ip[11] = 0xff;
    evt->dst_ip[10] = 0xff;
    evt->dst_ip[11] = 0xff;

    evt->src_ip[12] = saddr[0];
    evt->src_ip[13] = saddr[1];
    evt->src_ip[14] = saddr[2];
    evt->src_ip[15] = saddr[3];

    evt->dst_ip[12] = daddr[0];
    evt->dst_ip[13] = daddr[1];
    evt->dst_ip[14] = daddr[2];
    evt->dst_ip[15] = daddr[3];

    /* Ports are already host byte order in this tracepoint. */
    evt->src_port = BPF_CORE_READ(ctx, sport);
    evt->dst_port = BPF_CORE_READ(ctx, dport);

    bpf_ringbuf_submit(evt, 0);
    return 0;
}

char LICENSE[] SEC("license") = "Dual BSD/GPL";
