// Copyright 2026 The KubeStack Authors. All rights reserved.
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import App from "./App";
import { kubeconfig, registrationScript, type Cluster, type Access } from "./api";

const prod: Cluster = {
  id: "prod-eu",
  name: "prod-eu",
  tier: "provisioned",
  upgradeOwner: "rancher",
  kubernetesVersion: "v1.34.12+rke2r1",
  ready: true,
  operations: {
    upgrade: { allowed: true },
    delete: { allowed: true },
    register: { allowed: true },
    snapshot: { allowed: true },
    restore: { allowed: true },
    removeNode: { allowed: true },
    access: { allowed: true },
  },
};
const eks: Cluster = {
  id: "eks",
  name: "eks",
  tier: "hosted",
  upgradeOwner: "cloud",
  kubernetesVersion: "v1.33.4-eks",
  ready: true,
  operations: {
    upgrade: { allowed: false, reason: "hosted clusters do not support upgrade from KubeStack" },
    delete: { allowed: false, reason: "no" },
    register: { allowed: false, reason: "no" },
    snapshot: { allowed: false, reason: "no" },
    restore: { allowed: false, reason: "no" },
    removeNode: { allowed: false, reason: "no" },
    access: { allowed: false, reason: "no" },
  },
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
      if (url.endsWith("/snapshots") && (!init?.method || init.method === "GET"))
        return [{ name: "prod-eu-etcd-1", createdAt: "2026-11-18T10:00:00Z", successful: true }];
      if (url.endsWith("/nodes")) return [{ name: "cp-0", ready: true, roles: ["etcd", "controlplane"] }];
      if (url.endsWith("/registration"))
        return { command: "curl -fL https://e/install.sh | sudo sh -s - --token {token}", token: { namespace: "c-m-1", name: "crt-token-a", key: "token" } };
      if (url.endsWith("/upgrade"))
        return { error: "pre-flight checks failed", details: ["latest etcd snapshot is 72h0m0s old (max 24h0m0s)"] };
      return { status: "deleting" };
    })();
    const status = url.endsWith("/upgrade") ? 412 : url.endsWith("/me") && !signedIn ? 401 : 200;
    return new Response(JSON.stringify(body), { status });
  });
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  signedIn = true;
});

function renderApp() {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><App /></QueryClientProvider>,
  );
}

async function cardFor(name: string) {
  return (await screen.findByText(name, { selector: "h3" })).closest("article")!;
}

test("operations a tier does not support are disabled with the server's reason", async () => {
  renderApp();
  const upgrade = within(await cardFor("eks")).getByRole("button", { name: "Upgrade" });
  expect((upgrade as HTMLButtonElement).disabled).toBe(true);
  expect(upgrade.title).toBe("hosted clusters do not support upgrade from KubeStack");
  expect((within(await cardFor("prod-eu")).getByRole("button", { name: "Upgrade" }) as HTMLButtonElement).disabled).toBe(false);
});

test("delete requires typing the cluster name and sends an idempotency key", async () => {
  const user = userEvent.setup();
  renderApp();
  await user.click(within(await cardFor("prod-eu")).getByRole("button", { name: "Delete" }));
  const confirm = screen.getByTestId("delete-confirm-button");
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
  await user.click(within(await cardFor("prod-eu")).getByRole("button", { name: "Upgrade" }));
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
  await cardFor("prod-eu");
  const call = fetchMock.mock.calls.find(([u]) => String(u).endsWith("/clusters"))!;
  expect((call[1].headers as Record<string, string>)["X-KubeStack-Request"]).toBe("1");
});

test("registration shows a script that reads the token via kubectl, never the token itself", async () => {
  const user = userEvent.setup();
  renderApp();
  await user.click(within(await cardFor("prod-eu")).getByRole("button", { name: "Register" }));
  const script = (await screen.findByLabelText("Registration command")).textContent!;
  expect(script).toContain("kubectl -n c-m-1 get secret crt-token-a -o jsonpath='{.data.token}'");
  expect(script).toContain("--token ${TOKEN} --etcd --controlplane --worker");
  expect(script).not.toContain("{token}");
});

test("import URL placeholder is braced so the shell does not swallow the suffix", () => {
  const s = registrationScript(
    { command: "kubectl apply -f https://r/v3/import/{token}_c-2.yaml", token: { namespace: "c-2", name: "t", key: "token" } },
    false,
  );
  expect(s.split("\n")[1]).toBe("kubectl apply -f https://r/v3/import/${TOKEN}_c-2.yaml");
});

test("restore needs the cluster name typed and sends an idempotency key", async () => {
  const user = userEvent.setup();
  renderApp();
  await user.click(within(await cardFor("prod-eu")).getByRole("button", { name: "Snapshot" }));
  await user.click(await screen.findByRole("button", { name: "Restore prod-eu-etcd-1" }));
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
  await user.click(within(await cardFor("prod-eu")).getByRole("button", { name: "Nodes" }));
  await user.click(await screen.findByRole("button", { name: "Remove cp-0" }));
  await user.type(screen.getByLabelText("Cluster name confirmation"), "cp-0");
  await user.click(screen.getByRole("button", { name: "Remove node" }));
  const call = fetchMock.mock.calls.find(([, i]) => i?.method === "DELETE")!;
  expect(call[0]).toBe("/api/v1/clusters/prod-eu/nodes/cp-0");
});

test("connect builds a credential-free kubeconfig that logs in via the user's IdP", () => {
  const a: Access = {
    servers: ["https://203.0.113.5:6443"],
    certificateAuthorityData: "Q0E=",
    oidc: { issuerURL: "https://idp.example", clientID: "kubestack" },
  };
  const k = kubeconfig("prod-eu", a);
  expect(k).toContain("server: https://203.0.113.5:6443");
  expect(k).toContain("certificate-authority-data: Q0E=");
  expect(k).toContain("--oidc-issuer-url=https://idp.example");
  expect(k).toContain("apiVersion: client.authentication.k8s.io/v1");
  expect(k).toContain("command: kubectl");
  expect(k).toContain("args: [oidc-login, get-token, --oidc-issuer-url=https://idp.example, --oidc-client-id=kubestack,");
  expect(k).toContain("interactiveMode: IfAvailable");
  expect(k).toContain("current-context: prod-eu");
  expect(k).not.toMatch(/token:|client-key|client-certificate|password|auth-provider|username:/);
});

test("kubeconfig omits the CA when the cluster has none (system trust store)", () => {
  const a: Access = {
    servers: ["https://10.0.0.1:6443", "https://10.0.0.2:6443"],
    certificateAuthorityData: "",
    oidc: { issuerURL: "https://idp.example", clientID: "cli" },
  };
  const k = kubeconfig("dev", a);
  expect(k).not.toContain("certificate-authority-data");
  expect(k).toContain("server: https://10.0.0.1:6443");
  expect(k).toContain("--oidc-client-id=cli");
});
