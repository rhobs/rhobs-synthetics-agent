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

Local verification uses `--platform linux/amd64` because the workstation VM is
arm64 and the service build targets Linux amd64. JSON and YAML parse, the copied
PipelineRun differs only in the requested fields, and `git diff --check` passes.
Container build/run verification is pending after the shared Podman build service
lost its connections during concurrent compilation (`unexpected EOF`).
