// Copyright 2026 The KubeStack Authors. All rights reserved.
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, type Cluster, type Me, type Operation, type Registration } from "./api";

function ErrorBox({ error }: { error: unknown }) {
  if (!error) return null;
  const e = error as ApiError;
  return (
    <div role="alert" className="rounded border border-red-300 bg-red-50 p-3 text-sm text-red-800">
      <p className="font-medium">{e.message}</p>
      {e.details?.length > 0 && (
        <ul className="mt-1 list-disc pl-5">
          {e.details.map((d) => (
            <li key={d}>{d}</li>
          ))}
        </ul>
      )}
    </div>
  );
}

const btn = "rounded px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-40";

function useInvalidate() {
  const qc = useQueryClient();
  return () => {
    qc.invalidateQueries({ queryKey: ["clusters"] });
    qc.invalidateQueries({ queryKey: ["audit"] });
  };
}

function CreateForm({ versions }: { versions: string[] }) {
  const [name, setName] = useState("");
  const [version, setVersion] = useState("");
  const done = useInvalidate();
  const m = useMutation({ mutationFn: () => api.create(name, version || versions.at(-1)!), onSuccess: () => { setName(""); done(); } });
  return (
    <form className="flex flex-wrap items-end gap-3" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
      <label className="text-sm">
        <span className="block font-medium">Name</span>
        <input required pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?" maxLength={63} value={name} onChange={(e) => setName(e.target.value)}
          className="mt-1 rounded border border-slate-300 px-2 py-1.5" />
      </label>
      <label className="text-sm">
        <span className="block font-medium">Kubernetes version</span>
        <select value={version || versions.at(-1)} onChange={(e) => setVersion(e.target.value)} className="mt-1 rounded border border-slate-300 px-2 py-1.5">
          {versions.map((v) => <option key={v}>{v}</option>)}
        </select>
      </label>
      <button disabled={m.isPending} className={`${btn} bg-indigo-600 text-white`}>Create RKE2 cluster</button>
      <div className="w-full"><ErrorBox error={m.error} /></div>
    </form>
  );
}

function ImportForm() {
  const [name, setName] = useState("");
  const done = useInvalidate();
  const m = useMutation({ mutationFn: () => api.importCluster(name), onSuccess: () => { setName(""); done(); } });
  return (
    <form className="flex flex-wrap items-end gap-3" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
      <label className="text-sm">
        <span className="block font-medium">Existing cluster name</span>
        <input required pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?" maxLength={63} value={name} onChange={(e) => setName(e.target.value)}
          className="mt-1 rounded border border-slate-300 px-2 py-1.5" />
      </label>
      <button disabled={m.isPending} className={`${btn} bg-slate-700 text-white`}>Import cluster</button>
      <div className="w-full"><ErrorBox error={m.error} /></div>
    </form>
  );
}

/** Shell snippet: the operator reads the token with their own kubectl credentials; KubeStack never sees it. */
export function registrationScript(r: Registration, provisioned: boolean): string {
  const read = `TOKEN=$(kubectl -n ${r.token.namespace} get secret ${r.token.name} -o jsonpath='{.data.${r.token.key}}' | base64 -d)`;
  const cmd = r.command.split("{token}").join("${TOKEN}") + (provisioned ? " --etcd --controlplane --worker" : "");
  return `${read}\n${cmd}`;
}

function RegisterDialog({ c, onClose }: { c: Cluster; onClose: () => void }) {
  const q = useQuery({ queryKey: ["registration", c.id], queryFn: () => api.registration(c.id) });
  const provisioned = c.tier === "provisioned";
  return (
    <Dialog title={provisioned ? `Add nodes to ${c.name}` : `Connect ${c.name}`} onClose={onClose}>
      <p className="text-sm">
        {provisioned
          ? "Run on each new Linux node (as root). Drop the role flags a node should not have."
          : "Run against the existing cluster with a kubeconfig that can create cluster-scoped resources."}
        {" "}The first line reads the join token from Rancher's local cluster with your own credentials.
      </p>
      <ErrorBox error={q.error} />
      {q.data && <pre aria-label="Registration command" className="overflow-x-auto whitespace-pre-wrap break-all rounded bg-slate-900 p-3 text-xs text-slate-100">{registrationScript(q.data, provisioned)}</pre>}
    </Dialog>
  );
}

