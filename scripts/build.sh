#!/bin/sh
# Builds windowseat and windowseat-tray into dist/<os>-<arch>/.
#
# Flags are chosen so antivirus scanners have less to object to:
# -trimpath drops local paths, symbols stay in (stripped Go binaries are
# flagged more often), and Windows builds carry the version, manifest and
# icon resources from cmd/*/rsrc_windows_*.syso. Never pack with UPX or
# obfuscate with garble. Sign Windows releases before publishing them.
set -eu

cd "$(dirname "$0")/.."
targets="${*:-linux/amd64 windows/amd64 windows/arm64}"

for target in $targets; do
	os="${target%/*}"
	arch="${target#*/}"
	out="dist/$os-$arch"
	ext=""
	gui=""
	if [ "$os" = windows ]; then
		ext=".exe"
		gui="-H=windowsgui"
	fi
	mkdir -p "$out"
	GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -o "$out/windowseat$ext" ./cmd/windowseat
	GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags="$gui" -o "$out/windowseat-tray$ext" ./cmd/windowseat-tray
	echo "built $out"
done
