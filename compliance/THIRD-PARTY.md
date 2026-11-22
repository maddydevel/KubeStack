# Third-party software

All shipped dependencies are permissively licensed; verified by `make license-check`.

## Go (kubestack-api, kubestack-controller)
| Module | License |
|--------|---------|
| k8s.io/client-go, k8s.io/apimachinery, k8s.io/klog, k8s.io/utils, sigs.k8s.io/* | Apache-2.0 |
| sigs.k8s.io/controller-runtime, k8s.io/api, k8s.io/apiextensions-apiserver, k8s.io/kube-openapi | Apache-2.0 |
| github.com/prometheus/client_golang, client_model, common, procfs; gomodules.xyz/jsonpatch/v2; github.com/google/btree, uuid | Apache-2.0 / BSD-3-Clause |
| github.com/evanphx/json-patch/v5, fsnotify, emicklei/go-restful/v3, beorn7/perks, cespare/xxhash/v2 | BSD-3-Clause / MIT |
| github.com/coreos/go-oidc/v3 | Apache-2.0 |
| github.com/go-jose/go-jose/v4 | Apache-2.0 |
| github.com/jackc/pgx/v5, pgpassfile, pgservicefile, puddle | MIT |
| golang.org/x/* | BSD-3-Clause |
| others (gogo/protobuf, json-iterator, cbor, pflag, go-spew, go-cmp, gofuzz, logr, float16, reflect2, concurrent, goautoneg, inf.v0) | BSD/MIT/Apache-2.0 |

## Portal (bundled)
| Package | License |
|---------|---------|
| react, react-dom, scheduler, loose-envify, js-tokens | MIT |
| @tanstack/react-query, @tanstack/query-core | MIT |

Build/test-only, not shipped: vite, tailwindcss, typescript (Apache-2.0), esbuild, vitest, jsdom, @testing-library/*,
eslint, @eslint/js, typescript-eslint, eslint-plugin-react-hooks (MIT).

## Dev and release tools (run in Docker or CI, never linked or shipped)
| Tool | License |
|------|---------|
| golangci-lint | GPL-3.0 (tool only; R11 forbids GPL in shipped binaries/images, which this is not) |
| syft, cosign, helm, controller-gen (sigs.k8s.io/controller-tools) | Apache-2.0 |
