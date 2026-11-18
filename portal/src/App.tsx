// Copyright 2026 The KubeStack Authors. All rights reserved.
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, type Cluster } from "./api";

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
  return (
    <Dialog title={`Delete ${c.name}`} onClose={onClose}>
      <p className="text-sm">This destroys the cluster and its workloads. Type <code>{c.name}</code> to confirm.</p>
      <input aria-label="Cluster name confirmation" value={typed} onChange={(e) => setTyped(e.target.value)} className="rounded border border-slate-300 px-2 py-1.5" />
      <ErrorBox error={m.error} />
      <button disabled={typed !== c.name || m.isPending} onClick={() => m.mutate()} className={`${btn} bg-red-600 text-white`}>Delete cluster</button>
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

function OpButton({ c, op, label, onClick, cls }: { c: Cluster; op: "upgrade" | "delete"; label: string; onClick: () => void; cls: string }) {
  const p = c.operations[op];
  return (
    <button disabled={!p.allowed} title={p.reason} aria-description={p.reason} onClick={onClick} className={`${btn} ${cls}`}>{label}</button>
  );
}

export default function App() {
  const clusters = useQuery({ queryKey: ["clusters"], queryFn: api.clusters });
  const versions = useQuery({ queryKey: ["versions"], queryFn: api.versions, staleTime: Infinity });
  const audit = useQuery({ queryKey: ["audit"], queryFn: api.audit });
  const [dialog, setDialog] = useState<{ kind: "upgrade" | "delete"; c: Cluster } | null>(null);
  const vs = versions.data ?? [];

  return (
    <main className="mx-auto flex max-w-6xl flex-col gap-8 p-6">
      <h1 className="text-2xl font-bold">KubeStack</h1>

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
                    <OpButton c={c} op="delete" label="Delete" cls="bg-red-100 text-red-900" onClick={() => setDialog({ kind: "delete", c })} />
                  </td>
                </tr>
              ))}
              {clusters.data?.length === 0 && <tr><td colSpan={6} className="px-3 py-6 text-center text-slate-500">No clusters yet.</td></tr>}
            </tbody>
          </table>
        </div>
        {vs.length > 0 && <CreateForm versions={vs} />}
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
      {dialog?.kind === "delete" && <DeleteDialog c={dialog.c} onClose={() => setDialog(null)} />}
    </main>
  );
}
