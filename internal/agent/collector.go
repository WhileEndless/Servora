package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/WhileEndless/Servora/internal/model"
)

type Collector struct {
	mu                sync.Mutex
	lastCPUIdle       uint64
	lastCPUTotal      uint64
	lastNet           map[string][2]uint64
	lastProc          map[string]float64
	lastAt            time.Time
	clockTicks        float64
	bootTime          time.Time
	services          []model.Service
	sshSessions       []model.SSHSession
	timers            []model.Timer
	containers        []model.Container
	dockerSummary     model.DockerSummary
	connections       []model.Connection
	networkFlows      []model.NetworkFlow
	socketCounters    map[string][2]uint64
	processes         []model.Process
	lastSystemd       time.Time
	lastDocker        time.Time
	lastNetwork       time.Time
	dockerReady       bool
	systemdRefreshing bool
	dockerRefreshing  bool
	networkRefreshing bool
	dockerError       string
	serviceAllowlist  map[string]bool
	protectedServices map[string]bool
	bpfTracker        *bpfNetworkTracker
	networkMode       string
}

func NewCollector(serviceAllowlist, protectedServices map[string]bool) *Collector {
	return &Collector{
		lastNet: map[string][2]uint64{}, lastProc: map[string]float64{},
		socketCounters: map[string][2]uint64{}, networkMode: "socket-counter-fallback",
		clockTicks: 100, bootTime: detectBootTime(),
		serviceAllowlist: serviceAllowlist, protectedServices: protectedServices,
	}
}

func (c *Collector) EnableBPF(objectPath string) error {
	tracker, err := newBPFNetworkTracker(objectPath)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.bpfTracker = tracker
	c.networkMode = "ebpf-exact"
	c.mu.Unlock()
	return nil
}

func (c *Collector) Close() {
	c.mu.Lock()
	tracker := c.bpfTracker
	c.bpfTracker = nil
	c.mu.Unlock()
	if tracker != nil {
		tracker.Close()
	}
}

func (c *Collector) Snapshot(ctx context.Context) model.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	host, _ := os.Hostname()
	s := model.Snapshot{
		Timestamp: now, Hostname: host, Kernel: kernelRelease(),
		Capabilities: map[string]bool{
			"systemd":         commandExists("systemctl"),
			"docker":          c.dockerReady,
			"process_network": commandExists("ss"),
			"sensors":         len(sensorTemperatures()) > 0,
		},
		Freshness: map[string]time.Time{},
	}
	s.Uptime = readUptime()
	s.CPU = c.readCPU()
	s.Memory = readMemory()
	s.Disks = readDisks()
	s.Network = c.readNetwork(now)
	s.Processes = c.readProcesses(c.bootTime, c.clockTicks, s.Uptime, now)
	c.processes = s.Processes

	if !c.networkRefreshing && (c.lastNetwork.IsZero() || now.Sub(c.lastNetwork) >= time.Second) {
		c.networkRefreshing = true
		processes := append([]model.Process(nil), s.Processes...)
		go c.refreshConnections(processes)
	}
	if !c.systemdRefreshing && (c.lastSystemd.IsZero() || now.Sub(c.lastSystemd) >= 15*time.Second) {
		c.systemdRefreshing = true
		go c.refreshSystemd()
	}
	if !c.dockerRefreshing && (c.lastDocker.IsZero() || now.Sub(c.lastDocker) >= 5*time.Second) {
		c.dockerRefreshing = true
		go c.refreshDocker()
	}
	s.Services = c.services
	s.SSHSessions = c.sshSessions
	s.Timers = c.timers
	s.Containers = c.containers
	s.Docker = c.dockerSummary
	s.Connections = c.connections
	s.NetworkFlows = c.networkFlows
	s.NetworkMode = c.networkMode
	if c.bpfTracker != nil {
		s.NetworkDrops = c.bpfTracker.DroppedBytes()
	}
	s.Capabilities["docker"] = c.dockerReady
	if c.dockerError != "" {
		s.Errors = append(s.Errors, "Docker: "+c.dockerError)
	}
	s.Freshness["fast"] = now
	s.Freshness["connections"] = c.lastNetwork
	s.Freshness["systemd"] = c.lastSystemd
	s.Freshness["docker"] = c.lastDocker
	c.lastAt = now
	return s
}

