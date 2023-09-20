#!/usr/bin/env bash
set -euo pipefail

if [ -z "$(git tag --points-at HEAD)" ]; then
	git describe --always --long --dirty | sed 's/^v//'
else
	git tag --points-at HEAD | sed 's/^v//'
fi
