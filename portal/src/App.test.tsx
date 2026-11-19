// Copyright 2026 The KubeStack Authors. All rights reserved.
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import App, { registrationScript } from "./App";
import type { Cluster } from "./api";

const prod: Cluster = {
  id: "prod-eu", name: "prod-eu", tier: "provisioned", upgradeOwner: "rancher", kubernetesVersion: "v1.34.12+rke2r1", ready: true,
  operations: { upgrade: { allowed: true }, delete: { allowed: true }, register: { allowed: true }, snapshot: { allowed: true }, restore: { allowed: true }, removeNode: { allowed: true } },
};
const eks: Cluster = {
  id: "eks", name: "eks", tier: "hosted", upgradeOwner: "cloud", kubernetesVersion: "v1.33.4-eks", ready: true,
  operations: { upgrade: { allowed: false, reason: "hosted clusters do not support upgrade from KubeStack" }, delete: { allowed: false, reason: "no" }, register: { allowed: false, reason: "no" }, snapshot: { allowed: false, reason: "no" }, restore: { allowed: false, reason: "no" }, removeNode: { allowed: false, reason: "no" } },
};

let fetchMock: ReturnType<typeof vi.fn>;
let signedIn = true;

beforeEach(() => {
  fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    const body = (() => {
      if (url.endsWith("/me")) return signedIn ? { name: "alice@example.com", admin: true } : { error: "unauthenticated" };
      if (url.endsWith("/clusters") && (!init?.method || init.method === "GET")) return [prod, eks];
      if (url.endsWith("/versions")) return ["v1.34.12+rke2r1", "v1.35.9+rke2r1"];
      if (url.includes("/audit")) return [];
      if (url.endsWith("/snapshots") && (!init?.method || init.method === "GET")) return [{ name: "prod-eu-etcd-1", createdAt: "2026-11-18T10:00:00Z", successful: true }];
      if (url.endsWith("/nodes")) return [{ name: "cp-0", ready: true, roles: ["etcd", "controlplane"] }];
      if (url.endsWith("/registration")) return { command: "curl -fL https://e/install.sh | sudo sh -s - --token {token}", token: { namespace: "c-m-1", name: "crt-token-a", key: "token" } };
      if (url.endsWith("/upgrade")) return { error: "pre-flight checks failed", details: ["latest etcd snapshot is 72h0m0s old (max 24h0m0s)"] };
      return { status: "deleting" };
    })();
    const status = url.endsWith("/upgrade") ? 412 : url.endsWith("/me") && !signedIn ? 401 : 200;
    return new Response(JSON.stringify(body), { status });
  });
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); signedIn = true; });

function renderApp() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><App /></QueryClientProvider>);
}

async function rowFor(name: string) {
  return (await screen.findByText(name)).closest("tr")!;
}

test("operations a tier does not support are disabled with the server's reason", async () => {
  renderApp();
  const upgrade = within(await rowFor("eks")).getByRole("button", { name: "Upgrade" });
  expect((upgrade as HTMLButtonElement).disabled).toBe(true);
  expect(upgrade.title).toBe("hosted clusters do not support upgrade from KubeStack");
  expect((within(await rowFor("prod-eu")).getByRole("button", { name: "Upgrade" }) as HTMLButtonElement).disabled).toBe(false);
});

test("delete requires typing the cluster name and sends an idempotency key", async () => {
  const user = userEvent.setup();
  renderApp();
  await user.click(within(await rowFor("prod-eu")).getByRole("button", { name: "Delete" }));
  const confirm = screen.getByRole("button", { name: "Delete cluster" });
  expect((confirm as HTMLButtonElement).disabled).toBe(true);
  await user.type(screen.getByLabelText("Cluster name confirmation"), "prod-e");
  expect((confirm as HTMLButtonElement).disabled).toBe(true);
  await user.type(screen.getByLabelText("Cluster name confirmation"), "u");
  await user.click(confirm);
  const call = fetchMock.mock.calls.find(([, i]) => i?.method === "DELETE")!;
  expect(call[0]).toBe("/api/v1/clusters/prod-eu");
  expect((call[1].headers as Record<string, string>)["Idempotency-Key"]).toMatch(/[0-9a-f-]{36}/);
});