func (c *Collector) refreshConnections(processes []model.Process) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connections := readConnections(ctx, processes)
	c.mu.Lock()
	tracker := c.bpfTracker
	c.mu.Unlock()
	var flows []model.NetworkFlow
	counters := c.socketCounterSnapshot()
	if tracker != nil {
		flows = tracker.Drain(processes)
	} else {
		flows, counters = readNetworkFlows(ctx, processes, counters)
	}
	c.mu.Lock()
	c.connections = connections
	c.networkFlows = flows
	c.socketCounters = counters
	c.lastNetwork = time.Now()
	c.networkRefreshing = false
	c.mu.Unlock()
}

func (c *Collector) socketCounterSnapshot() map[string][2]uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string][2]uint64, len(c.socketCounters))
	for key, value := range c.socketCounters {
		result[key] = value
	}
	return result
}

func (c *Collector) refreshSystemd() {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	var services []model.Service
	var sessions []model.SSHSession
	var timers []model.Timer
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); services = readServices(ctx) }()
	go func() { defer wg.Done(); sessions = readSSHSessions(ctx) }()
	go func() { defer wg.Done(); timers = readTimers(ctx) }()
	wg.Wait()
	for index := range services {
		services[index].Manageable = c.serviceAllowlist[services[index].Name]
		services[index].Protected = c.protectedServices[services[index].Name]
	}
	c.mu.Lock()
	c.services, c.sshSessions, c.timers = services, sessions, timers
	c.lastSystemd = time.Now()
	c.systemdRefreshing = false
	c.mu.Unlock()
}

func (c *Collector) refreshDocker() {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	containers, summary, err := readContainers(ctx)
	c.mu.Lock()
	c.dockerReady = err == nil
	if err != nil {
		c.dockerError = err.Error()
	} else {
		c.dockerError = ""
		c.containers = containers
		c.dockerSummary = summary
	}
	c.lastDocker = time.Now()
	c.dockerRefreshing = false
	c.mu.Unlock()
}

func (c *Collector) ProcessDetail(pid int) (model.ProcessDetail, error) {
	c.mu.Lock()
	var process *model.Process
	for index := range c.processes {
		if c.processes[index].PID == pid {
			copy := c.processes[index]
			process = &copy
			break
		}
	}
	var connections []model.Connection
	for _, connection := range c.connections {
		if connection.PID == pid {
			connections = append(connections, connection)
		}
	}
	c.mu.Unlock()
	if process == nil {
		return model.ProcessDetail{}, errors.New("process no longer exists")
	}
	base := filepath.Join("/proc", strconv.Itoa(pid))
	detail := model.ProcessDetail{
		Process:     *process,
		Status:      readKeyValueFile(filepath.Join(base, "status")),
		Limits:      readLimits(filepath.Join(base, "limits")),
		Namespaces:  map[string]string{},
		Connections: connections,
	}
	detail.Executable, _ = os.Readlink(filepath.Join(base, "exe"))
	detail.WorkingDir, _ = os.Readlink(filepath.Join(base, "cwd"))
	if raw, err := os.ReadFile(filepath.Join(base, "cgroup")); err == nil {
		detail.Cgroup = strings.TrimSpace(string(raw))
	}
	children, _ := filepath.Glob(filepath.Join(base, "task", "*", "children"))
	seenChildren := map[int]bool{}
	for _, path := range children {
		raw, _ := os.ReadFile(path)
		for _, field := range strings.Fields(string(raw)) {
			child, _ := strconv.Atoi(field)
			if child > 0 && !seenChildren[child] {
				seenChildren[child] = true
				detail.Children = append(detail.Children, child)
			}
		}
	}
	fdPaths, _ := filepath.Glob(filepath.Join(base, "fd", "*"))
	detail.OpenFDs = len(fdPaths)
	for _, path := range fdPaths {
		target, err := os.Readlink(path)
		if err == nil && !strings.HasPrefix(target, "socket:") && !strings.HasPrefix(target, "pipe:") {
			detail.OpenFiles = append(detail.OpenFiles, target)
			if len(detail.OpenFiles) >= 100 {
				break
			}
		}
	}
	namespacePaths, _ := filepath.Glob(filepath.Join(base, "ns", "*"))
	for _, path := range namespacePaths {
		target, err := os.Readlink(path)
		if err == nil {
			detail.Namespaces[filepath.Base(path)] = target
		}
	}
	return detail, nil
}

