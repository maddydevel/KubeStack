// Copyright 2026 The KubeStack Authors. All rights reserved.

type StatusProps = { ready: boolean };

export function StatusIndicator({ ready }: StatusProps) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-xs font-signature ${
        ready
          ? "pill-success"
          : "pill-danger"
      }`}
    >
      <span className={`status-dot ${ready ? "ready" : "not-ready"}`} aria-hidden="true" />
      {ready ? "Ready" : "Not ready"}
    </span>
  );
}
