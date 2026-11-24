// Copyright 2026 The KubeStack Authors. All rights reserved.

export type AddMode = "create" | "import";

type RegisterDialogProps = {
  open: boolean;
  mode: AddMode;
  setMode: (m: AddMode) => void;
  onClose: () => void;
  versions: string[];
  name: string;
  setName: (v: string) => void;
  version: string;
  setVersion: (v: string) => void;
  creating: boolean;
  onCreate: () => void;
  error: unknown;
};

export default function RegisterDialog({
  open,
  mode,
  setMode,
  onClose,
  versions,
  name,
  setName,
  version,
  setVersion,
  creating,
  onCreate,
  error,
}: RegisterDialogProps) {
  if (!open) return null;

  const errMsg = error instanceof Error ? error.message : String(error || "");

  return (
    <div className="fixed inset-0 flex items-center justify-center bg-black/40 p-4 z-50" onClick={onClose}>
      <div className="surface w-full max-w-md p-5" onClick={(e) => e.stopPropagation()}>
        <div className="flex justify-between mb-3">
          <h2 className="text-lg font-signature">Add cluster</h2>
          <button onClick={onClose} className="rounded p-1 text-text-quaternary hover:text-white">✕</button>
        </div>
        <div role="radiogroup" aria-label="How to add the cluster" className="flex gap-3 mb-2 text-xs">
          {(["create", "import"] as const).map((m) => (
            <label key={m} className="flex items-center gap-1">
              <input type="radio" name="add-mode" checked={mode === m} onChange={() => setMode(m)} />
              {m === "create" ? "Create RKE2 cluster" : "Import existing"}
            </label>
          ))}
        </div>
        <p className="text-[10px] text-text-tertiary mb-3">
          {mode === "create"
            ? "Provision a new RKE2 cluster at the chosen Kubernetes version."
            : "Attach an existing CNCF-conformant cluster. KubeStack never sees its join token."}
        </p>
        <div className="space-y-3">
          <label className="text-[10px] font-signature block">
            Cluster name
            <input
              required
              pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?"
              maxLength={63}
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="mt-1 w-full rounded bg-white/5 px-2 py-1.5 text-xs border border-white/5 text-text-secondary"
            />
          </label>
          {mode === "create" && (
            <label className="text-[10px] font-signature block">
              Kubernetes version
              <select
                value={version || versions.at(-1)}
                onChange={(e) => setVersion(e.target.value)}
                className="mt-1 w-full rounded bg-white/5 px-2 py-1.5 text-xs border border-white/5 text-text-secondary"
              >
                {versions.map((v) => (
                  <option key={v} value={v}>
                    {v}
                  </option>
                ))}
              </select>
            </label>
          )}
          {errMsg && <p className="text-[10px] text-danger">{errMsg}</p>}
        </div>
        <div className="flex justify-end gap-2 mt-4">
          <button onClick={onClose} className="btn-ghost px-3 py-1.5 text-xs">
            Cancel
          </button>
          <button disabled={creating || !name} onClick={onCreate} className="btn-brand px-3 py-1.5 text-xs disabled:opacity-50">
            {creating ? "…" : mode === "create" ? "Create" : "Import"}
          </button>
        </div>
      </div>
    </div>
  );
}
