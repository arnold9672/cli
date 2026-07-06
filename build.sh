#!/usr/bin/env bash
# Copyright (c) 2026 Lark Technologies Pte. Ltd.
# SPDX-License-Identifier: MIT
set -euo pipefail
cd "$(dirname "$0")"
python3 scripts/fetch_meta.py
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
go build -ldflags "-s -w -X code.byted.org/lark_search/larksuite-cli/internal/build.Version=${VERSION} -X code.byted.org/lark_search/larksuite-cli/internal/build.Date=$(date +%Y-%m-%d)" -o lark-cli .
echo "OK: ./lark-cli (${VERSION})"