func (c *Collector) readCPU() model.CPU {
	cpu := model.CPU{Cores: runtime.NumCPU()}
	data, err := os.ReadFile("/proc/stat")
	if err == nil {
		fields := strings.Fields(strings.SplitN(string(data), "\n", 2)[0])
		var values []uint64
		for _, v := range fields[1:] {
			n, _ := strconv.ParseUint(v, 10, 64)
			values = append(values, n)
		}
		var total uint64
		for _, n := range values {
			total += n
		}
		idle := uint64(0)
		if len(values) > 3 {
			idle = values[3]
		}
		if len(values) > 4 {
			idle += values[4]
		}
		if c.lastCPUTotal > 0 && total > c.lastCPUTotal {
			delta := total - c.lastCPUTotal
			idleDelta := idle - c.lastCPUIdle
			cpu.Usage = 100 * float64(delta-idleDelta) / float64(delta)
		}
		c.lastCPUTotal, c.lastCPUIdle = total, idle
	}
	if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(raw))
		for i := 0; i < 3 && i < len(f); i++ {
			cpu.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}
	var freqTotal float64
	var freqCount int
	if raw, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "cpu MHz") {
				_, value, _ := strings.Cut(line, ":")
				n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				if err == nil {
					freqTotal += n
					freqCount++
				}
			}
		}
	}
	if freqCount > 0 {
		cpu.Frequency = freqTotal / float64(freqCount)
	}
	return cpu
}

func readMemory() model.Memory {
	values := map[string]uint64{}
	raw, _ := os.ReadFile("/proc/meminfo")
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			n, _ := strconv.ParseUint(fields[1], 10, 64)
			values[strings.TrimSuffix(fields[0], ":")] = n * 1024
		}
	}
	total, available := values["MemTotal"], values["MemAvailable"]
	return model.Memory{
		Total: total, Available: available, Used: total - available,
		SwapTotal: values["SwapTotal"], SwapUsed: values["SwapTotal"] - values["SwapFree"],
	}
}

func readDisks() []model.Disk {
	raw, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []model.Disk
	virtual := map[string]bool{"proc": true, "sysfs": true, "tmpfs": true, "devtmpfs": true, "devpts": true, "cgroup": true, "cgroup2": true, "overlay": false}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || virtual[f[2]] || seen[f[1]] {
			continue
		}
		seen[f[1]] = true
		var st syscall.Statfs_t
		if syscall.Statfs(f[1], &st) != nil {
			continue
		}
		total := st.Blocks * uint64(st.Bsize)
		available := st.Bavail * uint64(st.Bsize)
		used := total - st.Bfree*uint64(st.Bsize)
		percent := float64(0)
		if total > 0 {
			percent = 100 * float64(used) / float64(total)
		}
		out = append(out, model.Disk{
			Device: f[0], Mount: strings.ReplaceAll(f[1], `\040`, " "), Filesystem: f[2],
			Total: total, Used: used, Available: available, UsedPercent: percent,
			InodesTotal: st.Files, InodesUsed: st.Files - st.Ffree,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mount < out[j].Mount })
	return out
}

