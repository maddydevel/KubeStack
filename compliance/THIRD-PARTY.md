# Third-party software

All shipped dependencies are permissively licensed; verified by `make license-check`.

## Go (kubestack-api)
| Module | License |
|--------|---------|
| k8s.io/client-go, k8s.io/apimachinery, k8s.io/klog, k8s.io/utils, sigs.k8s.io/* | Apache-2.0 |
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

Build/test-only, not shipped: vite, tailwindcss, typescript (Apache-2.0), esbuild, vitest, jsdom, @testing-library/* (MIT).
