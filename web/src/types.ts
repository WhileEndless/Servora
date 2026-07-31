export interface Session {
  username: string;
  csrf: string;
  version: string;
  role: "admin";
}

export interface CpuMetric {
  usage: number;
  load: [number, number, number];
  cores: number;
  frequency_mhz?: number;
}

export interface MemoryMetric {
  Total: number;
  Used: number;
  Available: number;
  SwapTotal: number;
  SwapUsed: number;
}

export interface ProcessInfo {
  PID: number; PPID: number; Threads: number; User: string; Name: string;
  Command: string; State: string; CPU: number; Memory: number;
  ReadBytes: number; WriteBytes: number; StartTime: string;
}

export interface ProcessDetail {
  process: ProcessInfo;
  executable: string;
  working_dir: string;
  cgroup: string;
  open_fds: number;
  children: number[];
  status: Record<string, string>;
  limits: Record<string, string>;
  namespaces: Record<string, string>;
  open_files: string[];
  connections: Record<string, unknown>[];
}

export interface ServiceInfo {
  Name: string; Load: string; Active: string; Sub: string; Description: string;
  UnitFile: string; ActiveSince: string; Duration: string; PID: number;
  Restarts: number; Memory: number; Manageable: boolean; Protected: boolean;
}

export interface Snapshot {
  timestamp: string; hostname: string; kernel: string; uptime_seconds: number;
  cpu: CpuMetric; memory: MemoryMetric; disks: Record<string, unknown>[];
  network: Record<string, unknown>[]; processes: ProcessInfo[];
  services: ServiceInfo[]; ssh_sessions: Record<string, unknown>[];
  timers: Record<string, unknown>[]; containers: Record<string, unknown>[];
  docker: DockerSummary;
  connections: Record<string, unknown>[]; capabilities: Record<string, boolean>;
  network_attribution_mode: "ebpf-exact" | "socket-counter-fallback" | string;
  network_accounting_dropped_bytes: number;
  freshness: Record<string, string>; errors?: string[];
}

export interface DockerSummary {
  server_version: string; storage_driver: string; containers: number;
  containers_running: number; containers_stopped: number;
  containers_paused: number; images: number;
}

export interface DockerResponse {
  available: boolean;
  freshness: string;
  errors: string[] | null;
  items: Record<string, unknown>[];
  summary: DockerSummary;
}

export interface DockerImageInfo {
  id: string;
  references: string[];
  repo_digests: string[];
  created_at: string;
  size_bytes: number;
  container_names: string[];
  dangling: boolean;
}

export interface DockerImagesResponse {
  available: boolean;
  freshness: string;
  errors: string[];
  items: DockerImageInfo[];
}

export interface PackageInfo {
  id: string;
  manager: string;
  name: string;
  architecture: string;
  installed_version: string;
  candidate_version?: string;
  update_state: "current" | "update_available" | "unknown";
  source?: string;
  summary?: string;
  installed_size_bytes: number;
  first_seen: string;
  last_changed: string;
}

export interface PackageStatus {
  hostname: string;
  manager: string;
  inventory_available: boolean;
  update_check_available: boolean;
  inventory_scanned_at: string;
  metadata_refreshed_at: string;
  refreshing: boolean;
  error?: string;
}

export interface PackageListResponse {
  items: PackageInfo[];
  total: number;
  page: number;
  per_page: number;
  summary: { installed: number; updates: number; unknown: number };
  status: PackageStatus;
}

export interface PackageFilesResponse {
  items: string[];
  total: number;
  page: number;
  per_page: number;
  package: PackageInfo;
}

export interface PackageEvent {
  id: number;
  time: string;
  package_id: string;
  manager: string;
  name: string;
  architecture: string;
  event_type: "installed" | "removed" | "version_changed";
  old_version?: string;
  new_version?: string;
}

export interface PackageEventsResponse {
  items: PackageEvent[];
  total: number;
  page: number;
  per_page: number;
}

export interface NetworkUsage {
  key: string; process: string; group: string; user: string; pid: number;
  rx_bytes: number; tx_bytes: number; destinations: number;
  first_seen: string; last_seen: string;
}

export interface NetworkStorage {
  retention_days: number; rows: number; bytes: number; oldest: number; newest: number;
  network_enabled: boolean; cpu_enabled: boolean;
  memory_enabled: boolean; disk_io_enabled: boolean;
  resource_rows: number; resource_bytes: number;
}

export interface NetworkUsageResponse {
  items: NetworkUsage[]; group_by: "process" | "group";
  from: string; to: string; storage: NetworkStorage;
}

export interface NetworkDestination {
  remote_ip: string; remote_port: number; protocol: string;
  rx_bytes: number; tx_bytes: number; first_seen: string; last_seen: string;
}

export interface NetworkTimelinePoint {
  time: string; rx_bytes: number; tx_bytes: number;
}

export interface NetworkUsageDetail {
  selector: "process" | "group" | "pid"; value: string; from: string; to: string;
  destinations: NetworkDestination[]; timeline: NetworkTimelinePoint[];
}

export interface ResourceUsage {
  key: string; process: string; group: string; user: string; pid: number;
  cpu_average: number; cpu_max: number; memory_average: number; memory_max: number;
  read_bytes: number; write_bytes: number; first_seen: string; last_seen: string;
}

export interface ResourceTimelinePoint {
  time: string; cpu_average: number; cpu_max: number;
  memory_average: number; memory_max: number; read_bytes: number; write_bytes: number;
}

export interface ResourceUsageResponse {
  items: ResourceUsage[]; group_by: "process" | "group";
  from: string; to: string; storage: NetworkStorage;
}

export interface ResourceUsageDetail {
  selector: "process" | "group"; value: string; from: string; to: string;
  timeline: ResourceTimelinePoint[];
}

export interface MetricPoint {
  time: string; cpu: number; memory: number; swap: number; load: number;
  network_rx: number; network_tx: number; disk: number;
  processes: number; containers: number;
}

export type PageName =
  "overview" | "processes" | "services" | "docker" | "network" |
  "packages" | "ssh" | "schedules" | "alerts" | "activity" | "settings";