func (c *Collector) readNetwork(now time.Time) []model.NetworkInterface {
	raw, _ := os.ReadFile("/proc/net/dev")
	var out []model.NetworkInterface
	elapsed := now.Sub(c.lastAt).Seconds()
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.Contains(line, ":") {
			continue
		}
		name, values, _ := strings.Cut(line, ":")
		f := strings.Fields(values)
		if len(f) < 16 {
			continue
		}
		parse := func(i int) uint64 { n, _ := strconv.ParseUint(f[i], 10, 64); return n }
		row := model.NetworkInterface{
			Name: strings.TrimSpace(name), RXBytes: parse(0), RXErrors: parse(2), RXDropped: parse(3),
			TXBytes: parse(8), TXErrors: parse(10), TXDropped: parse(11),
		}
		if prev, ok := c.lastNet[row.Name]; ok && elapsed > 0 {
			if row.RXBytes >= prev[0] {
				row.RXRate = uint64(float64(row.RXBytes-prev[0]) / elapsed)
			}
			if row.TXBytes >= prev[1] {
				row.TXRate = uint64(float64(row.TXBytes-prev[1]) / elapsed)
			}
		}
		c.lastNet[row.Name] = [2]uint64{row.RXBytes, row.TXBytes}
		out = append(out, row)
	}
	return out
}

func (c *Collector) readProcesses(boot time.Time, ticks, uptime float64, now time.Time) []model.Process {
	entries, _ := os.ReadDir("/proc")
	pageSize := uint64(os.Getpagesize())
	var out []model.Process
	nextTicks := map[string]float64{}
	sampleSeconds := now.Sub(c.lastAt).Seconds()
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		statRaw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		stat := string(statRaw)
		closeIdx := strings.LastIndex(stat, ")")
		openIdx := strings.Index(stat, "(")
		if openIdx < 0 || closeIdx < openIdx {
			continue
		}
		name := stat[openIdx+1 : closeIdx]
		f := strings.Fields(stat[closeIdx+2:])
		if len(f) < 22 {
			continue
		}
		ppid, _ := strconv.Atoi(f[1])
		utime, _ := strconv.ParseFloat(f[11], 64)
		stime, _ := strconv.ParseFloat(f[12], 64)
		threads, _ := strconv.Atoi(f[17])
		startTicks, _ := strconv.ParseFloat(f[19], 64)
		rssPages, _ := strconv.ParseUint(f[21], 10, 64)
		elapsed := uptime - startTicks/ticks
		cpu := float64(0)
		totalTicks := utime + stime
		processKey := fmt.Sprintf("%d:%.0f", pid, startTicks)
		if previous, ok := c.lastProc[processKey]; ok && sampleSeconds > 0 && totalTicks >= previous {
			cpu = 100 * ((totalTicks - previous) / ticks) / sampleSeconds
		} else if elapsed > 0 {
			cpu = 100 * ((utime + stime) / ticks) / elapsed
		}
		nextTicks[processKey] = totalTicks
		commandRaw, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		command := strings.TrimSpace(strings.ReplaceAll(string(commandRaw), "\x00", " "))
		if command == "" {
			command = "[" + name + "]"
		}
		var username string
		if info, err := os.Stat(filepath.Join("/proc", entry.Name())); err == nil {
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				username = strconv.Itoa(int(st.Uid))
				if u, err := user.LookupId(username); err == nil {
					username = u.Username
				}
			}
		}
		readB, writeB := readProcIO(pid)
		out = append(out, model.Process{
			PID: pid, PPID: ppid, Threads: threads, User: username, Name: name,
			Command: command, State: f[0], CPU: cpu, Memory: rssPages * pageSize,
			ReadBytes: readB, WriteBytes: writeB,
			StartTime: boot.Add(time.Duration(startTicks/ticks) * time.Second),
		})
	}
	c.lastProc = nextTicks
	sort.Slice(out, func(i, j int) bool { return out[i].CPU > out[j].CPU })
	return out
}

func readProcIO(pid int) (uint64, uint64) {
	raw, _ := os.ReadFile(fmt.Sprintf("/proc/%d/io", pid))
	var readB, writeB uint64
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "read_bytes:"):
			fmt.Sscanf(line, "read_bytes: %d", &readB)
		case strings.HasPrefix(line, "write_bytes:"):
			fmt.Sscanf(line, "write_bytes: %d", &writeB)
		}
	}
	return readB, writeB
}