test("upgrade needs explicit confirmation and shows pre-flight failures", async () => {
  const user = userEvent.setup();
  renderApp();
  await user.click(within(await rowFor("prod-eu")).getByRole("button", { name: "Upgrade" }));
  const start = screen.getByRole("button", { name: "Start upgrade" });
  expect((start as HTMLButtonElement).disabled).toBe(true);
  await user.click(screen.getByRole("checkbox"));
  await user.click(start);
  expect(await screen.findByText(/latest etcd snapshot is 72h/)).toBeTruthy();
});

test("signed-out users get a sign-in link and no cluster data is requested", async () => {
  signedIn = false;
  renderApp();
  expect((await screen.findByRole("link", { name: "Sign in" })).getAttribute("href")).toBe("/auth/login");
  expect(fetchMock.mock.calls.some(([u]) => String(u).endsWith("/clusters"))).toBe(false);
});

test("API calls carry the CSRF header", async () => {
  renderApp();
  await rowFor("prod-eu");
  const call = fetchMock.mock.calls.find(([u]) => String(u).endsWith("/clusters"))!;
  expect((call[1].headers as Record<string, string>)["X-KubeStack-Request"]).toBe("1");
});

test("registration shows a script that reads the token via kubectl, never the token itself", async () => {
  const user = userEvent.setup();
  renderApp();
  await user.click(within(await rowFor("prod-eu")).getByRole("button", { name: "Add nodes" }));
  const script = (await screen.findByLabelText("Registration command")).textContent!;
  expect(script).toContain("kubectl -n c-m-1 get secret crt-token-a -o jsonpath='{.data.token}'");
  expect(script).toContain("--token ${TOKEN} --etcd --controlplane --worker");
  expect(script).not.toContain("{token}");
});

test("import URL placeholder is braced so the shell does not swallow the suffix", () => {
  const s = registrationScript({ command: "kubectl apply -f https://r/v3/import/{token}_c-2.yaml", token: { namespace: "c-2", name: "t", key: "token" } }, false);
  expect(s.split("\n")[1]).toBe("kubectl apply -f https://r/v3/import/${TOKEN}_c-2.yaml");
});

test("restore needs the cluster name typed and sends an idempotency key", async () => {
  const user = userEvent.setup();
  renderApp();
  await user.click(within(await rowFor("prod-eu")).getByRole("button", { name: "Backups" }));
  await user.click(await screen.findByRole("button", { name: "Restore…" }));
  const confirm = screen.getByRole("button", { name: "Restore snapshot" }) as HTMLButtonElement;
  expect(confirm.disabled).toBe(true);
  await user.type(screen.getByLabelText("Cluster name confirmation"), "prod-eu");
  await user.click(confirm);
  const call = fetchMock.mock.calls.find(([u]) => String(u).endsWith("/restore"))!;
  expect(JSON.parse(call[1].body)).toEqual({ snapshot: "prod-eu-etcd-1" });
  expect((call[1].headers as Record<string, string>)["Idempotency-Key"]).toMatch(/[0-9a-f-]{36}/);
});

test("node removal needs the node name typed", async () => {
  const user = userEvent.setup();
  renderApp();
  await user.click(within(await rowFor("prod-eu")).getByRole("button", { name: "Nodes" }));
  await user.click(await screen.findByRole("button", { name: "Remove…" }));
  await user.type(screen.getByLabelText("Node name confirmation"), "cp-0");
  await user.click(screen.getByRole("button", { name: "Remove node" }));
  const call = fetchMock.mock.calls.find(([, i]) => i?.method === "DELETE")!;
  expect(call[0]).toBe("/api/v1/clusters/prod-eu/nodes/cp-0");
});
