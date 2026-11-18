// Copyright 2026 The KubeStack Authors. All rights reserved.
// Hand-written mirror of api/openapi.yaml. The browser only ever talks to the KubeStack API.

export type Operation = "upgrade" | "delete";

export interface Cluster {
  id: string;
  name: string;
  tier: "provisioned" | "hosted" | "attached";
  upgradeOwner: "kubestack" | "rancher" | "cloud" | "none";
  kubernetesVersion: string;
  ready: boolean;
  operations: Record<Operation, { allowed: boolean; reason?: string }>;
}

export interface AuditRecord {
  id: number;
  time: string;
  actor: string;
  action: string;
  clusterId: string;
  result: string;
  error?: string;
}

export class ApiError extends Error {
  constructor(message: string, public details: string[] = []) {
    super(message);
  }
}

async function call<T>(method: string, path: string, body?: unknown, idempotent = false): Promise<T> {
  const headers: Record<string, string> = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (idempotent) headers["Idempotency-Key"] = crypto.randomUUID();
  const res = await fetch(`/api/v1${path}`, {
    method,
    headers,
    credentials: "same-origin",
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(data.error ?? res.statusText, data.details ?? []);
  return data as T;
}

export const api = {
  clusters: () => call<Cluster[]>("GET", "/clusters"),
  versions: () => call<string[]>("GET", "/versions"),
  audit: () => call<AuditRecord[]>("GET", "/audit?limit=50"),
  create: (name: string, kubernetesVersion: string) => call<Cluster>("POST", "/clusters", { name, kubernetesVersion }),
  upgrade: (id: string, kubernetesVersion: string) =>
    call<Cluster>("POST", `/clusters/${encodeURIComponent(id)}/upgrade`, { kubernetesVersion }, true),
  remove: (id: string) => call<unknown>("DELETE", `/clusters/${encodeURIComponent(id)}`, undefined, true),
};