func readServices(ctx context.Context) []model.Service {
	out, err := command(ctx, "systemctl", "list-units", "--type=service", "--all", "--no-legend", "--no-pager", "--plain")
	if err != nil {
		return nil
	}
	var services []model.Service
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		s := model.Service{Name: strings.TrimPrefix(f[0], "●"), Load: f[1], Active: f[2], Sub: f[3]}
		if len(f) > 4 {
			s.Description = strings.Join(f[4:], " ")
		}
		services = append(services, s)
	}
	details, _ := command(ctx, "systemctl", "show", "--type=service", "--all",
		"--property=Id,MainPID,NRestarts,MemoryCurrent,UnitFileState,ActiveEnterTimestampMonotonic")
	byName := map[string]*model.Service{}
	for index := range services {
		byName[services[index].Name] = &services[index]
	}
	for _, block := range strings.Split(details, "\n\n") {
		values := parseProperties(block)
		item := byName[values["Id"]]
		if item == nil {
			continue
		}
		item.PID, _ = strconv.Atoi(values["MainPID"])
		item.Restarts, _ = strconv.Atoi(values["NRestarts"])
		item.Memory, _ = strconv.ParseUint(values["MemoryCurrent"], 10, 64)
		item.UnitFile = values["UnitFileState"]
		activeUS, _ := strconv.ParseInt(values["ActiveEnterTimestampMonotonic"], 10, 64)
		if activeUS > 0 {
			activeAt := detectBootTime().Add(time.Duration(activeUS) * time.Microsecond)
			item.ActiveSince = activeAt.Format(time.RFC3339)
			item.Duration = time.Since(activeAt).Round(time.Second).String()
		}
	}
	return services
}

func readSSHSessions(ctx context.Context) []model.SSHSession {
	out, err := command(ctx, "loginctl", "list-sessions", "--no-legend")
	if err != nil {
		return nil
	}
	var sessions []model.SSHSession
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		id := f[0]
		props, _ := command(ctx, "loginctl", "show-session", id,
			"-p", "Name", "-p", "RemoteHost", "-p", "TTY", "-p", "Timestamp", "-p", "Leader", "-p", "Remote")
		p := parseProperties(props)
		if p["Remote"] != "yes" {
			continue
		}
		pid, _ := strconv.Atoi(p["Leader"])
		sessions = append(sessions, model.SSHSession{
			ID: id, User: p["Name"], Remote: p["RemoteHost"], TTY: p["TTY"], Since: p["Timestamp"], PID: pid,
		})
	}
	return sessions
}

func readTimers(ctx context.Context) []model.Timer {
	out, err := command(ctx, "systemctl", "list-timers", "--all", "--no-legend", "--no-pager")
	if err != nil {
		return nil
	}
	var timers []model.Timer
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		unit := f[len(f)-2]
		timers = append(timers, model.Timer{
			Next: strings.Join(f[0:4], " "), Left: f[4], Last: strings.Join(f[5:8], " "),
			Passed: f[8], Unit: unit, Activates: f[len(f)-1],
			Source:  "systemd",
			Managed: strings.HasPrefix(unit, "system-maintenance-job-"),
		})
	}
	timers = append(timers, readCronEntries()...)
	return timers
}

func readContainers(ctx context.Context) ([]model.Container, model.DockerSummary, error) {
	if !commandExists("docker") {
		return nil, model.DockerSummary{}, errors.New("docker command is not installed")
	}
	info, err := command(ctx, "docker", "info", "--format", `{{json .}}`)
	if err != nil {
		return nil, model.DockerSummary{}, fmt.Errorf("docker daemon is not reachable: %s", conciseCommandError(info))
	}
	var rawInfo struct {
		ServerVersion     string
		Driver            string
		Containers        int
		ContainersRunning int
		ContainersStopped int
		ContainersPaused  int
		Images            int
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(info)), &rawInfo); err != nil {
		return nil, model.DockerSummary{}, errors.New("docker daemon returned invalid host information")
	}
	summary := model.DockerSummary(rawInfo)
	out, err := command(ctx, "docker", "ps", "-a", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, model.DockerSummary{}, fmt.Errorf("container inventory failed: %s", conciseCommandError(out))
	}
	var containers []model.Container
	for _, line := range strings.Split(out, "\n") {
		var row map[string]string
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		containers = append(containers, model.Container{
			ID: row["ID"], Name: row["Names"], Image: row["Image"], Status: row["Status"],
			State: row["State"], Ports: row["Ports"], Created: row["CreatedAt"],
		})
	}
	stats, _ := command(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}")
	byName := map[string]*model.Container{}
	for index := range containers {
		byName[containers[index].Name] = &containers[index]
	}
	for _, line := range strings.Split(stats, "\n") {
		var row map[string]string
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		item := byName[row["Name"]]
		if item == nil {
			continue
		}
		item.CPU, _ = strconv.ParseFloat(strings.TrimSuffix(row["CPUPerc"], "%"), 64)
		item.Memory, item.MemoryLimit = parseBytePair(row["MemUsage"])
		item.NetRX, item.NetTX = parseBytePair(row["NetIO"])
		item.BlockRead, item.BlockWrite = parseBytePair(row["BlockIO"])
	}
	return containers, summary, nil
}

