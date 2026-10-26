# helm-cost

A Helm plugin that estimates the CPU and memory footprint of a chart, along with a rough monthly cost, **before you install it**.

It renders the chart with `helm template`, then sums resource requests and limits across the workloads it finds, multiplied by their replica counts.

```
$ helm cost my-nginx bitnami/nginx --set replicaCount=3
KIND        NAME      REPLICAS  CPU REQ  CPU LIM  MEM REQ  MEM LIM
Deployment  my-nginx  3         300m     450m     384Mi    576Mi
TOTAL                           300m     450m     384Mi    576Mi

Estimated monthly cost (based on requests): 7.66
  0.30 vCPU x 0.0300/hr + 0.38 GiB x 0.0040/hr, x 730 hours
  Default prices are placeholders. Pass --price-cpu and --price-mem for your provider.
```

## Features

- Works with any chart source accepted by `helm template`: local directories, repository charts, and OCI references.

- Respects `--set`, `-f`/`--values`, `--version`, and every other `helm template` flag.

- Handles `Deployment`, `StatefulSet`, `ReplicaSet`, `DaemonSet`, and bare `Pod` resources.

- Warns about containers with no resource requests, since those make the estimate too low.

- Requires no cluster access.

## Installation

```bash
helm plugin install https://github.com/fadedreams/helm-cost
```

On Helm 4 you may need `--verify=false`. Prebuilt binaries are provided for Linux and macOS (amd64 and arm64). Windows users can run it under WSL.

To build from source instead:

```bash
git clone https://github.com/fadedreams/helm-cost.git
cd helm-cost
go build -o bin/helm-cost .
HELM_COST_SKIP_DOWNLOAD=1 helm plugin install .
```

To uninstall:

```bash
helm plugin uninstall cost
```

## Usage

```
helm cost [--price-cpu N] [--price-mem N] RELEASE CHART [helm template flags...]
```

Cost flags must come **before** `RELEASE`. Everything after the release name is passed straight to `helm template`.

| Flag | Default | Description |
| --- | --- | --- |
| `--price-cpu` | `0.03` | Price per vCPU-hour |
| `--price-mem` | `0.004` | Price per GiB-hour |

The default prices are **placeholders**, not real provider rates. Set them to match your cloud or cluster.

### Examples

```bash
# A chart from a repo, with overrides
helm cost my-app bitnami/nginx --set replicaCount=3

# A local chart with a values file
helm cost my-app ./charts/my-app -f values-prod.yaml

# Custom pricing
helm cost --price-cpu 0.04 --price-mem 0.005 my-app bitnami/nginx

# An OCI chart at a specific version
helm cost my-app oci://registry.example.com/charts/my-app --version 1.2.3
```

## How it works

1. Runs `helm template` with the arguments you give it, using the Helm binary exposed to plugins (`$HELM_BIN`).

1. Parses the rendered manifests and finds workload resources.

1. For each workload, sums container requests and limits and multiplies by `spec.replicas` (default: `1`).

1. Prices **requests**, not limits, because requests are what the scheduler reserves:

   ```
   monthly = (vCPU × price-cpu + GiB × price-mem) × 730 hours
   ```

## Limitations

- Autoscaling is ignored. HPA minimum and maximum replicas are not considered; only `spec.replicas` is used.

- DaemonSets are counted as one replica, since the real count depends on your node count.

- `Job` and `CronJob` resources are skipped because they are not long-running.

- Init containers are not accounted for.

- Pricing uses two flat hourly rates. There is no cloud provider price lookup.

- Storage, load balancers, network egress, and node overhead are not included.

- Charts that do not set resource requests will be underestimated. The plugin prints a warning when it sees this.

Treat the number as a rough sizing guide, not a bill.

## Development

```bash
go mod tidy
go build -o bin/helm-cost .
helm cost my-release ./path/to/chart
```

Because the plugin is installed from a local directory, rebuilding `bin/helm-cost` is enough to pick up changes.

## Contributing

Issues and pull requests are welcome. Ideas on the roadmap:

- Release binaries and an install hook for `helm plugin install <url>`

- `--output json` for CI use

- Built-in price presets for common cloud providers

- HPA-aware estimates

- A `--compare` mode for estimating the cost difference between two values files

## License

Apache-2.0. See [LICENSE](LICENSE).
