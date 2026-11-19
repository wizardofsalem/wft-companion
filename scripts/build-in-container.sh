#!/usr/bin/env bash
set -euo pipefail

: "${VERSION:?}" "${SOURCE_DATE_EPOCH:?}"
export GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=mod CGO_ENABLED=0
export GOCACHE=/tmp/gocache GOPATH=/tmp/gopath HOME=/tmp

cp -a /src /tmp/src
if [[ -d /wowrecord ]]; then cp -a /wowrecord /tmp/wowrecord; fi
cd /tmp/src
go version >/out/.go-version

if [[ ${FROM_CONFIG:-} == 1 ]]; then
	rel() { go run ./tools/release "$@"; }
	pub=tools/release/keys/update-1.pub next=tools/release/keys/update-2.pub
	PUBLISHER=$(rel publisher -signer -config release.json)
	INSTALL_PUBLISHER=$(rel publisher -display -config release.json)
	BASE_URL=$(rel publisher -base -config release.json)
	ROOT_NAME=
	[[ ${SIGNED_RELEASE:-false} == true ]] && ROOT_NAME=$(rel publisher -root -config release.json)
	KEY_CURRENT=$(rel pubkey -pub "$pub")
	KEY_NEXT=$(rel pubkey -pub "$next")
fi

go run ./tools/release winres -version "${VERSION}" -config release.json -out app/rsrc_windows_amd64.syso

app=$(go list -m)/app
upd=${app}/updater
ldflags="-s -w -X main.version=${VERSION} -H=windowsgui"
ldflags+=" -X ${upd}.signedRelease=${SIGNED_RELEASE:-false}"
[[ -n ${PUBLISHER:-} ]] && ldflags+=" -X '${upd}.publisher=${PUBLISHER}'"
[[ -n ${INSTALL_PUBLISHER:-} ]] && ldflags+=" -X '${app}/install.Publisher=${INSTALL_PUBLISHER}'"
[[ -n ${ROOT_NAME:-} ]] && ldflags+=" -X '${upd}.rootName=${ROOT_NAME}'"
[[ -n ${BASE_URL:-} ]] && ldflags+=" -X ${upd}.baseURL=${BASE_URL}"
[[ -n ${KEY_CURRENT:-} ]] && ldflags+=" -X ${upd}.keyCurrent=${KEY_CURRENT}"
[[ -n ${KEY_NEXT:-} ]] && ldflags+=" -X ${upd}.keyNext=${KEY_NEXT}"

GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "${ldflags}" \
	-o "/out/WFTCompanion-${VERSION}-windows-amd64.exe" ./app

go run ./tools/release zip -src addon/WoWForeverGuildChronicle -epoch "${SOURCE_DATE_EPOCH}" \
	-out "/out/WoWForeverGuildChronicle-${VERSION}.zip"

if [[ ${FROM_CONFIG:-} == 1 ]]; then go run ./tools/release pecheck -dir /out; fi