func readDockerImages(ctx context.Context) ([]model.DockerImage, time.Time, error) {
	if !commandExists("docker") {
		return nil, time.Time{}, errors.New("docker command is not installed")
	}
	idOutput, err := command(ctx, "docker", "image", "ls", "-q", "--no-trunc")
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("image inventory failed: %s", conciseCommandError(idOutput))
	}
	seen := map[string]bool{}
	var ids []string
	for _, id := range strings.Fields(idOutput) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return []model.DockerImage{}, time.Now(), nil
	}
	args := append([]string{"image", "inspect"}, ids...)
	out, err := command(ctx, "docker", args...)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("image inspect failed: %s", conciseCommandError(out))
	}
	var raw []struct {
		ID          string
		RepoTags    []string
		RepoDigests []string
		Created     time.Time
		Size        uint64
	}
	if strings.TrimSpace(out) != "" {
		if err := json.Unmarshal([]byte(out), &raw); err != nil {
			return nil, time.Time{}, errors.New("Docker returned invalid image data")
		}
	}
	containers, _, containerErr := readContainers(ctx)
	used := map[string][]string{}
	if containerErr == nil && len(containers) > 0 {
		ids := make([]string, 0, len(containers))
		for _, container := range containers {
			ids = append(ids, container.ID)
		}
		inspectArgs := append([]string{"inspect", "--format", "{{.Id}}\t{{.Image}}\t{{.Name}}"}, ids...)
		inspect, inspectErr := command(ctx, "docker", inspectArgs...)
		if inspectErr == nil {
			for _, line := range strings.Split(inspect, "\n") {
				fields := strings.SplitN(line, "\t", 3)
				if len(fields) == 3 {
					used[fields[1]] = append(used[fields[1]], strings.TrimPrefix(fields[2], "/"))
				}
			}
		}
	}
	items := make([]model.DockerImage, 0, len(raw))
	for _, image := range raw {
		references := image.RepoTags
		if references == nil {
			references = []string{}
		}
		digests := image.RepoDigests
		if digests == nil {
			digests = []string{}
		}
		containerNames := used[image.ID]
		if containerNames == nil {
			containerNames = []string{}
		}
		dangling := len(references) == 0
		items = append(items, model.DockerImage{
			ID: image.ID, References: references, RepoDigests: digests,
			CreatedAt: image.Created, SizeBytes: image.Size,
			ContainerNames: containerNames, Dangling: dangling,
		})
	}
	return items, time.Now(), nil
}

func conciseCommandError(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "command failed"
	}
	if len(output) > 240 {
		output = output[:240] + "…"
	}
	return output
}

func readConnections(ctx context.Context, processes []model.Process) []model.Connection {
	if !commandExists("ss") {
		return nil
	}
	out, err := command(ctx, "ss", "-H", "-tunap")
	if err != nil {
		return nil
	}
	var connections []model.Connection
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		row := model.Connection{Protocol: f[0], State: f[1], Local: f[4], Remote: f[5]}
		rest := strings.Join(f[6:], " ")
		if idx := strings.Index(rest, "pid="); idx >= 0 {
			fmt.Sscanf(rest[idx:], "pid=%d", &row.PID)
		}
		for _, p := range processes {
			if p.PID == row.PID {
				row.Process = p.Name
				break
			}
		}
		connections = append(connections, row)
	}
	return connections
}

