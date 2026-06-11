# Development And Releases

The bridge is a standalone Go module under `bridge/`.

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
go vet ./...
go test ./... -cover
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
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

## Home Assistant Cards

The optional cards live under `ha-cards/`.

```shell
cd ha-cards
npm ci
npm test
npm run build
```

## Continuous Integration

Pull requests and branch pushes run `.github/workflows/ci.yml`.

The bridge job checks formatting, `go vet`, race-enabled tests with coverage, `staticcheck`, and `govulncheck`.

The card job installs dependencies with `npm ci`, runs the Node test suite, and builds the Vite bundle.

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
3. GoReleaser builds Linux `amd64` and `arm64` binaries.
4. GoReleaser publishes release archives and a multi-arch Docker image to `rcooler/jinko_exporter`.

Stable tags publish Docker tags:

```text
1.2.3
1.2
1
latest
```

Pre-release tags publish only the exact version tag.
