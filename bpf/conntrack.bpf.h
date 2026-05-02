/* SPDX-License-Identifier: GPL-2.0 OR BSD-3-Clause */
/* Copyright (c) 2024 Network Monitor Contributors */

#ifndef __CONNTRACK_H
#define __CONNTRACK_H

/* Direction constants */
#define DIR_INCOMING    0
#define DIR_OUTGOING    1
#define DIR_UNKNOWN     2  /* Used when connection was not tracked from start */

/* Protocol constants */
#define IPPROTO_TCP     6
#define IPPROTO_UDP     17
#define IPPROTO_ICMP    1

/* TCP flags - defined in vmlinux.h for eBPF */
/* #define TCP_FIN    0x01 */
/* #define TCP_SYN    0x02 */
/* #define TCP_RST    0x04 */
/* #define TCP_PSH    0x08 */
/* #define TCP_ACK    0x10 */
/* #define TCP_URG    0x20 */

/* Connection states */
#define CONN_STATE_NEW          0  /* Just created */
#define CONN_STATE_SYN_SENT     1  /* SYN sent (outgoing) */
#define CONN_STATE_SYN_RECEIVED 2  /* SYN received (incoming) */
#define CONN_STATE_ESTABLISHED  3  /* Connection established */
#define CONN_STATE_CLOSING      4  /* Connection closing (FIN sent) */
#define CONN_STATE_CLOSED       5  /* Connection closed */

/* Event types */
#define CONN_EVENT_NEW          0
#define CONN_EVENT_ESTABLISHED  1
#define CONN_EVENT_CLOSED       2
#define CONN_EVENT_FAILED       3  /* SYN without SYN+ACK timeout */
#define CONN_EVENT_REJECTED     4  /* Incoming connection rejected */

/* Max connections to track */
#define MAX_CONNECTIONS         10240

/* Task command length */
#define TASK_COMM_LEN           16

#endif /* __CONNTRACK_H */
