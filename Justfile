# Archaeopteryx development tasks

_CURRENT_VERSION := `git describe --tags --always --dirty 2>/dev/null || echo devel`
_VERSION := if env_var_or_default('APX_BUILD_VERSION', '') != '' { env_var_or_default('APX_BUILD_VERSION', '') } else { _CURRENT_VERSION }
_COMMIT := `git rev-parse --short HEAD 2>/dev/null || echo unknown`
_BUILD_DATE := `date -u +%Y-%m-%dT%H:%M:%SZ`
_LDFLAGS := '-s -w -X github.com/HuntedRaven7/Archaeopteryx/pkg/version.Version=' + _VERSION + ' -X github.com/HuntedRaven7/Archaeopteryx/pkg/version.Commit=' + _COMMIT + ' -X github.com/HuntedRaven7/Archaeopteryx/pkg/version.BuildDate=' + _BUILD_DATE

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

# Lint with golangci-lint (skips with a notice when not installed).
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed; skipping (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)"; \
	fi

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
check: fmt vet lint test build

# Stage the Microraptor drop-in tree (binaries + systemd units) into dist/rootfs/.
image-tree: build
	@rm -rf dist/rootfs
	@mkdir -p dist/rootfs/usr/bin
	@mkdir -p dist/rootfs/usr/lib/systemd/system
	@mkdir -p dist/rootfs/usr/lib/systemd/system-preset
	cp dist/apxd dist/apxctl dist/rootfs/usr/bin/
	cp packaging/microraptor/apxd.service dist/rootfs/usr/lib/systemd/system/
	cp packaging/microraptor/zz-enable-apxd.preset dist/rootfs/usr/lib/systemd/system-preset/
	@echo "staged dist/rootfs (microraptor drop-in)"

# Local loopback end-to-end smoke: config gen -> maintenance onboarding ->
# apply-config -> read-only checks against a live daemon.
smoke:
	./hack/smoke.sh

# Full QEMU smoke of a Microraptor DDI (requires MICRORAPTOR_IMAGE).
qemu-smoke:
	MICRORAPTOR_IMAGE="$(or ${MICRORAPTOR_IMAGE},)" ./hack/qemu-smoke.sh