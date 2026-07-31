import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "./ApiClient";

afterEach(() => vi.unstubAllGlobals());

describe("ApiError", () => {
  it("keeps the stable API error code and HTTP status", () => {
    const error = new ApiError(403, "csrf_failed", "Request verification failed");
    expect(error.status).toBe(403);
    expect(error.code).toBe("csrf_failed");
    expect(error.message).toBe("Request verification failed");
  });
});

describe("ApiClient stream", () => {
  it("does not report a disconnect when snapshot processing throws", () => {
    let snapshotListener: EventListener | undefined;
    class EventSourceStub {
      public onopen: ((event: Event) => void) | null = null;
      public onerror: ((event: Event) => void) | null = null;
      public addEventListener(_type: string, listener: EventListener): void {
        snapshotListener = listener;
      }
    }
    vi.stubGlobal("EventSource", EventSourceStub);
    const states: boolean[] = [];
    new ApiClient().stream(
      () => { throw new Error("consumer failed"); },
      (connected) => states.push(connected),
    );

    expect(() => snapshotListener?.({ data: "{}" } as MessageEvent<string>)).toThrow("consumer failed");
    expect(states).toEqual([true]);
  });
});

describe("ApiClient Docker images", () => {
  it("normalizes nullable arrays returned by Go JSON", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      available: true,
      freshness: "2026-01-01T00:00:00Z",
      errors: null,
      items: [{
        id: "sha256:test", references: null, repo_digests: null,
        created_at: "2026-01-01T00:00:00Z", size_bytes: 1,
        container_names: null, dangling: true,
      }],
    }), { status: 200, headers: { "Content-Type": "application/json" } })));

    const response = await new ApiClient().dockerImages();
    expect(response.errors).toEqual([]);
    expect(response.items[0]!.references).toEqual([]);
    expect(response.items[0]!.repo_digests).toEqual([]);
    expect(response.items[0]!.container_names).toEqual([]);
  });
});

describe("ApiClient list responses", () => {
  it("normalizes nullable package collections", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ items: null }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ items: null }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ items: null }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient();
    expect((await client.packages(new URLSearchParams())).items).toEqual([]);
    expect((await client.packageEvents(new URLSearchParams())).items).toEqual([]);
    expect((await client.packageFiles("test")).items).toEqual([]);
  });
});
