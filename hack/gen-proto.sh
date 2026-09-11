#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
buf generate
gofmt -w api