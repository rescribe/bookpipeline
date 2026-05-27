# See LICENSE file for copyright and license details.
#
# This is a set of make(1) rules to cross compile rescribe
# from Linux to other architectures - as we use Fyne, CGO
# is required, so we have to do more to cross compile than
# just rely on the standard go tools. It relies on osxcross
# being set up for the Mac builds, and mingw-w64 being
# installed for the Windows build.
#
# The standard go tools work perfectly for native builds on
# all architectures - note that "go generate" needs to be
# run before building to download the dependencies which are
# embedded.

# For osxcross, there are many versions of the MacOS SDK
# that are too old or too new to build Rescribe correctly.
# SDK 11.3, as extracted from XCode 12.5.1, seems to work
# perfectly for us.
OSXCROSSDIR=/opt/osxcross
# go < 1.25 needed for the MacOS SDK version we use due to
# https://go-review.googlesource.com/c/go/+/654376
# could probably use a newer SDK, but finding and building
# a working version is a pain
OSXGOVERSION=go1.24.13

GOPATH=$(HOME)/go

EMBEDS=embed_darwin.go embed_darwin_amd64.go embed_darwin_arm64.go embed_linux.go embed_windows.go embed_other.go
GODEPS=gui.go main.go $(EMBEDS)
VERSION=1.4.0

all: dist/linux/rescribe dist/linux/wayland/rescribe dist/darwin/rescribe.zip dist/windows/rescribe.exe

dist/linux/rescribe: $(GODEPS)
	go generate
	mkdir -p dist/linux
	GOOS=linux GOARCH=amd64 go build -tags embed -o $@ .

dist/linux/wayland/rescribe: $(GODEPS)
	go generate
	mkdir -p dist/linux/wayland
	GOOS=linux GOARCH=amd64 go build -tags embed,wayland -o $@ .

build/darwin_amd64/rescribe: $(GODEPS)
	go generate
	mkdir -p build/darwin_amd64
	LD_LIBRARY_PATH="$(OSXCROSSDIR)/lib" PATH="$(PATH):$(OSXCROSSDIR)/bin" CC="o64-clang" CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 GOTOOLCHAIN=$(OSXGOVERSION) go build -tags embed -o $@ .

build/darwin_arm64/rescribe: $(GODEPS)
	go generate
	mkdir -p build/darwin_arm64
	LD_LIBRARY_PATH="$(OSXCROSSDIR)/lib" PATH="$(PATH):$(OSXCROSSDIR)/bin" CC="oa64-clang" CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 GOTOOLCHAIN=$(OSXGOVERSION) go build -tags embed -o $@ .

build/darwin/rescribe: build/darwin_amd64/rescribe build/darwin_arm64/rescribe
	mkdir -p build/darwin
	LD_LIBRARY_PATH="$(OSXCROSSDIR)/lib" PATH="$(PATH):$(OSXCROSSDIR)/bin" lipo -create build/darwin_amd64/rescribe build/darwin_arm64/rescribe -output $@

build/darwin/Rescribe.app: build/darwin/rescribe
	go install fyne.io/tools/cmd/fyne@v1.7.0
	PATH="$(PATH):$(OSXCROSSDIR)/bin" $(GOPATH)/bin/fyne package --release --certificate Rescribe --id xyz.rescribe.rescribe --tags embed --name Rescribe --exe build/darwin/rescribe --os darwin --icon icon.png --app-version $(VERSION)
	# TODO: switch to using rcodesign instead so don't need a MacOS box:
	# https://gregoryszorc.com/docs/apple-codesign/stable/
	#codesign -s Rescribe Rescribe.app
	mv Rescribe.app $@

dist/darwin/rescribe.zip: build/darwin/Rescribe.app
	mkdir -p dist/darwin
	cd build/darwin; zip -r ../../dist/darwin/rescribe.zip Rescribe.app

dist/windows/rescribe.exe: $(GODEPS)
	mkdir -p dist/windows
	go generate
	go install fyne.io/tools/cmd/fyne@v1.7.0
	CC="x86_64-w64-mingw32-gcc" $(GOPATH)/bin/fyne package --tags embed --name Rescribe --exe $@ --os windows --icon icon.png --app-version $(VERSION)

# used for flatpak building
modules.tar.xz: ../../go.mod
	go mod vendor
	cd ../.. && tar c vendor | xz > cmd/rescribe/$@

clean:
	rm -rf dist build
	rm -rf ../../vendor
