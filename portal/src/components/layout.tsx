// Copyright 2026 The KubeStack Authors. All rights reserved.
import { useState, useEffect } from "react";
import { api, type Me } from "../api";
import { useTheme } from "./theme-context";

type LayoutProps = { me: Me; children: React.ReactNode };

const NAV = [
  { key: "clusters", label: "Clusters", icon: "◈" },
  { key: "audit", label: "Audit trail", icon: "⧉" },
  { key: "settings", label: "Settings", icon: "☰" },
];

export default function Layout({ me, children }: LayoutProps) {
  const { theme, toggleTheme } = useTheme()!;
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

  useEffect(() => {
    if (window.innerWidth >= 768) {
      setMobileMenuOpen(false);
    }
  }, []);

  return (
    <div className="flex h-screen overflow-hidden">
      {/* Mobile overlay */}
      {mobileMenuOpen && (
        <div
          className="fixed inset-0 bg-black/40 z-40"
          onClick={() => setMobileMenuOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside
        className={`panel flex flex-col w-72 shrink-0 border-r border-white/5 sidebar ${
          mobileMenuOpen ? "" : "sidebar-collapsed"
        }`}
      >
        <div className="flex items-center justify-between px-5 py-4 border-b border-white/5">
          <div className="flex items-center gap-2">
            <div className="rounded bg-accent p-1.5 text-xs font-mono text-white">
              K
            </div>
            <span className={`text-sm font-signature tracking-tight ${window.innerWidth >= 768 ? "block" : "hidden"}`}>
              KubeStack
            </span>
          </div>
          <button
            onClick={() => setMobileMenuOpen((o) => !o)}
            className="rounded p-1.5 text-text-quaternary hover:text-white hover:bg-white/5 transition-colors"
            aria-label="Toggle menu"
          >
            ☰
          </button>
        </div>
        <nav className="flex-1 px-3 py-3 space-y-1">
          {NAV.map((n) => (
            <button
              key={n.key}
              className={`flex items-center gap-3 px-3 py-2 rounded-sm text-sm transition-colors text-text-tertiary hover:bg-white/5 hover:text-text-secondary`}
            >
              <span className={`w-5 text-text-quaternary ${window.innerWidth < 768 ? "hidden" : "block"}`}>
                {n.icon}
              </span>
              <span className={`font-signature ${window.innerWidth < 768 ? "hidden" : "block"}`}>
                {n.label}
              </span>
            </button>
          ))}
        </nav>
        <div className="px-5 py-4 border-t border-white/5">
          <div className="flex items-center gap-3">
            <div className="h-8 w-8 rounded-full bg-accent flex items-center justify-center text-xs font-bold">
              {me.name.charAt(0).toUpperCase()}
            </div>
            <div className={`flex flex-col ${window.innerWidth < 768 ? "hidden" : "block"}`}>
              <div className="text-sm font-signature">{me.name}</div>
              <div className="text-xs text-text-tertiary">
                {me.admin ? "Administrator" : "Read-only"}
              </div>
            </div>
          </div>
          <button
            onClick={toggleTheme}
            className="mt-3 w-full btn-ghost px-3 py-2 text-sm hover:bg-white/5 border-r-0 border-l-0 border-b-0 border-t border-white/5 flex items-center justify-between"
          >
            <span>{theme === "dark" ? "Light mode" : "Dark mode"}</span>
            <span>{theme === "dark" ? "☀️" : "🌙"}</span>
          </button>
          <button
            onClick={async () => {
              await api.logout();
              window.location.reload();
            }}
            className="mt-2 w-full btn-ghost px-3 py-2 text-sm hover:bg-white/5 border-r-0 border-l-0 border-b-0 border-t border-white/5"
          >
            Sign out
          </button>
        </div>
      </aside>

      {/* Main content area */}
      <div className="flex-1 flex flex-col overflow-hidden bg-bg">
        <header className="flex items-center justify-between px-6 py-4 border-b border-white/5 bg-bg/80 backdrop-blur z-10">
          <div className="flex items-center gap-3">
            <h2 className="text-lg font-signature">Clusters</h2>
            <span className="h-4 w-px bg-white/10 mx-1"></span>
            <span className="text-xs text-text-quaternary font-mono">
              Control plane
            </span>
          </div>
          <div className="flex items-center gap-3">
            <input
              type="search"
              placeholder="Search clusters…"
              className="rounded bg-white/5 px-3 py-1.5 text-xs border border-white/5 focus:border-accent focus:outline-none transition-colors w-48"
            />
            <button
              className="rounded p-1.5 text-text-quaternary hover:text-white hover:bg-white/5 transition-colors"
              aria-label="Notifications"
            >
              🔔
            </button>
            <button
              onClick={toggleTheme}
              className="rounded p-1.5 text-text-quaternary hover:text-white hover:bg-white/5 transition-colors"
              aria-label="Toggle theme"
            >
              {theme === "dark" ? "☀️" : "🌙"}
            </button>
            <button
              className="rounded p-1.5 text-text-quaternary hover:text-white hover:bg-white/5 transition-colors"
              aria-label="User menu"
            >
              ☰
            </button>
          </div>
        </header>
        <main className="flex-1 overflow-y-auto p-6">{children}</main>
      </div>
    </div>
  );
}
