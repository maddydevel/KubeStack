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
  // getRandomValues, not randomUUID: the latter is missing on plain-HTTP (non-localhost) origins.
  if (idempotent)
    headers["Idempotency-Key"] = Array.from(crypto.getRandomValues(new Uint8Array(16)), (b) =>
      b.toString(16).padStart(2, "0"),
    ).join("");
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

/** A kubeconfig without credentials: kubectl obtains the user's own IdP token via the kubelogin plugin. */
export function kubeconfig(name: string, a: Access): string {
  const ca = a.certificateAuthorityData ? `\n    certificate-authority-data: ${a.certificateAuthorityData}` : "";
  return `apiVersion: v1
kind: Config
clusters:
- name: ${name}
  cluster:
    server: ${a.servers[0]}${ca}
users:
- name: ${name}-oidc
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: kubectl
      args: [oidc-login, get-token, --oidc-issuer-url=${a.oidc.issuerURL}, --oidc-client-id=${a.oidc.clientID}, --oidc-extra-scope=email, --oidc-extra-scope=groups]
      interactiveMode: IfAvailable
contexts:
- name: ${name}
  context: {cluster: ${name}, user: ${name}-oidc}
current-context: ${name}
`;
}

/** Shell snippet: the operator reads the token with their own kubectl credentials; KubeStack never sees it. */
export function registrationScript(r: Registration, provisioned: boolean): string {
  const read = `TOKEN=$(kubectl -n ${r.token.namespace} get secret ${r.token.name} -o jsonpath='{.data.${r.token.key}}' | base64 -d)`;
  const cmd = r.command.split("{token}").join("${TOKEN}") + (provisioned ? " --etcd --controlplane --worker" : "");
  return `${read}\n${cmd}`;
}
