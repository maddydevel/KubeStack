// Copyright 2026 The KubeStack Authors. All rights reserved.
import { type Cluster, type Me } from "../api";
import ClusterCard from "./cluster-card";
import EmptyState from "./empty-state";

type DashboardProps = {
  me: Me;
  clusters: Cluster[];
  versions: string[];
  audit: Array<{ id: number; time: string; actor: string; action: string; clusterId: string; result: string; error?: string }>;
  active: string;
  onCreateCluster: () => void;
};

export default function Dashboard({ me, clusters, versions, audit, active, onCreateCluster }: DashboardProps) {
  const readyCount = clusters.filter((c) => c.ready).length;
  const notReadyCount = clusters.length - readyCount;
  const snapshotCount = clusters.reduce((sum, c) => sum + (c.operations.snapshot?.allowed ? 1 : 0), 0);

  if (active === "audit") {
    return (
      <div className="space-y-8">
        <h1 className="text-xl font-signature text-text-primary">Audit trail</h1>
        <p className="text-xs text-text-tertiary mt-0.5">
          Lifecycle operations and their outcomes across all clusters.
        </p>
        <div className="surface">
          <table className="w-full text-left text-sm border-collapse">
            <thead>
              <tr className="bg-white/5">
                <th className="px-3 py-2 text-left text-[10px] font-signature text-text-tertiary">Time</th>
                <th className="px-3 py-2 text-left text-[10px] font-signature text-text-tertiary">Actor</th>
                <th className="px-3 py-2 text-left text-[10px] font-signature text-text-tertiary">Action</th>
                <th className="px-3 py-2 text-left text-[10px] font-signature text-text-tertiary">Cluster</th>
                <th className="px-3 py-2 text-left text-[10px] font-signature text-text-tertiary">Result</th>
              </tr>
            </thead>
            <tbody>
              {audit.slice(0, 50).map((r) => (
                <tr key={r.id} className="border-t border-white/5 animate-fade-in" style={{ animationDelay: "0.2s" }}>
                  <td className="px-3 py-2 text-text-quaternary font-mono text-[10px]">
                    {new Date(r.time).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
                  </td>
                  <td className="px-3 py-2 text-text-secondary">{r.actor}</td>
                  <td className="px-3 py-2 text-text-secondary">{r.action}</td>
                  <td className="px-3 py-2 text-text-secondary font-mono text-[10px]">{r.clusterId}</td>
                  <td className={`px-3 py-2 text-[10px] font-signature ${r.result === "failure" ? "text-danger" : "text-text-quaternary"}`}>
                    {r.result}{r.error && `: ${r.error}`}
                  </td>
                </tr>
              ))}
              {audit.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-3 py-6 text-center text-text-quaternary">
                    No activity yet.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    );
  }

  if (active === "settings") {
    return (
      <div className="space-y-8">
        <h1 className="text-xl font-signature text-text-primary">Settings</h1>
        <p className="text-xs text-text-tertiary mt-0.5">
          Platform configuration and preferences.
        </p>
        <div className="surface p-6">
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-sm font-signature text-text-primary">Theme</h3>
                <p className="text-xs text-text-tertiary">Switch between dark and light mode.</p>
              </div>
              <button className="text-xs font-signature text-accent hover:text-accent-hover">Toggle theme</button>
            </div>
            <div className="flex items-center justify-between pt-4 border-t border-white/10">
              <div>
                <h3 className="text-sm font-signature text-text-primary">Admin group</h3>
                <p className="text-xs text-text-tertiary">{me.admin ? "You are an administrator" : "Standard user access"}</p>
              </div>
              <span className="text-xs font-mono text-text-tertiary">{me.admin ? "admin" : "user"}</span>
            </div>
          </div>
        </div>
      </div>
    );
  }

  // Default: Clusters view
  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between animate-fade-in" style={{ animationDelay: "0.1s" }}>
        <div>
          <h1 className="text-xl font-signature text-text-primary">Clusters</h1>
          <p className="text-xs text-text-tertiary mt-0.5">
            Manage RKE2 and attached clusters through the headless Rancher engine.
          </p>
        </div>
        <button
          disabled={!me.admin}
          onClick={onCreateCluster}
          className="btn-brand px-3 py-1.5 text-xs disabled:opacity-50 disabled:cursor-not-allowed"
        >
          Create cluster
        </button>
      </div>

      {/* Stat cards — glanceable, real data */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-3">
        <StatCard
          label="Total clusters"
          value={clusters.length.toString()}
          subtitle="Across all tiers"
          className="animate-slide-in"
          style={{ animationDelay: "0.1s" }}
        />
        <StatCard
          label="Ready"
          value={readyCount.toString()}
          subtitle="Control plane healthy"
          accent="success"
          className="animate-slide-in"
          style={{ animationDelay: "0.2s" }}
        />
        <StatCard
          label="Not ready"
          value={notReadyCount.toString()}
          subtitle="Needs attention"
          accent="warning"
          className="animate-slide-in"
          style={{ animationDelay: "0.3s" }}
        />
        <StatCard
          label="Snapshots (24h)"
          value={snapshotCount.toString()}
          subtitle="Clusters with snapshots"
          accent="accent"
          className="animate-slide-in"
          style={{ animationDelay: "0.4s" }}
        />
      </div>

      {/* Cluster grid */}
      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-signature text-text-secondary uppercase tracking-wider">
            Cluster inventory
          </h2>
          <span className="text-xs text-text-quaternary font-mono">
            {clusters.length} cluster{clusters.length !== 1 ? "s" : ""}
          </span>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {clusters.length === 0 ? (
            <div className="col-span-full animate-fade-in">
              <EmptyState
                title="No clusters yet"
                subtitle="Create your first RKE2 cluster or import an existing one to get started."
                action={
                  <button
                    disabled={!me.admin}
                    className="btn-brand px-4 py-2 text-sm disabled:opacity-50"
                  >
                    Create cluster
                  </button>
                }
              />
            </div>
          ) : (
            clusters.map((c) => (
              <ClusterCard key={c.id} c={c} versions={versions} className="card-lift" />
            ))
          )}
        </div>
      </section>

      {/* Recent audit activity */}
      <section className="flex flex-col gap-3">
        <h2 className="text-sm font-signature text-text-secondary uppercase tracking-wider">
          Recent activity
        </h2>
        <div className="surface">
          <table className="w-full text-left text-sm border-collapse">
            <thead>
              <tr className="bg-white/5">
                <th className="px-3 py-2 text-left text-[10px] font-signature text-text-tertiary">Time</th>
                <th className="px-3 py-2 text-left text-[10px] font-signature text-text-tertiary">Actor</th>
                <th className="px-3 py-2 text-left text-[10px] font-signature text-text-tertiary">Action</th>
                <th className="px-3 py-2 text-left text-[10px] font-signature text-text-tertiary">Cluster</th>
                <th className="px-3 py-2 text-left text-[10px] font-signature text-text-tertiary">Result</th>
              </tr>
            </thead>
            <tbody>
              {audit.slice(0, 10).map((r) => (
                <tr key={r.id} className="border-t border-white/5 animate-fade-in" style={{ animationDelay: "0.2s" }}>
                  <td className="px-3 py-2 text-text-quaternary font-mono text-[10px]">
                    {new Date(r.time).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
                  </td>
                  <td className="px-3 py-2 text-text-secondary">{r.actor}</td>
                  <td className="px-3 py-2 text-text-secondary">{r.action}</td>
                  <td className="px-3 py-2 text-text-secondary font-mono text-[10px]">{r.clusterId}</td>
                  <td className={`px-3 py-2 text-[10px] font-signature ${r.result === "failure" ? "text-danger" : "text-text-quaternary"}`}>
                    {r.result}{r.error && `: ${r.error}`}
                  </td>
                </tr>
              ))}
              {audit.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-3 py-6 text-center text-text-quaternary">
                    No activity yet.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

function StatCard({
  label,
  value,
  subtitle,
  accent,
  className,
  style,
}: {
  label: string;
  value: string;
  subtitle: string;
  accent?: "success" | "warning" | "accent";
  className?: string;
  style?: React.CSSProperties;
}) {
  const accentClass =
    accent === "success"
      ? "bg-success-subtle text-success border border-success/20"
      : accent === "warning"
        ? "bg-warning-subtle text-warning border border-warning/20"
        : accent === "accent"
          ? "bg-accent/10 text-accent border border-accent/20"
          : "bg-white/5 border border-white/5";

  return (
    <div
      className={`surface p-3 rounded-lg border stat-card ${accentClass} ${className ?? ""}`}
      style={style}
    >
      <p className="text-[10px] font-signature text-text-tertiary mb-1">{label}</p>
      <div className="flex items-baseline gap-1">
        <span className="text-lg font-signature text-text-primary">{value}</span>
      </div>
      <p className="text-[10px] text-text-tertiary mt-0.5">{subtitle}</p>
    </div>
  );
}
