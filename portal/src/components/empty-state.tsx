// Copyright 2026 The KubeStack Authors. All rights reserved.

type EmptyStateProps = { title: string; subtitle: string; action?: React.ReactNode };

export default function EmptyState({ title, subtitle, action }: EmptyStateProps) {
  return (
    <div className="surface flex flex-col items-center justify-center py-20 text-center">
      <div className="h-12 w-12 rounded bg-accent/10 flex items-center justify-center text-accent mb-4 text-sm">K</div>
      <h3 className="text-base font-signature mb-1">{title}</h3>
      <p className="text-text-tertiary text-sm max-w-xs mx-auto mb-6">{subtitle}</p>
      {action && <div className="flex justify-center">{action}</div>}
    </div>
  );
}