var (
	socketPIDPattern      = regexp.MustCompile(`pid=(\d+)`)
	socketProcessPattern  = regexp.MustCompile(`users:\(\("([^"]+)"`)
	socketSentPattern     = regexp.MustCompile(`\bbytes_sent:(\d+)`)
	socketReceivedPattern = regexp.MustCompile(`\bbytes_received:(\d+)`)
)

func readNetworkFlows(ctx context.Context, processes []model.Process, previous map[string][2]uint64) ([]model.NetworkFlow, map[string][2]uint64) {
	out, err := command(ctx, "ss", "-H", "-tinp")
	if err != nil {
		return nil, previous
	}
	processByPID := make(map[int]model.Process, len(processes))
	for _, process := range processes {
		processByPID[process.PID] = process
	}
	lines := strings.Split(out, "\n")
	current := make(map[string][2]uint64)
	var flows []model.NetworkFlow
	now := time.Now()
	for index := 0; index+1 < len(lines); index++ {
		fields := strings.Fields(lines[index])
		if len(fields) < 5 || !strings.Contains(lines[index+1], "bytes_") {
			continue
		}
		local, remote := fields[3], fields[4]
		pid := matchInt(socketPIDPattern, lines[index])
		process := processByPID[pid]
		processName := process.Name
		if processName == "" {
			processName = matchString(socketProcessPattern, lines[index])
		}
		if processName == "" {
			processName = "unknown"
		}
		rx := uint64(matchInt64(socketReceivedPattern, lines[index+1]))
		tx := uint64(matchInt64(socketSentPattern, lines[index+1]))
		key := fmt.Sprintf("%d|%s|%s", pid, local, remote)
		current[key] = [2]uint64{rx, tx}
		old, exists := previous[key]
		if !exists {
			continue
		}
		rxDelta, txDelta := counterDelta(rx, old[0]), counterDelta(tx, old[1])
		if rxDelta == 0 && txDelta == 0 {
			continue
		}
		remoteIP, remotePort := splitEndpoint(remote)
		flows = append(flows, model.NetworkFlow{
			Timestamp: now, PID: pid, Process: processName, Group: networkProcessGroup(processName),
			User: process.User, Protocol: "tcp", Local: local, RemoteIP: remoteIP,
			RemotePort: remotePort, RXBytes: rxDelta, TXBytes: txDelta,
		})
	}
	return flows, current
}

func matchInt(pattern *regexp.Regexp, value string) int {
	matches := pattern.FindStringSubmatch(value)
	if len(matches) != 2 {
		return 0
	}
	result, _ := strconv.Atoi(matches[1])
	return result
}

func matchInt64(pattern *regexp.Regexp, value string) int64 {
	matches := pattern.FindStringSubmatch(value)
	if len(matches) != 2 {
		return 0
	}
	result, _ := strconv.ParseInt(matches[1], 10, 64)
	return result
}

func matchString(pattern *regexp.Regexp, value string) string {
	matches := pattern.FindStringSubmatch(value)
	if len(matches) != 2 {
		return ""
	}
	return matches[1]
}

func counterDelta(current, previous uint64) uint64 {
	if current < previous {
		return current
	}
	return current - previous
}

func splitEndpoint(value string) (string, int) {
	host, rawPort, err := net.SplitHostPort(value)
	if err != nil {
		return strings.Trim(value, "[]"), 0
	}
	port, _ := strconv.Atoi(rawPort)
	return strings.TrimPrefix(strings.TrimSuffix(host, "]"), "["), port
}

func networkProcessGroup(process string) string {
	name := strings.ToLower(process)
	switch {
	case strings.HasPrefix(name, "ssh"):
		return "ssh"
	case strings.HasPrefix(name, "system-maintenance"):
		return "system-maintenance"
	case strings.HasPrefix(name, "docker"), strings.HasPrefix(name, "containerd"):
		return "docker"
	case strings.HasPrefix(name, "postgres"):
		return "postgresql"
	case strings.HasPrefix(name, "mysql"), strings.HasPrefix(name, "mariadb"):
		return "mysql"
	default:
		return name
	}
}

