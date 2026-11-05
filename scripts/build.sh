#!/bin/bash
# Builds the release packages with `mygo build`.
#
#   scripts/build.sh                                  this machine's platform
#   scripts/build.sh [-skip-installer] windows/amd64 windows/arm64
#
# For each target it compiles the helper programs (every ./cmd/* except the
# GUI itself, e.g. wslpilot-agent and wslpilot-cli) into
# resources/windows-<arch>/, which `mygo build` ships next to the app.
# The GUI then gets packaged with NSIS (wslpilot-setup-<version>-<arch>.exe)
# unless -skip-installer is given.
# MYGO_UPDATER_PRIVATE_KEY, when set, makes mygo build sign update archives.
set -euo pipefail
cd "$(dirname "$0")/.."

flags=()
targets=()
for a in "$@"; do
	case "$a" in
	-*) flags+=("$a") ;;
	*) targets+=("$a") ;;
	esac
done
if [ ${#targets[@]} -eq 0 ]; then
	targets=("$(go env GOOS)/$(go env GOARCH)")
fi

for t in "${targets[@]}"; do
	goos=${t%/*}
	goarch=${t#*/}
	if [ "$goos" != windows ]; then
		echo "build.sh: WSLPilot only ships for Windows, not $t" >&2
		exit 2
	fi
	out="resources/$goos-$goarch"
	rm -rf "$out"
	for dir in cmd/*/; do
		cmd=$(basename "$dir")
		[ "$cmd" = wslpilot ] && continue
		mkdir -p "$out"
		CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags="-s -w" \
			-o "$out/$cmd.exe" "./cmd/$cmd"
	done
done

go tool mygo build ${flags[@]+"${flags[@]}"} -platform "$(IFS=,; echo "${targets[*]}")"
