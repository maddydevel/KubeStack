// Copyright 2026 The KubeStack Authors. All rights reserved.
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "./api";
import Layout from "./components/layout";
import Dashboard from "./components/dashboard";
import RegisterDialog from "./components/register-dialog";

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

export default function App() {
  // Hooks must be called in the same order on every render.
  const me = useQuery({ queryKey: ["me"], queryFn: api.me });
  const qc = useQueryClient();
  const clusters = useQuery({ queryKey: ["clusters"], queryFn: api.clusters, enabled: !!me.data });
  const versions = useQuery({ queryKey: ["versions"], queryFn: api.versions, staleTime: Infinity, enabled: !!me.data });
  const audit = useQuery({ queryKey: ["audit"], queryFn: api.audit, enabled: !!me.data });

  const [registerOpen, setRegisterOpen] = useState(false);
  const [regName, setRegName] = useState("");
  const [regVersion, setRegVersion] = useState("");
  const regDone = useMutation({
    mutationFn: () => {
      const v = versions.data?.at(-1);
      if (!v) throw new Error("No versions available");
      return api.create(regName, v);
    },
    onSuccess: () => {
      setRegName("");
      setRegVersion("");
      setRegisterOpen(false);
      qc.invalidateQueries({ queryKey: ["clusters"] });
    },
  });

  if (me.error && (me.error as ApiError).status === 401) {
    return (
      <main className="surface flex h-screen flex-col items-center justify-center p-6">
        <div className="rounded bg-white/5 p-6 text-center">
          <h1 className="text-2xl font-signature">KubeStack</h1>
          <p className="text-sm text-text-secondary mt-1">
            Sign in with your organisation account to continue.
          </p>
          <a href="/auth/login" className="mt-4 inline-block btn-brand px-4 py-2 text-sm">
            Sign in
          </a>
        </div>
      </main>
    );
  }
  if (!me.data) return <main className="surface p-6">{<ErrorBox error={me.error} />}</main>;

  return (
    <div className="flex h-screen overflow-hidden">
      <Layout me={me.data}>
        <Dashboard
          me={me.data}
          clusters={clusters.data ?? []}
          versions={versions.data ?? []}
          audit={audit.data ?? []}
          onCreateCluster={() => setRegisterOpen(true)}
        />
        {registerOpen && (
          <RegisterDialog
            open={registerOpen}
            onClose={() => setRegisterOpen(false)}
            versions={versions.data ?? []}
            name={regName}
            setName={setRegName}
            version={regVersion}
            setVersion={setRegVersion}
            creating={regDone.isPending}
            onCreate={() => regDone.mutate()}
            error={regDone.error}
          />
        )}
      </Layout>
    </div>
  );
}
