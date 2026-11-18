// Copyright 2026 The KubeStack Authors. All rights reserved.
// Hand-written mirror of api/openapi.yaml. The browser only ever talks to the KubeStack API.

export type Operation = "upgrade" | "delete" | "register" | "snapshot" | "restore" | "removeNode" | "access";

/** Connection info for direct access with the user's own IdP token; contains no credentials. */
export interface Access {
  servers: string[];
  certificateAuthorityData: string;
  oidc: { issuerURL: string; clientID: string };
}

export interface Snapshot {
  name: string;
  createdAt: string;
  successful: boolean;
}

export interface Node {
  name: string;
  ready: boolean;
  roles: string[];
}

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

/** The command contains the literal "{token}"; the token stays in the referenced Secret. */
export interface Registration {
  command: string;
  token: { namespace: string; name: string; key: string };
}

export interface Me {
  name: string;
  admin: boolean;
}

export class ApiError extends Error {
  constructor(message: string, public details: string[] = [], public status = 0) {
    super(message);
  }
}

async function call<T>(method: string, path: string, body?: unknown, idempotent = false): Promise<T> {
  // Required by the server for cookie-authenticated mutations (CSRF protection).
  const headers: Record<string, string> = { "X-KubeStack-Request": "1" };
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (idempotent) headers["Idempotency-Key"] = crypto.randomUUID();
  const res = await fetch(`/api/v1${path}`, {
    method,
    headers,
    credentials: "same-origin",
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(data.error ?? res.statusText, data.details ?? [], res.status);
  return data as T;
}

export const api = {
  me: () => call<Me>("GET", "/me"),
  logout: () => fetch("/auth/logout", { method: "POST", credentials: "same-origin" }),
  clusters: () => call<Cluster[]>("GET", "/clusters"),
  versions: () => call<string[]>("GET", "/versions"),
  audit: () => call<AuditRecord[]>("GET", "/audit?limit=50"),
  create: (name: string, kubernetesVersion: string) => call<Cluster>("POST", "/clusters", { name, kubernetesVersion }),
  importCluster: (name: string) => call<Cluster>("POST", "/clusters/import", { name }),
  registration: (id: string) => call<Registration>("GET", `/clusters/${encodeURIComponent(id)}/registration`),
  access: (id: string) => call<Access>("GET", `/clusters/${encodeURIComponent(id)}/access`),
  snapshots: (id: string) => call<Snapshot[]>("GET", `/clusters/${encodeURIComponent(id)}/snapshots`),
  takeSnapshot: (id: string) => call<unknown>("POST", `/clusters/${encodeURIComponent(id)}/snapshots`),
  restore: (id: string, snapshot: string) => call<unknown>("POST", `/clusters/${encodeURIComponent(id)}/restore`, { snapshot }, true),
  nodes: (id: string) => call<Node[]>("GET", `/clusters/${encodeURIComponent(id)}/nodes`),
  removeNode: (id: string, node: string) =>
    call<unknown>("DELETE", `/clusters/${encodeURIComponent(id)}/nodes/${encodeURIComponent(node)}`, undefined, true),
  upgrade: (id: string, kubernetesVersion: string) =>
    call<Cluster>("POST", `/clusters/${encodeURIComponent(id)}/upgrade`, { kubernetesVersion }, true),
  remove: (id: string) => call<unknown>("DELETE", `/clusters/${encodeURIComponent(id)}`, undefined, true),
};