/** Destructive confirm: the button enables only once the exact name is typed (rule 17). */
function TypeToConfirm({ name, label, action, pending, onConfirm }: { name: string; label: string; action: string; pending: boolean; onConfirm: () => void }) {
  const [typed, setTyped] = useState("");
  return (
    <div className="flex flex-col gap-2 rounded border border-red-200 bg-red-50 p-3">
      <p className="text-sm">Type <code>{name}</code> to confirm.</p>
      <input aria-label={label} value={typed} onChange={(e) => setTyped(e.target.value)} className="rounded border border-slate-300 px-2 py-1.5" />
      <button disabled={typed !== name || pending} onClick={onConfirm} className={`${btn} bg-red-600 text-white`}>{action}</button>
    </div>
  );
}

function BackupsDialog({ c, admin, onClose }: { c: Cluster; admin: boolean; onClose: () => void }) {
  const q = useQuery({ queryKey: ["snapshots", c.id], queryFn: () => api.snapshots(c.id) });
  const qc = useQueryClient();
  const done = useInvalidate();
  const [selected, setSelected] = useState<string | null>(null);
  const take = useMutation({ mutationFn: () => api.takeSnapshot(c.id), onSuccess: () => { done(); qc.invalidateQueries({ queryKey: ["snapshots", c.id] }); } });
  const restore = useMutation({ mutationFn: (s: string) => api.restore(c.id, s), onSuccess: () => { done(); onClose(); } });
  return (
    <Dialog title={`etcd snapshots of ${c.name}`} onClose={onClose}>
      <ErrorBox error={q.error ?? take.error ?? restore.error} />
      <ul className="max-h-60 divide-y divide-slate-100 overflow-y-auto text-sm">
        {q.data?.map((s) => (
          <li key={s.name} className="flex items-center justify-between gap-2 py-1.5">
            <span><span className="font-mono">{s.name}</span> <time className="text-slate-500">{new Date(s.createdAt).toLocaleString()}</time>{!s.successful && <span className="text-red-700"> failed</span>}</span>
            {admin && <button disabled={!s.successful || !c.operations.restore.allowed} title={c.operations.restore.reason} onClick={() => setSelected(s.name)} className={`${btn} bg-red-100 text-red-900`}>Restore…</button>}
          </li>
        ))}
        {q.data?.length === 0 && <li className="py-2 text-slate-500">No snapshots yet.</li>}
      </ul>
      {admin && <button disabled={!c.operations.snapshot.allowed || take.isPending} title={c.operations.snapshot.reason} onClick={() => take.mutate()} className={`${btn} bg-indigo-600 text-white`}>Take snapshot now</button>}
      {selected && (
        <>
          <p className="text-sm">Restoring <code>{selected}</code> replaces all etcd data in {c.name}; changes since the snapshot are lost. The cluster restarts.</p>
          <TypeToConfirm name={c.name} label="Cluster name confirmation" action="Restore snapshot" pending={restore.isPending} onConfirm={() => restore.mutate(selected)} />
        </>
      )}
    </Dialog>
  );
}

function NodesDialog({ c, admin, onClose }: { c: Cluster; admin: boolean; onClose: () => void }) {
  const q = useQuery({ queryKey: ["nodes", c.id], queryFn: () => api.nodes(c.id) });
  const qc = useQueryClient();
  const done = useInvalidate();
  const [selected, setSelected] = useState<string | null>(null);
  const remove = useMutation({
    mutationFn: (n: string) => api.removeNode(c.id, n),
    onSuccess: () => { done(); setSelected(null); qc.invalidateQueries({ queryKey: ["nodes", c.id] }); },
  });
  return (
    <Dialog title={`Nodes of ${c.name}`} onClose={onClose}>
      <ErrorBox error={q.error ?? remove.error} />
      <ul className="max-h-60 divide-y divide-slate-100 overflow-y-auto text-sm">
        {q.data?.map((n) => (
          <li key={n.name} className="flex items-center justify-between gap-2 py-1.5">
            <span><span className="font-mono">{n.name}</span> <span className="text-slate-500">{n.roles.join(", ")}</span> {n.ready ? "Ready" : <span className="text-red-700">Not ready</span>}</span>
            {admin && <button disabled={!c.operations.removeNode.allowed} title={c.operations.removeNode.reason} onClick={() => setSelected(n.name)} className={`${btn} bg-red-100 text-red-900`}>Remove…</button>}
          </li>
        ))}
        {q.data?.length === 0 && <li className="py-2 text-slate-500">No nodes registered yet.</li>}
      </ul>
      {selected && (
        <>
          <p className="text-sm">Rancher drains <code>{selected}</code> and removes it from {c.name}.</p>
          <TypeToConfirm name={selected} label="Node name confirmation" action="Remove node" pending={remove.isPending} onConfirm={() => remove.mutate(selected)} />
        </>
      )}
    </Dialog>
  );
}

