#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
IMAGE=docker.io/library/golang:1.25.7@sha256:fb4095b65a7bb89f039def7e33d7b90095d2c25f34597748758a6f209eead7ff
if [[ -z ${CONTAINER:-} ]]; then
	if command -v podman >/dev/null; then CONTAINER=podman; else CONTAINER=docker; fi
fi
SIGNED=${SIGNED:-false}
[[ $SIGNED == true || $SIGNED == false ]] || { echo "build-repro: SIGNED must be true or false" >&2; exit 1; }

[[ -z $(git status --porcelain -- .) ]] || echo "build-repro: note: uncommitted changes are not part of this build (it uses HEAD)" >&2
commit=$(git rev-parse HEAD)
epoch=${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct HEAD)}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

prefix=$(git rev-parse --show-prefix)
top=$(git rev-parse --show-toplevel)
mkdir "$work/src"
if [[ -n $prefix ]]; then
	git -C "$top" archive --format=tar "HEAD:${prefix%/}" | tar -xf - -C "$work/src"
else
	git -C "$top" archive --format=tar HEAD | tar -xf - -C "$work/src"
fi
if [[ -n $prefix ]] && git -C "$top" cat-file -e HEAD:wowrecord 2>/dev/null; then
	mkdir "$work/wowrecord"
	git -C "$top" archive --format=tar HEAD:wowrecord | tar -xf - -C "$work/wowrecord"
fi
version=$(tr -d '[:space:]' <"$work/src/VERSION")
echo "== WFT Companion ${version} (commit ${commit:0:12}, signed build: ${SIGNED})"

for n in 1 2; do
	echo "== build $n"
	mkdir -p "$work/b$n/out"
	cp -a "$work/src" "$work/b$n/src"
	mounts=(-v "$work/b$n/src:/src:ro,z" -v "$work/b$n/out:/out:z")
	if [[ -d $work/wowrecord ]]; then
		cp -a "$work/wowrecord" "$work/b$n/wowrecord"
		mounts+=(-v "$work/b$n/wowrecord:/wowrecord:ro,z")
	fi
	"$CONTAINER" run --rm --network=none \
		-e VERSION="$version" -e SOURCE_DATE_EPOCH="$epoch" -e SIGNED_RELEASE="$SIGNED" -e FROM_CONFIG=1 \
		"${mounts[@]}" "$IMAGE" bash /src/scripts/build-in-container.sh
done
if ! diff -r "$work/b1/out" "$work/b2/out" >/dev/null; then
	echo "build-repro: the two builds differ: not reproducible" >&2
	(cd "$work" && sha256sum b1/out/* b2/out/*) >&2
	exit 1
fi
echo "== reproducible: both builds identical"

out=dist/repro
rm -rf "$out" && mkdir -p "$out"
cp "$work/b1/out/"* "$out/"
cat >"$out/BUILD-INFO.txt" <<EOF
version: $version
commit: $commit
source-date-epoch: $epoch
signed-build: $SIGNED
go: $(cat "$work/b1/out/.go-version")
image: $IMAGE
EOF
(cd "$out" && sha256sum -- *.exe *.zip >SHA256SUMS)
cat "$out/SHA256SUMS"
echo "== done: $out"
