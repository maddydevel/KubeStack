// Copyright 2026 The KubeStack Authors. All rights reserved.
import { useState, useEffect } from "react";
import { api, registrationScript, type Cluster, type Access, type Operation } from "../api";
import { StatusIndicator } from "./status-indicator";

type ClusterCardProps = { c: Cluster; versions: string[]; className?: string };

const OP_LABELS: Record<Operation, string> = {
  upgrade: "Upgrade",
  delete: "Delete",
  register: "Register",
  snapshot: "Snapshot",
  restore: "Restore",
  removeNode: "Remove node",
  access: "Access",
};

export default function ClusterCard({ c, versions, className }: ClusterCardProps) {
  const [open, setOpen] = useState<Operation | null>(null);
  const tier = c.tier === "provisioned" ? "provisioned" : c.tier === "hosted" ? "hosted" : "attached";

  return (
    <article className={`surface surface-hover group p-4 ${className ?? ""}`}>
      <div className="flex flex-wrap items-start justify-between gap-2 mb-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-baseline gap-2">
            <h3 className="text-base font-signature truncate">{c.name}</h3>
            <span className="pill pill-tier">{tier}</span>
            {c.upgradeOwner !== "none" && <span className="pill pill-accent">{c.upgradeOwner}</span>}
          </div>
          <div className="flex items-center gap-2 mt-1.5">
            <span className="text-xs font-mono text-text-tertiary">{c.kubernetesVersion}</span>
            <span className="w-px h-3 bg-white/10 mx-1.5"></span>
            <StatusIndicator ready={c.ready} />
          </div>
        </div>
        <div className="flex items-end gap-1">
          {/* Quick-toggle icon buttons — only for ops without a full button below */}
        {c.operations.register && (
          <button
            disabled={!c.operations.register.allowed}
            title={c.operations.register.reason}
            aria-label="Register"
            onClick={() => setOpen("register")}
            className={`rounded p-1.5 text-xs transition-colors ${
              c.operations.register.allowed
                ? "text-text-quaternary hover:text-accent-hover hover:bg-accent/10"
                : "text-text-quaternary opacity-50"
            }`}
          >
            <span aria-hidden="true">⊕</span>
          </button>
        )}
        </div>
      </div>

      <div className="flex flex-wrap gap-1.5 mb-3">
        {["upgrade", "snapshot", "restore", "removeNode", "access", "register"].map((op) => {
          const o = op as Operation;
          const p = c.operations[o];
          if (!p) return null;
          return (
            <span
              key={op}
              className={`rounded border px-2 py-0.5 text-[10px] font-signature ${
                p.allowed
                  ? "border-white/10 text-text-tertiary"
                  : "border-white/5 text-text-quaternary opacity-50"
              }`}
              title={p.allowed ? OP_LABELS[o] : p.reason || "Not permitted"}
            >
              {OP_LABELS[o]}
            </span>
          );
        })}
      </div>

      <div className="flex flex-wrap gap-2">
        {c.operations.access && (
          <button
            disabled={!c.operations.access.allowed}
            title={c.operations.access.reason}
            aria-label="Connect"
            onClick={() => setOpen("access")}
            className={`rounded px-3 py-1.5 text-xs flex items-center gap-1.5 transition-colors ${
              c.operations.access.allowed
                ? "btn-ghost hover:bg-accent/10"
                : "text-text-quaternary opacity-50"
            }`}
          >
            <span aria-hidden="true">↗</span> Connect
          </button>
        )}
        {c.operations.upgrade && versions.length > 0 && (
          <button
            disabled={!c.operations.upgrade.allowed}
            title={c.operations.upgrade.reason}
            aria-label="Upgrade"
            onClick={() => setOpen("upgrade")}
            className={`rounded px-3 py-1.5 text-xs flex items-center gap-1.5 transition-colors ${
              c.operations.upgrade.allowed ? "btn-brand" : "text-text-quaternary opacity-50"
            }`}
          >
            <span aria-hidden="true">↟</span> Upgrade
          </button>
        )}
        {c.operations.snapshot && (
          <button
            disabled={!c.operations.snapshot.allowed}
            title={c.operations.snapshot.reason}
            aria-label="Snapshot"
            onClick={() => setOpen("snapshot")}
            className={`rounded px-3 py-1.5 text-xs flex items-center gap-1.5 transition-colors ${
              c.operations.snapshot.allowed
                ? "btn-ghost hover:bg-accent/10"
                : "text-text-quaternary opacity-50"
            }`}
          >
            <span aria-hidden="true">⟳</span> Snapshot
          </button>
        )}
        {c.operations.removeNode && (
          <button
            disabled={!c.operations.removeNode.allowed}
            title={c.operations.removeNode.reason}
            aria-label="Nodes"
            onClick={() => setOpen("removeNode")}
            className={`rounded px-3 py-1.5 text-xs flex items-center gap-1.5 transition-colors ${
              c.operations.removeNode.allowed
                ? "btn-ghost hover:bg-accent/10"
                : "text-text-quaternary opacity-50"
            }`}
          >
            <span aria-hidden="true">−</span> Nodes
          </button>
        )}
        {open === "upgrade" && (
          <UpgradeInline c={c} versions={versions} onClose={() => setOpen(null)} />
        )}
        {open === "register" && (
          <RegisterInline c={c} onClose={() => setOpen(null)} />
        )}
        {open === "access" && (
          <AccessInline c={c} />
        )}
        {open === "snapshot" && (
          <SnapshotInline c={c} onClose={() => setOpen(null)} />
        )}
        {open === "removeNode" && (
          <NodesInline c={c} onClose={() => setOpen(null)} />
        )}
        <button
          disabled={!c.operations.delete.allowed}
          title={c.operations.delete.reason}
          onClick={() => setOpen("delete")}
          className="rounded px-3 py-1.5 text-xs text-danger hover:bg-danger-subtle transition-colors border border-transparent hover:border-danger/30"
        >
          {c.tier === "attached" ? "Disconnect" : "Delete"}
        </button>
      </div>

      {open === "delete" && <DeleteConfirm c={c} onClose={() => setOpen(null)} />}
    </article>
  );
}

