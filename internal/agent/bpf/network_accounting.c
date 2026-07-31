// SPDX-License-Identifier: GPL-2.0
// Metadata-only socket accounting. No payload byte is ever read.
#define BPF_NO_PRESERVE_ACCESS_INDEX
#include "vmlinux.h"
#include <bpf/bpf_endian.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

/*
 * AF_INET/AF_INET6 are userspace header macros and are not guaranteed to be
 * emitted into a kernel-generated vmlinux.h. Keep the Linux UAPI values local
 * instead of mixing Linux headers with CO-RE types from vmlinux.h.
 */
#ifndef AF_INET
#define AF_INET 2
#endif
#ifndef AF_INET6
#define AF_INET6 10
#endif

enum direction {
	DIRECTION_RX = 1,
	DIRECTION_TX = 2,
};

struct inflight {
	struct sock *sk;
	__u64 requested_bytes;
	__u32 pid;
	__u32 direction;
	__u32 protocol;
};

struct flow_key {
	__u32 pid;
	__u32 family;
	__u32 protocol;
	__u32 remote_port;
	__u8 remote_addr[16];
	char comm[16];
};

struct flow_value {
	__u64 rx_bytes;
	__u64 tx_bytes;
};

struct accounting_stats {
	__u64 dropped_events;
	__u64 dropped_bytes;
};

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 32768);
	__type(key, __u64);
	__type(value, struct inflight);
} calls SEC(".maps");

struct {
	/*
	 * Never silently evict an older, not-yet-drained counter. If capacity is
	 * exhausted, the accounting_stats map makes the loss explicit.
	 */
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 262144);
	__type(key, struct flow_key);
	__type(value, struct flow_value);
} flows SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, struct accounting_stats);
} accounting_stats SEC(".maps");

static __always_inline void record_drop(__u64 bytes)
{
	__u32 key = 0;
	struct accounting_stats *stats = bpf_map_lookup_elem(&accounting_stats, &key);
	if (stats) {
		stats->dropped_events++;
		stats->dropped_bytes += bytes;
	}
}

static __always_inline int remember_socket(struct sock *sk, __u64 requested_bytes,
					   __u32 direction, __u32 protocol)
{
	__u64 id = bpf_get_current_pid_tgid();
	struct inflight value = {
		.sk = sk,
		.requested_bytes = requested_bytes,
		.pid = id >> 32,
		.direction = direction,
		.protocol = protocol,
	};
	bpf_map_update_elem(&calls, &id, &value, BPF_ANY);
	return 0;
}

static __always_inline int account_return(struct pt_regs *ctx)
{
	__s64 transferred = PT_REGS_RC(ctx);
	__u64 id = bpf_get_current_pid_tgid();
	struct inflight *call = bpf_map_lookup_elem(&calls, &id);
	if (!call)
		return 0;
	if (transferred <= 0) {
		bpf_map_delete_elem(&calls, &id);
		return 0;
	}
	if ((__u64)transferred > call->requested_bytes) {
		/*
		 * A return larger than the requested buffer is an invalid probe sample,
		 * not a flow-map capacity failure. Discard it instead of presenting an
		 * untrusted ABI/signature value as lost network traffic.
		 */
		bpf_map_delete_elem(&calls, &id);
		return 0;
	}

	struct flow_key key = {
		.pid = call->pid,
		.protocol = call->protocol,
	};
	struct sock *sk = call->sk;
	__u16 family = 0;
	__be16 destination_port = 0;
	bpf_probe_read_kernel(&family, sizeof(family), &sk->__sk_common.skc_family);
	bpf_probe_read_kernel(&destination_port, sizeof(destination_port), &sk->__sk_common.skc_dport);
	key.family = family;
	key.remote_port = bpf_ntohs(destination_port);
	bpf_get_current_comm(&key.comm, sizeof(key.comm));
	if (key.family == AF_INET) {
		__u32 address = 0;
		bpf_probe_read_kernel(&address, sizeof(address), &sk->__sk_common.skc_daddr);
		__builtin_memcpy(key.remote_addr, &address, sizeof(address));
	} else if (key.family == AF_INET6) {
		bpf_probe_read_kernel(&key.remote_addr, sizeof(key.remote_addr),
				      &sk->__sk_common.skc_v6_daddr.in6_u.u6_addr8);
	}

	struct flow_value zero = {};
	struct flow_value *value = bpf_map_lookup_elem(&flows, &key);
	if (!value) {
		bpf_map_update_elem(&flows, &key, &zero, BPF_NOEXIST);
		value = bpf_map_lookup_elem(&flows, &key);
		if (!value) {
			record_drop(transferred);
			bpf_map_delete_elem(&calls, &id);
			return 0;
		}
	}
	if (value) {
		if (call->direction == DIRECTION_RX)
			__sync_fetch_and_add(&value->rx_bytes, transferred);
		else
			__sync_fetch_and_add(&value->tx_bytes, transferred);
	} else
		record_drop(transferred);
	bpf_map_delete_elem(&calls, &id);
	return 0;
}

SEC("kprobe/tcp_sendmsg")
int BPF_KPROBE(enter_tcp_sendmsg, struct sock *sk, struct msghdr *msg, size_t size)
{
	return remember_socket(sk, size, DIRECTION_TX, IPPROTO_TCP);
}

SEC("kretprobe/tcp_sendmsg")
int BPF_KRETPROBE(exit_tcp_sendmsg)
{
	return account_return(ctx);
}

SEC("kprobe/tcp_recvmsg")
int BPF_KPROBE(enter_tcp_recvmsg, struct sock *sk, struct msghdr *msg, size_t len)
{
	return remember_socket(sk, len, DIRECTION_RX, IPPROTO_TCP);
}

SEC("kretprobe/tcp_recvmsg")
int BPF_KRETPROBE(exit_tcp_recvmsg)
{
	return account_return(ctx);
}

SEC("kprobe/udp_sendmsg")
int BPF_KPROBE(enter_udp_sendmsg, struct sock *sk, struct msghdr *msg, size_t len)
{
	return remember_socket(sk, len, DIRECTION_TX, IPPROTO_UDP);
}

SEC("kretprobe/udp_sendmsg")
int BPF_KRETPROBE(exit_udp_sendmsg)
{
	return account_return(ctx);
}

SEC("kprobe/udp_recvmsg")
int BPF_KPROBE(enter_udp_recvmsg, struct sock *sk, struct msghdr *msg, size_t len)
{
	return remember_socket(sk, len, DIRECTION_RX, IPPROTO_UDP);
}

SEC("kretprobe/udp_recvmsg")
int BPF_KRETPROBE(exit_udp_recvmsg)
{
	return account_return(ctx);
}

char LICENSE[] SEC("license") = "GPL";
