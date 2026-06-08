#!/bin/sh

set -e

VERSION="$1"
if [ -z "$VERSION" ]; then
	VERSION="$(go run tools/build-version.go)"
fi

mkdir -p binaries
mkdir -p microneo-$VERSION

cp LICENSE microneo-$VERSION
cp README.md microneo-$VERSION
cp LICENSE-THIRD-PARTY microneo-$VERSION
cp assets/packaging/micro.1 microneo-$VERSION
cp assets/packaging/micro.desktop microneo-$VERSION
cp assets/micro-logo-mark.svg microneo-$VERSION/microneo.svg

create_artefact_generic()
{
	mv microneo microneo-$VERSION/
	tar -czf microneo-$VERSION-$1.tar.gz microneo-$VERSION
	sha256sum microneo-$VERSION-$1.tar.gz > microneo-$VERSION-$1.tar.gz.sha
	mv microneo-$VERSION-$1.* binaries
	rm microneo-$VERSION/microneo
}

create_artefact_windows()
{
	mv microneo.exe microneo-$VERSION/
	zip -r -q -T microneo-$VERSION-$1.zip microneo-$VERSION
	sha256sum microneo-$VERSION-$1.zip > microneo-$VERSION-$1.zip.sha
	mv microneo-$VERSION-$1.* binaries
	rm microneo-$VERSION/microneo.exe
}

# Linux x64
echo "Linux x64"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 make build
if ./tools/package-deb.sh $VERSION; then
	sha256sum microneo-$VERSION-amd64.deb > microneo-$VERSION-amd64.deb.sha
	mv microneo-$VERSION-amd64.* binaries
fi
create_artefact_generic "linux64"

# Linux ARM64
echo "Linux ARM64"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 make build
create_artefact_generic "linux-arm64"

# macOS Intel
echo "macOS Intel"
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 make build
create_artefact_generic "osx"

# macOS ARM64
echo "macOS ARM64"
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 make build
create_artefact_generic "macos-arm64"

# Windows x64
echo "Windows x64"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 make build
create_artefact_windows "win64"

# Windows ARM64
echo "Windows ARM64"
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 make build
create_artefact_windows "win-arm64"

rm -rf microneo-$VERSION
