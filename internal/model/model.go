package model

import "time"

type CPU struct {
	Usage     float64    `json:"usage"`
	Load      [3]float64 `json:"load"`
	Cores     int        `json:"cores"`
	Frequency float64    `json:"frequency_mhz,omitempty"`
}

type Memory struct {
	Total, Used, Available, SwapTotal, SwapUsed uint64
}

type Disk struct {
	Device, Mount, Filesystem string
	Total, Used, Available    uint64
	UsedPercent               float64
	InodesTotal, InodesUsed   uint64
}

type NetworkInterface struct {
	Name                                                                       string
	RXBytes, TXBytes, RXRate, TXRate, RXErrors, TXErrors, RXDropped, TXDropped uint64
}

type Process struct {
	PID, PPID, Threads            int
	User, Name, Command           string
	State                         string
	CPU                           float64
	Memory, ReadBytes, WriteBytes uint64
	StartTime                     time.Time
}

type ProcessDetail struct {
	Process     Process           `json:"process"`
	Executable  string            `json:"executable"`
	WorkingDir  string            `json:"working_dir"`
	Cgroup      string            `json:"cgroup"`
	OpenFDs     int               `json:"open_fds"`
	Children    []int             `json:"children"`
	Status      map[string]string `json:"status"`
	Limits      map[string]string `json:"limits"`
	Namespaces  map[string]string `json:"namespaces"`
	OpenFiles   []string          `json:"open_files"`
	Connections []Connection      `json:"connections"`
}

type Service struct {
	Name, Load, Active, Sub, Description, UnitFile string
	ActiveSince, Duration                          string
	PID, Restarts                                  int
	Memory                                         uint64
	Manageable, Protected                          bool
}

type SSHSession struct {
	ID, User, Remote, TTY, Since, Duration, Idle string
	PID                                          int
}

type Timer struct {
	Next, Left, Last, Passed, Unit, Activates string
	Source, Schedule, Command, User           string
	Managed                                   bool
}

type Container struct {
	ID, Name, Image, Status, State, Health, Ports, Created   string
	CPU                                                      float64
	Memory, MemoryLimit, NetRX, NetTX, BlockRead, BlockWrite uint64
}

type DockerSummary struct {
	ServerVersion     string `json:"server_version"`
	Driver            string `json:"storage_driver"`
	Containers        int    `json:"containers"`
	ContainersRunning int    `json:"containers_running"`
	ContainersStopped int    `json:"containers_stopped"`
	ContainersPaused  int    `json:"containers_paused"`
	Images            int    `json:"images"`
}

type DockerImage struct {
	ID             string    `json:"id"`
	References     []string  `json:"references"`
	RepoDigests    []string  `json:"repo_digests"`
	CreatedAt      time.Time `json:"created_at"`
	SizeBytes      uint64    `json:"size_bytes"`
	ContainerNames []string  `json:"container_names"`
	Dangling       bool      `json:"dangling"`
}

type Package struct {
	ID                 string    `json:"id"`
	Manager            string    `json:"manager"`
	Name               string    `json:"name"`
	Architecture       string    `json:"architecture"`
	InstalledVersion   string    `json:"installed_version"`
	CandidateVersion   string    `json:"candidate_version,omitempty"`
	UpdateState        string    `json:"update_state"`
	Source             string    `json:"source,omitempty"`
	Summary            string    `json:"summary,omitempty"`
	InstalledSizeBytes uint64    `json:"installed_size_bytes"`
	FirstSeen          time.Time `json:"first_seen,omitempty"`
	LastChanged        time.Time `json:"last_changed,omitempty"`
}

type PackageScan struct {
	Hostname             string    `json:"hostname"`
	Manager              string    `json:"manager"`
	InventoryAvailable   bool      `json:"inventory_available"`
	UpdateCheckAvailable bool      `json:"update_check_available"`
	InventoryScannedAt   time.Time `json:"inventory_scanned_at"`
	MetadataRefreshedAt  time.Time `json:"metadata_refreshed_at,omitempty"`
	Error                string    `json:"error,omitempty"`
	Items                []Package `json:"items"`
}

type Connection struct {
	Protocol, Local, Remote, State, Process, Container string
	PID                                                int
}

// NetworkFlow is a metadata-only byte delta for one process/socket interval.
// Payloads, DNS contents and packet bodies are intentionally never captured.
type NetworkFlow struct {
	Timestamp  time.Time `json:"timestamp"`
	PID        int       `json:"pid"`
	Process    string    `json:"process"`
	Group      string    `json:"group"`
	User       string    `json:"user"`
	Protocol   string    `json:"protocol"`
	Local      string    `json:"local"`
	RemoteIP   string    `json:"remote_ip"`
	RemotePort int       `json:"remote_port"`
	RXBytes    uint64    `json:"rx_bytes"`
	TXBytes    uint64    `json:"tx_bytes"`
}

type Snapshot struct {
	Timestamp    time.Time            `json:"timestamp"`
	Hostname     string               `json:"hostname"`
	Kernel       string               `json:"kernel"`
	Uptime       float64              `json:"uptime_seconds"`
	CPU          CPU                  `json:"cpu"`
	Memory       Memory               `json:"memory"`
	Disks        []Disk               `json:"disks"`
	Network      []NetworkInterface   `json:"network"`
	Processes    []Process            `json:"processes"`
	Services     []Service            `json:"services"`
	SSHSessions  []SSHSession         `json:"ssh_sessions"`
	Timers       []Timer              `json:"timers"`
	Containers   []Container          `json:"containers"`
	Docker       DockerSummary        `json:"docker"`
	Connections  []Connection         `json:"connections"`
	NetworkFlows []NetworkFlow        `json:"network_flows"`
	NetworkMode  string               `json:"network_attribution_mode"`
	NetworkDrops uint64               `json:"network_accounting_dropped_bytes"`
	Capabilities map[string]bool      `json:"capabilities"`
	Freshness    map[string]time.Time `json:"freshness"`
	Errors       []string             `json:"errors,omitempty"`
}

type ActionRequest struct {
	Action string            `json:"action"`
	Target string            `json:"target"`
	Params map[string]string `json:"params,omitempty"`
}

type ActionResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}
