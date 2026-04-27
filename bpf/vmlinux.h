/* SPDX-License-Identifier: GPL-2.0 OR BSD-3 */
/* Minimal vmlinux.h for conntrack eBPF program */
/* This file provides kernel type definitions for eBPF CO-RE */

#ifndef __VMLINUX_H__
#define __VMLINUX_H__

/* Basic types */
typedef unsigned char __u8;
typedef unsigned short __u16;
typedef unsigned int __u32;
typedef unsigned long long __u64;
typedef signed char __s8;
typedef signed short __s16;
typedef signed int __s32;
typedef signed long long __s64;
typedef long __kernel_long_t;
typedef __kernel_long_t pid_t;

/* Kernel types */
typedef __u32 __wsum;
typedef __u16 __sum16;

/* Socket address family */
#define AF_INET 2
#define AF_INET6 10

/* IP protocols */
#define IPPROTO_IP 0
#define IPPROTO_ICMP 1
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17
#define IPPROTO_IPV6 41

/* TCP states */
#define TCP_ESTABLISHED 1
#define TCP_SYN_SENT 2
#define TCP_SYN_RECV 3
#define TCP_FIN_WAIT1 4
#define TCP_FIN_WAIT2 5
#define TCP_TIME_WAIT 6
#define TCP_CLOSE 7
#define TCP_CLOSE_WAIT 8
#define TCP_LAST_ACK 9
#define TCP_LISTEN 10
#define TCP_CLOSING 11
#define TCP_NEW_SYN_RECV 12
#define TCP_MAX_STATES 13

/* TCP flags */
#define TCPH_FIN_SHIFT 0
#define TCPH_SYN_SHIFT 1
#define TCPH_RST_SHIFT 2
#define TCPH_PSH_SHIFT 3
#define TCPH_ACK_SHIFT 4
#define TCPH_URG_SHIFT 5
#define TCPH_ECE_SHIFT 6
#define TCPH_CWR_SHIFT 7

#define TCP_FLAG_FIN (1 << TCPH_FIN_SHIFT)
#define TCP_FLAG_SYN (1 << TCPH_SYN_SHIFT)
#define TCP_FLAG_RST (1 << TCPH_RST_SHIFT)
#define TCP_FLAG_PSH (1 << TCPH_PSH_SHIFT)
#define TCP_FLAG_ACK (1 << TCPH_ACK_SHIFT)
#define TCP_FLAG_URG (1 << TCPH_URG_SHIFT)
#define TCP_FLAG_ECE (1 << TCPH_ECE_SHIFT)
#define TCP_FLAG_CWR (1 << TCPH_CWR_SHIFT)

/* Socket types */
#define SOCK_STREAM 1
#define SOCK_DGRAM 2
#define SOCK_RAW 3

/* Error codes */
#define EINVAL 22
#define ENOMEM 12
#define EFAULT 14

/* Max constants */
#define TASK_COMM_LEN 16
#define IFNAMSIZ 16

/* Inode types */
typedef unsigned long ino_t;
typedef long loff_t;

/* IPv6 address - must be defined before use */
struct in6_addr {
    union {
        __u8 u6_addr8[16];
        __u16 u6_addr16[8];
        __u32 u6_addr32[4];
    } in6_u;
};

/* Network device */
struct net_device {
    char name[IFNAMSIZ];
    unsigned int ifindex;
};

/* Socket common */
struct sock_common {
    __u32 skc_daddr;
    __u32 skc_rcv_saddr;
    __u16 skc_dport;
    __u16 skc_num;
    __u16 skc_family;
};

/* Socket */
struct sock {
    struct sock_common __sk_common;
    __u16 sk_gso_max_segs;
    __u8 sk_state;
};

/* TCP header */
struct tcphdr {
    __u16 source;
    __u16 dest;
    __u32 seq;
    __u32 ack_seq;
    __u16 res1:4;
    __u16 doff:4;
    __u16 fin:1;
    __u16 syn:1;
    __u16 rst:1;
    __u16 psh:1;
    __u16 ack:1;
    __u16 urg:1;
    __u16 ece:1;
    __u16 cwr:1;
    __u16 window;
    __sum16 check;
    __u32 urg_ptr;
};

/* IPv4 header */
struct iphdr {
    __u8 ihl:4;
    __u8 version:4;
    __u8 tos;
    __u16 tot_len;
    __u16 id;
    __u16 frag_off;
    __u8 ttl;
    __u8 protocol;
    __sum16 check;
    __u32 saddr;
    __u32 daddr;
};

/* IPv6 header */
struct ipv6hdr {
    __u8 priority:4;
    __u8 version:4;
    __u8 flow_lbl[3];
    __u16 payload_len;
    __u8 nexthdr;
    __u8 hop_limit;
    struct in6_addr saddr;
    struct in6_addr daddr;
};

/* Forward declarations */
struct sk_buff;
struct sock;
struct tcphdr;
struct iphdr;
struct ipv6hdr;
struct task_struct;

/* Socket buffer */
struct sk_buff {
    union {
        __u32 mark;
        __u32 drop_reason;
    };
    unsigned char *head;
    unsigned char *data;
    __u32 len;
    __u32 data_len;
    __u16 mac_len;
    __u16 hdr_len;
    __u16 queue_mapping;
    __u8 pkt_type;
    __u8 ip_summed;
    struct sk_buff *next;
    struct sk_buff *prev;
    struct net_device *dev;
};

/* Task struct */
struct task_struct {
    pid_t pid;
    pid_t tgid;
    char comm[TASK_COMM_LEN];
};

/* Trace event common */
struct trace_entry {
    unsigned short type;
    unsigned char flags;
    unsigned char preempt_count;
    pid_t pid;
    pid_t tid;
};

/* Trace event raw */
struct trace_event_raw_inet_sock_set_state {
    struct trace_entry ent;
    __u32 oldstate;
    __u32 newstate;
    __u32 sport;
    __u32 dport;
    __u32 family;
    __u32 protocol;
    __u32 saddr;
    __u32 daddr;
    __u32 saddr_v6[4];
    __u32 daddr_v6[4];
};

/* BPF helpers - declared as external */
extern void *bpf_map_lookup_elem(void *map, const void *key) __attribute__((weak));
extern int bpf_map_update_elem(void *map, const void *key, const void *value, __u64 flags) __attribute__((weak));
extern int bpf_map_delete_elem(void *map, const void *key) __attribute__((weak));
extern __u64 bpf_ktime_get_ns(void) __attribute__((weak));
extern __u64 bpf_get_current_pid_tgid(void) __attribute__((weak));
extern long bpf_get_current_comm(void *buf, __u32 size) __attribute__((weak));
extern __u64 bpf_get_current_task(void) __attribute__((weak));
extern int bpf_probe_read_kernel(void *dst, __u32 size, const void *unsafe_ptr) __attribute__((weak));
extern int bpf_probe_read_user(void *dst, __u32 size, const void *unsafe_ptr) __attribute__((weak));
extern __u32 bpf_ntohl(__u32 x) __attribute__((weak));
extern __u16 bpf_ntohs(__u16 x) __attribute__((weak));

#endif /* __VMLINUX_H__ */