func command(ctx context.Context, name string, args ...string) (string, error) {
	child, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(child, name, args...).CombinedOutput()
	return string(out), err
}

func readUptime() float64 {
	raw, _ := os.ReadFile("/proc/uptime")
	var value float64
	fmt.Sscanf(string(raw), "%f", &value)
	return value
}

func detectBootTime() time.Time { return time.Now().Add(-time.Duration(readUptime()) * time.Second) }
func kernelRelease() string {
	var u syscall.Utsname
	if syscall.Uname(&u) != nil {
		return runtime.GOOS
	}
	var b []byte
	for _, c := range u.Release {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}
func commandExists(name string) bool { _, err := exec.LookPath(name); return err == nil }
func sensorTemperatures() []float64 {
	var out []float64
	files, _ := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err == nil {
			v, _ := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
			out = append(out, v/1000)
		}
	}
	return out
}

func parseProperties(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}

func readKeyValueFile(path string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			out[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return out
}

func readLimits(path string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	lines := strings.Split(string(raw), "\n")
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		nameEnd := len(fields) - 2
		out[strings.Join(fields[:nameEnd], " ")] = fields[nameEnd] + " / " + fields[nameEnd+1]
	}
	return out
}

func readCronEntries() []model.Timer {
	paths := []string{"/etc/crontab"}
	system, _ := filepath.Glob("/etc/cron.d/*")
	userDebian, _ := filepath.Glob("/var/spool/cron/crontabs/*")
	userOther, _ := filepath.Glob("/var/spool/cron/*")
	paths = append(paths, system...)
	paths = append(paths, userDebian...)
	paths = append(paths, userOther...)
	seen := map[string]bool{}
	var out []model.Timer
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for number, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") ||
				(strings.Contains(line, "=") && !strings.HasPrefix(line, "@")) {
				continue
			}
			fields := strings.Fields(line)
			scheduleFields := 5
			if strings.HasPrefix(line, "@") {
				scheduleFields = 1
			}
			if len(fields) <= scheduleFields {
				continue
			}
			userName := filepath.Base(path)
			commandStart := scheduleFields
			if path == "/etc/crontab" || strings.HasPrefix(path, "/etc/cron.d/") {
				userName = fields[scheduleFields]
				commandStart++
			}
			if commandStart >= len(fields) {
				continue
			}
			out = append(out, model.Timer{
				Unit:   fmt.Sprintf("cron:%s:%d", filepath.Base(path), number+1),
				Source: "cron", Schedule: strings.Join(fields[:scheduleFields], " "),
				Command: strings.Join(fields[commandStart:], " "), User: userName,
				Next: strings.Join(fields[:scheduleFields], " "),
			})
		}
	}
	return out
}

func parseBytePair(value string) (uint64, uint64) {
	left, right, _ := strings.Cut(value, "/")
	return parseHumanBytes(strings.TrimSpace(left)), parseHumanBytes(strings.TrimSpace(right))
}

func parseHumanBytes(value string) uint64 {
	units := []struct {
		suffix     string
		multiplier float64
	}{
		{"KiB", 1 << 10}, {"MiB", 1 << 20}, {"GiB", 1 << 30}, {"TiB", 1 << 40},
		{"kB", 1e3}, {"KB", 1e3}, {"MB", 1e6}, {"GB", 1e9}, {"TB", 1e12}, {"B", 1},
	}
	for _, unit := range units {
		if strings.HasSuffix(value, unit.suffix) {
			number := strings.TrimSpace(strings.TrimSuffix(value, unit.suffix))
			parsed, _ := strconv.ParseFloat(number, 64)
			return uint64(parsed * unit.multiplier)
		}
	}
	parsed, _ := strconv.ParseUint(value, 10, 64)
	return parsed
}

// Keep imports used on Linux across Go versions.
var _ = bufio.ErrInvalidUnreadByte
var _ = net.IP{}
