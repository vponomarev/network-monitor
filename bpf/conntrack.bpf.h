/* SPDX-License-Identifier: GPL-2.0 OR BSD-3-Clause */
/* Copyright (c) 2024 Network Monitor Contributors */

#ifndef __CONNTRACK_H
#define __CONNTRACK_H

// Direction constants
#define DIR_INCOMING  0
#define DIR_OUTGOING  1

// Protocol constants
#define IPPROTO_TCP  6
#define IPPROTO_UDP  17
#define IPPROTO_ICMP 1

// Connection states
#define CONN_STATE_NEW      0
#define CONN_STATE_ESTABLISHED 1
#define CONN_STATE_CLOSING  2
#define CONN_STATE_CLOSED   3

#endif /* __CONNTRACK_H */
