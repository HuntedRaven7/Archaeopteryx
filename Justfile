# Archaeopteryx development tasks

_CURRENT_VERSION := `git describe --tags --always --dirty 2>/dev/null || echo devel`
_COMMIT := `git rev-parse --short HEAD 2>/dev/null || echo unknown`
_BUILD_DATE := `date -u +%Y-%m-%dT%H:%M:%SZ`
_LDFLAGS := '-s -w -X github.com/HuntedRaven7/Archaeopteryx/pkg/version.Version=' + _CURRENT_VERSION + ' -X github.com/HuntedRaven7/Archaeopteryx/pkg/version.Commit=' + _COMMIT + ' -X github.com/HuntedRaven7/Archaeopteryx/pkg/version.BuildDate=' + _BUILD_DATE

default: check

# Format all Go sources.
fmt:
	gofmt -w $(find . -name '*.go' -type f)

# Run go vet.
vet:
	go vet ./...

# Run all unit tests.
test:
	go test ./...

# Regenerate protobuf/gRPC code.
proto:
	./hack/gen-proto.sh

# Build the CLI and the daemon into dist/.
build:
	@mkdir -p dist
	CGO_ENABLED=0 go build -ldflags "{{_LDFLAGS}}" -o dist/apxd ./cmd/apxd
	CGO_ENABLED=0 go build -ldflags "{{_LDFLAGS}}" -o dist/apxctl ./cmd/apxctl

# Cross-compile static binaries for amd64 and arm64.
cross:
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "{{_LDFLAGS}}" -o dist/apxctl-linux-amd64 ./cmd/apxctl
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "{{_LDFLAGS}}" -o dist/apxd-linux-amd64 ./cmd/apxd
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "{{_LDFLAGS}}" -o dist/apxctl-linux-arm64 ./cmd/apxctl
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "{{_LDFLAGS}}" -o dist/apxd-linux-arm64 ./cmd/apxd

# Everything that must pass before a commit.
check: fmt vet test build