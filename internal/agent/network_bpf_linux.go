//go:build linux

package agent

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"

	"github.com/WhileEndless/Servora/internal/model"
)

type bpfFlowKey struct {
	PID        uint32
	Family     uint32
	Protocol   uint32
	RemotePort uint32
	RemoteAddr [16]byte
	Command    [16]byte
}

type bpfFlowValue struct {
	RXBytes uint64
	TXBytes uint64
}

type bpfAccountingStats struct {
	DroppedEvents uint64
	DroppedBytes  uint64
}

type bpfNetworkTracker struct {
	mu         sync.Mutex
	collection *ebpf.Collection
	flows      *ebpf.Map
	calls      *ebpf.Map
	stats      *ebpf.Map
	links      []link.Link
}

func newBPFNetworkTracker(objectPath string) (*bpfNetworkTracker, error) {
	if _, err := os.Stat(objectPath); err != nil {
		return nil, fmt.Errorf("eBPF object unavailable: %w", err)
	}
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("raise BPF memory limit: %w", err)
	}
	spec, err := ebpf.LoadCollectionSpec(objectPath)
	if err != nil {
		return nil, fmt.Errorf("read eBPF object: %w", err)
	}
	collection, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, fmt.Errorf("load eBPF collection: %w", err)
	}
	tracker := &bpfNetworkTracker{
		collection: collection,
		flows:      collection.Maps["flows"],
		calls:      collection.Maps["calls"],
		stats:      collection.Maps["accounting_stats"],
	}
	if tracker.flows == nil || tracker.calls == nil || tracker.stats == nil {
		collection.Close()
		return nil, errors.New("eBPF accounting maps are missing")
	}
	attachments := []struct {
		symbol  string
		program string
		ret     bool
	}{
		{"tcp_sendmsg", "enter_tcp_sendmsg", false}, {"tcp_sendmsg", "exit_tcp_sendmsg", true},
		{"tcp_recvmsg", "enter_tcp_recvmsg", false}, {"tcp_recvmsg", "exit_tcp_recvmsg", true},
		{"udp_sendmsg", "enter_udp_sendmsg", false}, {"udp_sendmsg", "exit_udp_sendmsg", true},
		{"udp_recvmsg", "enter_udp_recvmsg", false}, {"udp_recvmsg", "exit_udp_recvmsg", true},
	}
	for _, attachment := range attachments {
		program := collection.Programs[attachment.program]
		if program == nil {
			tracker.Close()
			return nil, fmt.Errorf("eBPF program %s is missing", attachment.program)
		}
		var attached link.Link
		if attachment.ret {
			attached, err = link.Kretprobe(attachment.symbol, program, nil)
		} else {
			attached, err = link.Kprobe(attachment.symbol, program, nil)
		}
		if err != nil {
			tracker.Close()
			return nil, fmt.Errorf("attach %s: %w", attachment.program, err)
		}
		tracker.links = append(tracker.links, attached)
	}
	return tracker, nil
}

func (tracker *bpfNetworkTracker) Close() {
	for _, attached := range tracker.links {
		_ = attached.Close()
	}
	if tracker.collection != nil {
		tracker.collection.Close()
	}
}

func (tracker *bpfNetworkTracker) DroppedBytes() uint64 {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	var perCPU []bpfAccountingStats
	if err := tracker.stats.Lookup(uint32(0), &perCPU); err != nil {
		return 0
	}
	var total uint64
	for _, stats := range perCPU {
		total += stats.DroppedBytes
	}
	return total
}

// Reset clears the integrity counter together with pending and completed flow
// maps. It is called by the authenticated history-clear operation so a new
// accounting epoch cannot inherit kernel-side totals from the previous one.
func (tracker *bpfNetworkTracker) Reset() error {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if err := clearBPFMap(tracker.flows); err != nil {
		return fmt.Errorf("clear flow map: %w", err)
	}
	if err := clearBPFMap(tracker.calls); err != nil {
		return fmt.Errorf("clear pending-call map: %w", err)
	}
	possibleCPUs, err := ebpf.PossibleCPU()
	if err != nil {
		return fmt.Errorf("read possible CPUs: %w", err)
	}
	zero := make([]bpfAccountingStats, possibleCPUs)
	if err := tracker.stats.Update(uint32(0), zero, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("clear accounting statistics: %w", err)
	}
	return nil
}

func clearBPFMap(target *ebpf.Map) error {
	iterator := target.Iterate()
	key := make([]byte, target.KeySize())
	value := make([]byte, target.ValueSize())
	for iterator.Next(&key, &value) {
		if err := target.Delete(key); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
			return err
		}
	}
	return iterator.Err()
}

func (tracker *bpfNetworkTracker) Drain(processes []model.Process) []model.NetworkFlow {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	processByPID := make(map[int]model.Process, len(processes))
	for _, process := range processes {
		processByPID[process.PID] = process
	}
	iterator := tracker.flows.Iterate()
	var key bpfFlowKey
	var ignored bpfFlowValue
	var result []model.NetworkFlow
	now := time.Now()
	for iterator.Next(&key, &ignored) {
		var value bpfFlowValue
		if err := tracker.flows.LookupAndDelete(&key, &value); err != nil {
			continue
		}
		if value.RXBytes == 0 && value.TXBytes == 0 {
			continue
		}
		pid := int(key.PID)
		process := processByPID[pid]
		name := process.Name
		if name == "" {
			name = strings.TrimRight(string(key.Command[:]), "\x00")
		}
		if name == "" {
			name = "unknown"
		}
		remote := "unknown"
		if key.Family == 2 {
			remote = net.IP(key.RemoteAddr[:4]).String()
		} else if key.Family == 10 {
			remote = net.IP(key.RemoteAddr[:]).String()
		}
		protocol := "tcp"
		if key.Protocol == 17 {
			protocol = "udp"
		}
		result = append(result, model.NetworkFlow{
			Timestamp: now, PID: pid, Process: name, Group: networkProcessGroup(name),
			User: process.User, Protocol: protocol, RemoteIP: remote,
			RemotePort: int(key.RemotePort), RXBytes: value.RXBytes, TXBytes: value.TXBytes,
		})
	}
	return result
}
