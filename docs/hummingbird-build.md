# Hummingbird parallel build (ROSAENG-75337)

The production `Dockerfile` is unchanged. `Dockerfile.hummingbird` retains the
repository's pinned boilerplate Go toolchain and uses a distinct, digest-pinned
`hi/core-runtime:2.43-openssl-fips` runtime. The runtime digest was resolved with
`skopeo inspect --override-os linux --override-arch amd64 --format '{{.Digest}}'`.

The application is the only runtime binary. An audit of `cmd/` and `internal/`
found no subprocess invocation or runtime package installation. No Hummingbird
RPM or downloaded CLI is needed, so no Hummingbird builder stage is necessary.
The existing compilation stage prepares only application-owned empty home/data
directories; no UBI RPM files or libraries are copied into the runtime.

The shell entrypoint only adds `start`; its `APP_ENV=dev` branch is a no-op.
The hardened variant executes the Go binary with `start` directly, preserving
argument forwarding. UID 65532 with group 0 and group-writable home/data support
OpenShift arbitrary UIDs. The original entrypoint remains untouched.

The copied push PipelineRun changes only its name, application/component labels,
output tag and Dockerfile parameter. Its distinct labels and `-hummingbird` tag
keep this experimental build separate from production snapshots. Konflux
application/component registration, cluster tests and scanning require the
corresponding external environment; this change does not deploy the image.

## Local verification

Both Dockerfiles built successfully using Linux amd64 emulation on the arm64
workstation VM. The images were:

- hardened: `608367b5fa6207abb831a7a025697c5d703e9f2823cfbdb58da0b5693e0ed6a8`
- production: `b0378d96b6bb0d644fc75266ef64f70531faff62351eb375a6086837c460568b`

```sh
podman build --platform linux/amd64 --cpu-quota=300000 \
  --volume /var/tmp/hummingbird-go-cache-v841:/tmp/go-build:z --pull=never \
  -f Dockerfile.hummingbird -t rhobs-synthetics-agent-hummingbird-test .
podman build --platform linux/amd64 --cpu-quota=300000 \
  --volume /var/tmp/hummingbird-go-cache-v841:/tmp/go-build:z --pull=never \
  -f Dockerfile -t rhobs-synthetics-agent-production-test .
```

The shared Go cache and CPU quota are local verification aids; they do not modify
the Dockerfiles or CI requirements. Initial concurrent attempts disconnected with
`unexpected EOF`; the successful completed builds above supersede those attempts.

The default entrypoint with `--help` succeeds in both images. The hardened image
also passes with `--user 1001230000:0`, and its binary can be invoked directly with
`--entrypoint /home/rhobs/rhobs-synthetics-agent --help` (image name precedes
`--help`). The executable is dynamically linked with interpreter
`/lib64/ld-linux-x86-64.so.2`; successful execution verifies its startup libraries
are supplied by the Hummingbird base without copying UBI libraries.

A detached hardened container running as `1001230000:0` with
`--leader-elect=false` and no API URLs started successfully in standalone mode.
The HTTP `/livez` and `/readyz` endpoints returned `OK` and `Ready`. The smoke
container was stopped and removed afterward.

JSON and YAML parse, only the specified PipelineRun fields differ, and
`git diff --check` passes. The production Dockerfile is byte-for-byte unchanged.
No live Kubernetes reconciliation, external CI, CVE scan, Enterprise Contract or
release gate was exercised locally. The base is FIPS-capable; the application
retains its existing `make build` behavior and this check does not establish
application-level FIPS certification.
