# Development And Releases

The bridge is a standalone Go module under `bridge/`.

## Toolchain

- Go 1.27.0, as declared by `bridge/go.mod`.
- Node.js 24.20.0, as pinned by the repository `.node-version`; both card packages support Node.js `^24.12.0` and declare npm 11.19.0 as their package manager.
- A Docker release with BuildKit support for cache mounts, linked copies, and file-permission flags.

Use the repository-pinned toolchains when reproducing CI or release builds. This keeps local type stripping, package-lock resolution, and Go module selection consistent with automation.

## Local Commands

Run tests:

```shell
cd bridge
go test ./...
```

Run the same core Go checks as CI:

```shell
cd bridge
gofmt -w .
go mod tidy -diff
go vet ./...
go test ./... -cover
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
```

The CI workflow also runs `go test ./... -race -cover` on Ubuntu. On Windows this requires cgo and a C toolchain.

Build locally:

```shell
cd bridge
go build -o jinko-exporter .
```

Run the server:

```shell
cd bridge
go run . serve
```

Fetch one snapshot:

```shell
cd bridge
go run . fetch
```

## Fixtures

Fixtures live under:

```text
bridge/testdata/
```

Current fixtures:

- `jinko_detail_response.json`
- `params.json`

## Docker

Build from the repository root:

```shell
docker build -f bridge/Dockerfile -t rcooler/jinko-exporter:local bridge
```

The Docker build uses a Go 1.27.0/Alpine 3.24 builder and an Alpine 3.24.1 runtime. BuildKit caches Go modules and compilation artifacts between builds. The final image contains the statically linked bridge and CA certificates, runs as the fixed non-root user `65532:65532`, and includes the bridge healthcheck.

## Home Assistant Cards

The card implementations live under `ha-cards/` and `new-ha-cards/`. Each package has its own lockfile and the same validation command. Their bundles register the same Home Assistant custom elements, so consumers must install one implementation at a time.

```shell
cd ha-cards
npm ci
npm run check
```

```shell
cd new-ha-cards
npm ci
npm run check
```

`npm run check` runs the Node test suite, both browser and tooling TypeScript checks, and the Vite production build. Use `npm run dev` in either package for the development server.

## Continuous Integration

Pull requests and branch pushes run `.github/workflows/ci.yml`.

The bridge job checks formatting, `go vet`, race-enabled tests with coverage, `staticcheck`, and `govulncheck`.

The cards matrix installs both packages with `npm ci`, runs their Node test suites, typechecks application and tooling sources, and builds both Vite bundles. The package-level `check` command provides the same validation sequence locally.

## Dependency Updates

Dependabot checks Go modules, both npm card packages, bridge Docker images, and GitHub Actions every week. Patch and minor releases are grouped per ecosystem to keep routine updates reviewable; major releases remain separate so their migration impact is explicit.

## GoReleaser

The GoReleaser config stays at the repository root because release archives include root-level files and docs.

Validate config:

```shell
goreleaser check
```

Build a local snapshot:

```shell
goreleaser build --snapshot --clean
```

Full local release dry run:

```shell
goreleaser release --snapshot --clean
```

## GitHub Release Workflow

Tagged releases are handled by `.github/workflows/release.yml`.

Repository secrets required for Docker Hub publishing:

- `DOCKER_USERNAME`
- `DOCKER_PASSWORD`

Publishing flow:

1. Push a semantic version tag such as `v1.2.3`.
2. GitHub Actions checks out the repository and sets up Go from `bridge/go.mod`.
3. Both Home Assistant card implementations are built and included under their distinct package paths in every release archive.
4. GoReleaser builds Linux `amd64` and `arm64` binaries, SBOM-backed container images, and digest metadata.
5. GoReleaser publishes the archives and multi-arch Docker image to `rcooler/jinko_exporter`; GitHub then attests the archive checksums and image digests.

Stable tags publish Docker tags:

```text
1.2.3
1.2
1
latest
```

Pre-release tags publish only the exact version tag.
