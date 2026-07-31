import { computed, readonly, ref } from "vue";
import { apiClient } from "./ApiClient";
import type { MetricPoint, PageName, Session, Snapshot } from "@/types";

const emptySnapshot = (): Snapshot => ({
  timestamp: "", hostname: "", kernel: "", uptime_seconds: 0,
  cpu: { usage: 0, load: [0, 0, 0], cores: 0 },
  memory: { Total: 0, Used: 0, Available: 0, SwapTotal: 0, SwapUsed: 0 },
  disks: [], network: [], processes: [], services: [], ssh_sessions: [],
  timers: [], containers: [], docker: {
    server_version: "", storage_driver: "", containers: 0,
    containers_running: 0, containers_stopped: 0, containers_paused: 0, images: 0,
  }, connections: [], capabilities: {}, network_attribution_mode: "",
  network_accounting_dropped_bytes: 0, freshness: {},
});

/**
 * Small observable application store. Domain mutations stay in API services;
 * components receive readonly state and explicit operations.
 */
export class MonitorStore {
  private readonly sessionState = ref<Session | null>(null);
  private readonly snapshotState = ref<Snapshot>(emptySnapshot());
  private readonly historyState = ref<MetricPoint[]>([]);
  private readonly pageState = ref<PageName>("overview");
  private readonly searchState = ref("");
  private readonly streamConnectedState = ref(false);
  private eventSource?: EventSource;

  public readonly session = readonly(this.sessionState);
  public readonly snapshot = readonly(this.snapshotState);
  public readonly history = readonly(this.historyState);
  public readonly page = readonly(this.pageState);
  public readonly search = readonly(this.searchState);
  public readonly streamConnected = readonly(this.streamConnectedState);
  public readonly authenticated = computed(() => this.sessionState.value !== null);

  public async restore(): Promise<boolean> {
    try {
      this.sessionState.value = await apiClient.session();
    } catch {
      this.sessionState.value = null;
      return false;
    }
    await this.connect();
    return true;
  }

  public async login(username: string, password: string): Promise<void> {
    this.sessionState.value = await apiClient.login(username, password);
    await this.connect();
  }

  public async logout(): Promise<void> {
    try { await apiClient.logout(); } finally {
      this.eventSource?.close();
      this.streamConnectedState.value = false;
      this.sessionState.value = null;
    }
  }

  public setPage(page: PageName): void { this.pageState.value = page; }
  public setSearch(value: string): void { this.searchState.value = value; }

  public async refresh(): Promise<void> {
    this.applySnapshot(await apiClient.overview());
  }

  public async loadHistory(hours: number): Promise<void> {
    this.historyState.value = await apiClient.history(hours);
  }

  private async connect(): Promise<void> {
    // Authentication and telemetry availability are separate concerns. A slow
    // collector or history query must never discard a valid browser session.
    await Promise.allSettled([this.refresh(), this.loadHistory(24)]);
    this.eventSource?.close();
    this.streamConnectedState.value = false;
    this.eventSource = apiClient.stream(
      (value) => {
        this.streamConnectedState.value = true;
        this.applySnapshot(value);
      },
		(connected) => { this.streamConnectedState.value = connected; },
		() => { this.expireSession(); },
	);
	}

	private expireSession(): void {
		this.eventSource?.close();
		this.eventSource = undefined;
		this.streamConnectedState.value = false;
		this.sessionState.value = null;
		this.snapshotState.value = emptySnapshot();
		this.historyState.value = [];
		apiClient.setCsrfToken("");
	}

  private applySnapshot(value: Snapshot): void {
    // Go encodes nil slices/maps as null. Normalize them at the boundary so a
    // host without Docker, systemd, disks, or connections remains a valid
    // snapshot for every view and for the history calculation below.
    const snapshot: Snapshot = {
      ...value,
      disks: value.disks ?? [],
      network: value.network ?? [],
      processes: value.processes ?? [],
      services: value.services ?? [],
      ssh_sessions: value.ssh_sessions ?? [],
      timers: value.timers ?? [],
      containers: value.containers ?? [],
      connections: value.connections ?? [],
      capabilities: value.capabilities ?? {},
      freshness: value.freshness ?? {},
    };
    this.snapshotState.value = snapshot;
    if (!snapshot.timestamp) return;
    const memory = snapshot.memory.Total ? snapshot.memory.Used * 100 / snapshot.memory.Total : 0;
    const swap = snapshot.memory.SwapTotal ? snapshot.memory.SwapUsed * 100 / snapshot.memory.SwapTotal : 0;
    const disk = snapshot.disks.reduce((max, item) => {
      const used = typeof item.UsedPercent === "number" ? item.UsedPercent : 0;
      return Math.max(max, used);
    }, 0);
    const networkRx = snapshot.network.reduce((sum, item) => sum + (typeof item.RXRate === "number" ? item.RXRate : 0), 0);
    const networkTx = snapshot.network.reduce((sum, item) => sum + (typeof item.TXRate === "number" ? item.TXRate : 0), 0);
    const point: MetricPoint = {
      time: snapshot.timestamp, cpu: snapshot.cpu.usage, memory, swap,
      load: snapshot.cpu.load[0] ?? 0, network_rx: networkRx,
      network_tx: networkTx, disk, processes: snapshot.processes.length,
      containers: snapshot.containers.length,
    };
    const previous = this.historyState.value[this.historyState.value.length - 1];
    if (!previous || previous.time !== point.time) {
      this.historyState.value = [...this.historyState.value.slice(-899), point];
    }
  }
}

export const monitorStore = new MonitorStore();