function UpgradeDialog({ c, versions, onClose }: { c: Cluster; versions: string[]; onClose: () => void }) {
  const [target, setTarget] = useState(versions.find((v) => v > c.kubernetesVersion) ?? versions.at(-1)!);
  const [confirmed, setConfirmed] = useState(false);
  const done = useInvalidate();
  const m = useMutation({ mutationFn: () => api.upgrade(c.id, target), onSuccess: () => { done(); onClose(); } });
  return (
    <Dialog title={`Upgrade ${c.name}`} onClose={onClose}>
      <p className="text-sm">Current: <code>{c.kubernetesVersion}</code>. Pre-flight checks (version skew, etcd snapshot, node health) run on the server and cannot be skipped.</p>
      <select value={target} onChange={(e) => setTarget(e.target.value)} className="rounded border border-slate-300 px-2 py-1.5">
        {versions.map((v) => <option key={v}>{v}</option>)}
      </select>
      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} />
        I understand this upgrades every node in {c.name}.
      </label>
      <ErrorBox error={m.error} />
      <button disabled={!confirmed || m.isPending} onClick={() => m.mutate()} className={`${btn} bg-amber-600 text-white`}>Start upgrade</button>
    </Dialog>
  );
}

function DeleteDialog({ c, onClose }: { c: Cluster; onClose: () => void }) {
  const [typed, setTyped] = useState("");
  const done = useInvalidate();
  const m = useMutation({ mutationFn: () => api.remove(c.id), onSuccess: () => { done(); onClose(); } });
  const attached = c.tier === "attached";
  return (
    <Dialog title={`${attached ? "Disconnect" : "Delete"} ${c.name}`} onClose={onClose}>
      <p className="text-sm">
        {attached ? "This removes the cluster from KubeStack; the cluster and its workloads keep running." : "This destroys the cluster and its workloads."}
        {" "}Type <code>{c.name}</code> to confirm.
      </p>
      <input aria-label="Cluster name confirmation" value={typed} onChange={(e) => setTyped(e.target.value)} className="rounded border border-slate-300 px-2 py-1.5" />
      <ErrorBox error={m.error} />
      <button disabled={typed !== c.name || m.isPending} onClick={() => m.mutate()} className={`${btn} bg-red-600 text-white`}>{attached ? "Disconnect cluster" : "Delete cluster"}</button>
    </Dialog>
  );
}