function UpgradeInline({ c, versions, onClose }: { c: Cluster; versions: string[]; onClose: () => void }) {
  const [target, setTarget] = useState(versions.find((v) => v > c.kubernetesVersion) ?? versions.at(-1)!);
  const [confirmed, setConfirmed] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  return (
    <div className="mt-3 surface p-3 rounded-lg border border-white/5">
      <div className="flex items-baseline gap-2 mb-2">
        <span className="text-xs font-signature">Upgrade to</span>
        <select
          value={target}
          onChange={(e) => setTarget(e.target.value)}
          className="rounded bg-white/5 px-2 py-1 text-xs border border-white/5 text-text-secondary"
        >
          {versions.map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
        </select>
      </div>
      <p className="text-[10px] text-text-tertiary mb-2">Pre-flight (skew, snapshot, nodes) runs on the server.</p>
      <label className="flex items-center gap-2 text-[10px]">
        <input
          type="checkbox"
          checked={confirmed}
          onChange={(e) => setConfirmed(e.target.checked)}
          className="rounded bg-white/5 border border-white/5 text-accent"
        />
        I understand this upgrades every node in {c.name}.
      </label>
      {error && <p className="text-[10px] text-danger mt-1">{error}</p>}
      <div className="flex justify-end gap-2 mt-2">
        <button onClick={onClose} className="btn-ghost px-2 py-1 text-[10px]">
          Cancel
        </button>
        <button
          disabled={!confirmed || pending}
          onClick={async () => {
            setPending(true);
            try {
              await api.upgrade(c.id, target);
              onClose();
            } catch (e) {
              const detail = (e as { details?: string[] })?.details?.[0];
              setError(detail ? `Upgrade failed: ${detail}` : "Upgrade failed");
              setPending(false);
            }
          }}
          className="btn-brand px-3 py-1.5 text-[10px] disabled:opacity-50"
        >
          {pending ? "…" : "Start upgrade"}
        </button>
      </div>
    </div>
  );
}

function AccessInline({ c }: { c: Cluster }) {
  const [showing, setShowing] = useState(false);
  const [access, setAccess] = useState<Access | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  useEffect(() => {
    if (!showing) return;
    setPending(true);
    api.access(c.id)
      .then(setAccess)
      .catch((e: Error) => setErr(e.message ?? "Access not available"))
      .finally(() => setPending(false));
  }, [showing, c.id]);

  return (
    <div className="mt-3 surface p-3 rounded-lg border border-white/5">
      <button
        onClick={() => {
          setShowing(!showing);
          if (showing) setErr(null);
        }}
        className="w-full btn-ghost px-3 py-1.5 text-xs flex items-center justify-between"
      >
        <span className="flex items-center gap-1.5">
          <span>↗</span> Access
        </span>
        {showing ? <span>▲</span> : <span>▼</span>}
      </button>
      {showing && (
        <div className="mt-2 surface p-2 rounded border border-white/5">
          {pending && (
            <div className="flex items-center justify-center py-3">
              <span className="text-[10px] text-text-tertiary">Loading access info…</span>
            </div>
          )}
          {!pending && err && (
            <div className="flex items-center gap-2 p-3 bg-danger/10 border border-danger/20 rounded-md">
              <span className="text-[10px] text-danger">{err}</span>
            </div>
          )}
          {!pending && access && (
            <div className="space-y-2">
              <p className="text-[10px] text-text-tertiary">
                kubectl signs you in with your IdP via kubelogin. No credentials stored.
              </p>
              <div className="flex flex-wrap gap-2 mb-2">
                <span className="rounded bg-white/5 px-2 py-1 text-xs border border-white/5 text-text-tertiary font-mono">
                  {access.servers.join(", ")}
                </span>
                {access.oidc && (
                  <span className="rounded bg-white/5 px-2 py-1 text-xs border border-white/5 text-text-tertiary font-mono">
                    {access.oidc.issuerURL}
                  </span>
                )}
              </div>
              <button
                onClick={() => {
                  const issuer = access.oidc?.issuerURL ?? "";
                  const clientID = access.oidc?.clientID ?? "";
                  navigator.clipboard.writeText(
                    `kubectl oidc-login get-token --issuer ${issuer} --oidc-client-id ${clientID}`
                  );
                }}
                className="w-full btn-brand px-3 py-1.5 text-[10px]"
              >
                Copy kubectl login command
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function RegisterInline({ c, onClose }: { c: Cluster; onClose: () => void }) {
  const [reg, setReg] = useState<{ command: string; token: { namespace: string; name: string; key: string } } | null>(null);

  useEffect(() => {
    api.registration(c.id).then(setReg).catch(() => setReg(null));
  }, [c.id]);

  if (!reg) {
    return (
      <div className="mt-3 surface p-3 rounded-lg border border-white/5">
        <p className="text-[10px] text-text-tertiary">Loading registration command…</p>
        <div className="flex justify-end">
          <button onClick={onClose} className="btn-ghost px-2 py-1 text-[10px]">Close</button>
        </div>
      </div>
    );
  }

  const script = registrationScript(reg, c.tier === "provisioned");

  return (
    <div className="mt-3 surface p-3 rounded-lg border border-white/5">
      <div className="flex items-center justify-between mb-2">
        <span className="text-xs font-signature">Registration command</span>
        <button onClick={onClose} className="btn-ghost px-2 py-1 text-[10px]">Close</button>
      </div>
      <p className="text-[10px] text-text-tertiary mb-2">
        Run on each node. The first line reads the join token from the cluster's Secret with your own kubectl credentials.
      </p>
      <pre aria-label="Registration command" className="overflow-x-auto whitespace-pre-wrap break-all rounded bg-slate-900 p-3 text-xs text-slate-100 mb-2">
        {script}
      </pre>
      <div className="flex justify-end">
        <button onClick={onClose} className="btn-ghost px-2 py-1 text-[10px]">Close</button>
      </div>
    </div>
  );
}

function SnapshotInline({ c, onClose }: { c: Cluster; onClose: () => void }) {
  const [snaps, setSnaps] = useState<Array<{ name: string; createdAt: string; successful: boolean }>>([]);
  const [pending, setPending] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [confirmTyped, setConfirmTyped] = useState("");
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    api.snapshots(c.id).then(setSnaps).catch(() => setSnaps([]));
  }, [c.id]);

  return (
    <div className="mt-3 surface p-3 rounded-lg border border-white/5">
      <div className="flex items-center justify-between mb-2">
        <span className="text-xs font-signature">etcd snapshots</span>
        <button
          disabled={pending}
          onClick={async () => {
            setPending(true);
            try {
              await api.takeSnapshot(c.id);
              const s = await api.snapshots(c.id);
              setSnaps(s);
              setPending(false);
            } catch {
              setPending(false);
            }
          }}
          className="btn-brand px-3 py-1.5 text-[10px] disabled:opacity-50"
        >
          {pending ? "…" : "Take snapshot"}
        </button>
      </div>
      <ul className="max-h-40 divide-y divide-white/5 overflow-y-auto text-sm mb-2">
        {snaps.map((s) => (
          <li key={s.name} className="flex items-center justify-between gap-2 px-2 py-1.5">
            <span className="flex items-center gap-2">
              <span className="font-mono text-[10px]">{s.name}</span>
              <span className="text-[10px] text-text-tertiary">{new Date(s.createdAt).toLocaleDateString()}</span>
              {s.successful ? <span className="text-[10px] text-success">successful</span> : <span className="text-[10px] text-danger">failed</span>}
            </span>
            {s.successful && (
              <button
                onClick={() => setSelected(s.name)}
                className="rounded px-2 py-0.5 text-[10px] text-text-quaternary hover:text-accent hover:bg-accent/10"
                aria-label={`Restore ${s.name}`}
              >
                Restore…
              </button>
            )}
          </li>
        ))}
        {snaps.length === 0 && <li className="px-2 py-2 text-[10px] text-text-quaternary">No snapshots yet.</li>}
      </ul>
      {selected && (
        <div className="mt-2 surface p-2 rounded border border-white/5">
          <p className="text-[10px] text-text-tertiary mb-1">
            Restoring <code className="font-mono">{selected}</code> replaces all etcd data in {c.name}.
          </p>
          <input
            value={confirmTyped}
            onChange={(e) => setConfirmTyped(e.target.value)}
            className="w-full rounded bg-white/5 px-2 py-1 text-xs border border-white/5 text-text-secondary"
            placeholder={c.name}
            aria-label="Cluster name confirmation"
          />
          {err && <p className="text-[10px] text-danger mt-1">{err}</p>}
          <div className="flex justify-end gap-2 mt-2">
            <button onClick={() => setSelected(null)} className="btn-ghost px-2 py-1 text-[10px]">
              Cancel
            </button>
            <button
              disabled={confirmTyped !== c.name}
              onClick={async () => {
                try {
                  await api.restore(c.id, selected!);
                  onClose();
                } catch (e) {
                  setErr((e as { message?: string })?.message ?? "Restore failed");
                }
              }}
              className="btn-danger px-3 py-1.5 text-[10px] disabled:opacity-50"
            >
              Restore snapshot
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function NodesInline({ c, onClose }: { c: Cluster; onClose: () => void }) {
  const [nodes, setNodes] = useState<Array<{ name: string; ready: boolean; roles: string[] }>>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [confirmTyped, setConfirmTyped] = useState("");
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    api.nodes(c.id).then(setNodes).catch(() => setNodes([]));
  }, [c.id]);

  return (
    <div className="mt-3 surface p-3 rounded-lg border border-white/5">
      <div className="flex items-center justify-between mb-2">
        <span className="text-xs font-signature">Nodes</span>
        <button onClick={onClose} className="btn-ghost px-2 py-1 text-[10px]">
          Close
        </button>
      </div>
      <ul className="max-h-40 divide-y divide-white/5 overflow-y-auto text-sm mb-2">
        {nodes.map((n) => (
          <li key={n.name} className="flex items-center justify-between gap-2 px-2 py-1.5">
            <span className="flex items-center gap-2">
              <span className="font-mono text-[10px]">{n.name}</span>
              <span className="text-[10px] text-text-tertiary">{n.roles.join(", ")}</span>
              {n.ready ? <span className="text-[10px] text-success">Ready</span> : <span className="text-[10px] text-danger">Not ready</span>}
            </span>
            <button
              onClick={() => setSelected(n.name)}
              className="rounded px-2 py-0.5 text-[10px] text-text-quaternary hover:text-danger hover:bg-danger-subtle"
              aria-label={`Remove ${n.name}`}
            >
              Remove…
            </button>
          </li>
        ))}
        {nodes.length === 0 && <li className="px-2 py-2 text-[10px] text-text-quaternary">No nodes registered yet.</li>}
      </ul>
      {selected && (
        <div className="mt-2 surface p-2 rounded border border-white/5">
          <p className="text-[10px] text-text-tertiary mb-1">
            Removing <code className="font-mono">{selected}</code> from {c.name}. The node will be drained first.
          </p>
          <input
            value={confirmTyped}
            onChange={(e) => setConfirmTyped(e.target.value)}
            className="w-full rounded bg-white/5 px-2 py-1 text-xs border border-white/5 text-text-secondary"
            placeholder={selected}
            aria-label="Cluster name confirmation"
          />
          {err && <p className="text-[10px] text-danger mt-1">{err}</p>}
          <div className="flex justify-end gap-2 mt-2">
            <button onClick={() => setSelected(null)} className="btn-ghost px-2 py-1 text-[10px]">
              Cancel
            </button>
            <button
              disabled={confirmTyped !== selected}
              onClick={async () => {
                try {
                  await api.removeNode(c.id, selected!);
                  onClose();
                } catch (e) {
                  setErr((e as { message?: string })?.message ?? "Remove failed");
                }
              }}
              className="btn-danger px-3 py-1.5 text-[10px] disabled:opacity-50"
            >
              Remove node
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function DeleteConfirm({ c, onClose }: { c: Cluster; onClose: () => void }) {
  const [typed, setTyped] = useState("");
  const [pending, setPending] = useState(false);

  return (
    <div className="mt-3 surface p-3 rounded-lg border border-danger/30">
      <p className="text-[10px] text-text-tertiary mb-2">
        Type <code className="font-mono">{c.name}</code> to confirm {c.tier === "attached" ? "disconnect" : "deletion"}.
      </p>
      <input
        value={typed}
        onChange={(e) => setTyped(e.target.value)}
        className="w-full rounded bg-white/5 px-2 py-1 text-xs border border-white/5 text-text-secondary"
        aria-label="Cluster name confirmation"
      />
      <div className="flex justify-end gap-2 mt-2">
        <button onClick={onClose} className="btn-ghost px-2 py-1 text-[10px]">
          Cancel
        </button>
        <button
          disabled={typed !== c.name || pending}
          onClick={async () => {
            setPending(true);
            try {
              await api.remove(c.id);
              onClose();
            } catch {
              setPending(false);
            }
          }}
          className="btn-danger px-3 py-1.5 text-[10px] disabled:opacity-50"
          data-testid="delete-confirm-button"
        >
          {pending ? "…" : c.tier === "attached" ? "Disconnect" : "Delete"}
        </button>
      </div>
    </div>
  );
}
