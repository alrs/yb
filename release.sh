#!/bin/bash

set -eux

APP="app_gtQEt1zkGMj"
PROJECT="artificer"
VERSION="$(echo $YB_GIT_BRANCH | sed -e 's|refs/tags/||g')"
TOKEN="${BUILDAGENT_TOKEN}"
RELEASE_KEY="${BUILDAGENT_RELEASE_KEY}"

umask 077

cleanup() {
    rv=$?
    rm -rf "$tmpdir"
    exit $rv
}

tmpdir="$(mktemp)"
trap "cleanup" INT TERM EXIT

KEY_FILE="${tmpdir}"
echo "${RELEASE_KEY}" > "${KEY_FILE}"

equinox release \
        --version=$VERSION \
        --platforms="darwin_amd64 linux_amd64" \
        --signing-key="${KEY_FILE}"  \
        --app="$APP" \
        --token="${TOKEN}" \
	-- \
	-ldflags "-X main.version=$VERSION -X 'main.date=$(date)'" \
	"github.com/microclusters/${PROJECT}"