function Dialog({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  return (
    <div className="fixed inset-0 flex items-center justify-center bg-black/30 p-4" onClick={onClose}>
      <div role="dialog" aria-modal="true" aria-label={title} className="flex w-full max-w-md flex-col gap-3 rounded-lg bg-white p-5 shadow-xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex justify-between">
          <h2 className="text-lg font-semibold">{title}</h2>
          <button aria-label="Close" onClick={onClose}>✕</button>
        </div>
        {children}
      </div>
    </div>
  );
}

function OpButton({ c, op, label, onClick, cls }: { c: Cluster; op: Operation; label: string; onClick: () => void; cls: string }) {
  const p = c.operations[op];
  return (
    <button disabled={!p.allowed} title={p.reason} aria-description={p.reason} onClick={onClick} className={`${btn} ${cls}`}>{label}</button>
  );
}

export default function App() {
  const me = useQuery({ queryKey: ["me"], queryFn: api.me });
  if (me.error && (me.error as ApiError).status === 401) {
    return (
      <main className="mx-auto flex max-w-md flex-col items-center gap-4 p-12 text-center">
        <h1 className="text-2xl font-bold">KubeStack</h1>
        <p className="text-sm text-slate-600">Sign in with your organisation account to continue.</p>
        <a href="/auth/login" className={`${btn} bg-indigo-600 text-white`}>Sign in</a>
      </main>
    );
  }
  if (!me.data) return <main className="p-6"><ErrorBox error={me.error} /></main>;
  return <Console me={me.data} />;
}

function Console({ me }: { me: Me }) {
  const clusters = useQuery({ queryKey: ["clusters"], queryFn: api.clusters });
  const versions = useQuery({ queryKey: ["versions"], queryFn: api.versions, staleTime: Infinity });
  const audit = useQuery({ queryKey: ["audit"], queryFn: api.audit });
  const [dialog, setDialog] = useState<{ kind: Operation; c: Cluster } | null>(null);
  const vs = versions.data ?? [];

  return (
    <main className="mx-auto flex max-w-6xl flex-col gap-8 p-6">
      <header className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">KubeStack</h1>
        <div className="flex items-center gap-3 text-sm">
          <span>{me.name}{!me.admin && " (read-only)"}</span>
          <button className={`${btn} bg-slate-200`} onClick={async () => { await api.logout(); window.location.reload(); }}>Sign out</button>
        </div>
      </header>

      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold">Clusters</h2>
        <ErrorBox error={clusters.error} />
        <div className="overflow-x-auto rounded border border-slate-200 bg-white">
          <table className="w-full text-left text-sm">
            <thead className="bg-slate-100">
              <tr>{["Name", "Tier", "Upgrades owned by", "Kubernetes", "Status", ""].map((h) => <th key={h} className="px-3 py-2">{h}</th>)}</tr>
            </thead>
            <tbody>
              {clusters.data?.map((c) => (
                <tr key={c.id} className="border-t border-slate-100">
                  <td className="px-3 py-2 font-medium">{c.name}</td>
                  <td className="px-3 py-2">{c.tier}</td>
                  <td className="px-3 py-2">{c.upgradeOwner}</td>
                  <td className="px-3 py-2 font-mono">{c.kubernetesVersion}</td>
                  <td className="px-3 py-2">{c.ready ? "Ready" : "Not ready"}</td>
                  <td className="flex justify-end gap-2 px-3 py-2">
                    <OpButton c={c} op="upgrade" label="Upgrade" cls="bg-amber-100 text-amber-900" onClick={() => setDialog({ kind: "upgrade", c })} />
                    {c.tier === "provisioned" && <>
                      <button className={`${btn} bg-slate-100`} onClick={() => setDialog({ kind: "snapshot", c })}>Backups</button>
                      <button className={`${btn} bg-slate-100`} onClick={() => setDialog({ kind: "removeNode", c })}>Nodes</button>
                    </>}
                    <OpButton c={c} op="register" label={c.tier === "attached" ? "Connect" : "Add nodes"} cls="bg-slate-100" onClick={() => setDialog({ kind: "register", c })} />
                    <OpButton c={c} op="delete" label={c.tier === "attached" ? "Disconnect" : "Delete"} cls="bg-red-100 text-red-900" onClick={() => setDialog({ kind: "delete", c })} />
                  </td>
                </tr>
              ))}
              {clusters.data?.length === 0 && <tr><td colSpan={6} className="px-3 py-6 text-center text-slate-500">No clusters yet.</td></tr>}
            </tbody>
          </table>
        </div>
        {me.admin && vs.length > 0 && <CreateForm versions={vs} />}
        {me.admin && <ImportForm />}
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold">Audit trail</h2>
        <ul className="divide-y divide-slate-100 rounded border border-slate-200 bg-white text-sm">
          {audit.data?.map((r) => (
            <li key={r.id} className="flex flex-wrap gap-x-4 px-3 py-2">
              <time className="text-slate-500">{new Date(r.time).toLocaleString()}</time>
              <span>{r.actor}</span>
              <span className="font-medium">{r.action}</span>
              <span>{r.clusterId}</span>
              <span className={r.result === "failure" ? "text-red-700" : ""}>{r.result}{r.error && `: ${r.error}`}</span>
            </li>
          ))}
        </ul>
      </section>

      {dialog?.kind === "upgrade" && <UpgradeDialog c={dialog.c} versions={vs} onClose={() => setDialog(null)} />}
      {dialog?.kind === "snapshot" && <BackupsDialog c={dialog.c} admin={me.admin} onClose={() => setDialog(null)} />}
      {dialog?.kind === "removeNode" && <NodesDialog c={dialog.c} admin={me.admin} onClose={() => setDialog(null)} />}
      {dialog?.kind === "register" && <RegisterDialog c={dialog.c} onClose={() => setDialog(null)} />}
      {dialog?.kind === "delete" && <DeleteDialog c={dialog.c} onClose={() => setDialog(null)} />}
    </main>
  );
}
