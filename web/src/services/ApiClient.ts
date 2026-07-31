import type { DockerImagesResponse, DockerResponse, MetricPoint, NetworkStorage, NetworkUsageDetail, NetworkUsageResponse, PackageEventsResponse, PackageFilesResponse, PackageInfo, PackageListResponse, ProcessDetail, ResourceUsageDetail, ResourceUsageResponse, Session, Snapshot } from "@/types";

export class ApiError extends Error {
  public readonly status: number;
  public readonly code: string;

  public constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

interface ApiErrorBody {
  error?: { code?: string; message?: string };
}

/** Centralized HTTP client. Authentication cookies never enter application state. */
export class ApiClient {
  private csrfToken = "";

  public setCsrfToken(token: string): void {
    this.csrfToken = token;
  }

  public async login(username: string, password: string): Promise<Session> {
    const session = await this.request<Session>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }, false);
    this.setCsrfToken(session.csrf);
    return session;
  }

  public async session(): Promise<Session> {
    const session = await this.request<Session>("/auth/session");
    this.setCsrfToken(session.csrf);
    return session;
  }

  public logout(): Promise<{ ok: boolean }> {
    return this.request("/auth/logout", { method: "POST" });
  }

  public overview(): Promise<Snapshot> {
    return this.request("/overview");
  }

  public async processDetail(pid: number): Promise<ProcessDetail> {
    const detail = await this.request<ProcessDetail>(`/processes/${pid}`);
    detail.children ??= [];
    detail.open_files ??= [];
    detail.connections ??= [];
    detail.status ??= {};
    detail.limits ??= {};
    detail.namespaces ??= {};
    return detail;
  }

  public async docker(): Promise<DockerResponse> {
    const response = await this.request<DockerResponse>("/docker");
    response.items ??= [];
    response.errors ??= [];
    return response;
  }

  public async dockerImages(): Promise<DockerImagesResponse> {
    const response = await this.request<DockerImagesResponse>("/docker/images");
    response.items ??= [];
    response.errors ??= [];
    response.items = response.items.map((item) => ({
      ...item,
      references: item.references ?? [],
      repo_digests: item.repo_digests ?? [],
      container_names: item.container_names ?? [],
    }));
    return response;
  }

  public async packages(params: URLSearchParams): Promise<PackageListResponse> {
    const response = await this.request<PackageListResponse>(`/packages?${params}`);
    response.items ??= [];
    return response;
  }

  public packageDetail(id: string): Promise<PackageInfo> {
    return this.request(`/packages/${encodeURIComponent(id)}`);
  }

  public async packageFiles(id: string, query = "", page = 1): Promise<PackageFilesResponse> {
    const params = new URLSearchParams({ q: query, page: String(page), per_page: "200" });
    const response = await this.request<PackageFilesResponse>(`/packages/${encodeURIComponent(id)}/files?${params}`);
    response.items ??= [];
    return response;
  }

  public async packageEvents(params: URLSearchParams): Promise<PackageEventsResponse> {
    const response = await this.request<PackageEventsResponse>(`/package-events?${params}`);
    response.items ??= [];
    return response;
  }

  public refreshPackages(): Promise<{ accepted: boolean; already_running: boolean }> {
    return this.request("/packages/refresh", { method: "POST", body: "{}" });
  }

  public async networkUsage(from: string, groupBy: "process" | "group", query = ""): Promise<NetworkUsageResponse> {
    const params = new URLSearchParams({ from, group_by: groupBy });
    if (query) params.set("q", query);
    const response = await this.request<NetworkUsageResponse>(`/network-usage?${params}`);
    response.items ??= [];
    return response;
  }

  public async networkUsageDetail(from: string, selector: "process" | "group" | "pid", value: string): Promise<NetworkUsageDetail> {
    const params = new URLSearchParams({ from, selector, value });
    const response = await this.request<NetworkUsageDetail>(`/network-usage/detail?${params}`);
    response.destinations ??= [];
    response.timeline ??= [];
    return response;
  }

  public async resourceUsage(from: string, groupBy: "process" | "group", query = ""): Promise<ResourceUsageResponse> {
    const params = new URLSearchParams({ from, group_by: groupBy });
    if (query) params.set("q", query);
    const response = await this.request<ResourceUsageResponse>(`/resource-usage?${params}`);
    response.items ??= [];
    return response;
  }

  public async resourceUsageDetail(from: string, selector: "process" | "group", value: string): Promise<ResourceUsageDetail> {
    const params = new URLSearchParams({ from, selector, value });
    const response = await this.request<ResourceUsageDetail>(`/resource-usage/detail?${params}`);
    response.timeline ??= [];
    return response;
  }

  public networkSettings(): Promise<NetworkStorage> {
    return this.request("/settings/network");
  }

  public updateNetworkSettings(settings: Partial<NetworkStorage> & { retention_days: number }): Promise<NetworkStorage> {
    return this.request("/settings/network", {
      method: "PATCH", body: JSON.stringify(settings),
    });
  }

  public clearNetworkHistory(): Promise<NetworkStorage> {
    return this.request("/settings/network", {
      method: "DELETE", body: JSON.stringify({ confirm: "DELETE NETWORK HISTORY" }),
    });
  }

  public clearResourceHistory(): Promise<NetworkStorage> {
    return this.request("/settings/resources", {
      method: "DELETE", body: JSON.stringify({ confirm: "DELETE RESOURCE HISTORY" }),
    });
  }

  public async history(hours: number): Promise<MetricPoint[]> {
    const from = new Date(Date.now() - hours * 3_600_000).toISOString();
    const response = await this.request<{ items: MetricPoint[] }>(
      `/history?from=${encodeURIComponent(from)}`,
    );
    return response.items ?? [];
  }

  public async list<T>(path: string): Promise<T[]> {
    const response = await this.request<{ items: T[] }>(path);
    return response.items ?? [];
  }

  public action(action: string, target = "", params: Record<string, string> = {}): Promise<unknown> {
    return this.request("/actions", {
      method: "POST",
      body: JSON.stringify({ action, target, params }),
    });
  }

  public schedule(action: string, target: string, params: Record<string, string> = {}): Promise<unknown> {
    return this.request("/schedules", {
      method: "POST",
      body: JSON.stringify({ action, target, params }),
    });
  }

  public create<T>(path: string, value: unknown): Promise<T> {
    return this.request(path, { method: "POST", body: JSON.stringify(value) });
  }

  public remove(path: string): Promise<void> {
    return this.request(path, { method: "DELETE" });
  }

	public stream(
		onSnapshot: (snapshot: Snapshot) => void,
		onState?: (connected: boolean) => void,
		onAuthExpired?: () => void,
	): EventSource {
    const source = new EventSource("/api/v1/stream");
    let disconnectTimer: number | undefined;
    const connected = (): void => {
      if (disconnectTimer !== undefined) window.clearTimeout(disconnectTimer);
      disconnectTimer = undefined;
      onState?.(true);
    };
    source.onopen = connected;
    source.onerror = () => {
      if (disconnectTimer !== undefined) window.clearTimeout(disconnectTimer);
      // EventSource transparently reconnects. Do not flash an offline state
      // while fresh snapshots are still arriving.
      disconnectTimer = window.setTimeout(() => onState?.(false), 4_000);
    };
	source.addEventListener("snapshot", (event) => {
      let snapshot: Snapshot;
      try {
        snapshot = JSON.parse((event as MessageEvent<string>).data) as Snapshot;
      } catch {
        // A malformed application payload does not mean the SSE transport
        // disconnected. Leave connection state to onopen/onerror.
        return;
      }
      connected();
		onSnapshot(snapshot);
	});
	source.addEventListener("auth-expired", () => {
		source.close();
		if (disconnectTimer !== undefined) window.clearTimeout(disconnectTimer);
		disconnectTimer = undefined;
		onState?.(false);
		onAuthExpired?.();
	});
    return source;
  }

  private async request<T>(
    path: string,
    init: RequestInit = {},
    includeCsrf = true,
  ): Promise<T> {
    const headers = new Headers(init.headers);
    headers.set("Accept", "application/json");
    if (init.body) headers.set("Content-Type", "application/json");
    if (includeCsrf && init.method && !["GET", "HEAD"].includes(init.method)) {
      headers.set("X-CSRF-Token", this.csrfToken);
    }
    const response = await fetch(`/api/v1${path}`, {
      ...init,
      headers,
      credentials: "same-origin",
    });
    if (!response.ok) {
      const body = await response.json().catch(() => ({})) as ApiErrorBody;
      throw new ApiError(
        response.status,
        body.error?.code ?? "request_failed",
        body.error?.message ?? response.statusText,
      );
    }
    if (response.status === 204) return undefined as T;
    return response.json() as Promise<T>;
  }
}

export const apiClient = new ApiClient();
