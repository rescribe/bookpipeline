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
OSXCROSSBIN=$(HOME)/src/osxcross/target/bin

EMBEDS=embed_darwin.go embed_darwin_amd64.go embed_darwin_arm64.go embed_linux.go embed_windows.go embed_other.go
GODEPS=gui.go main.go $(EMBEDS)

all: dist/linux/rescribe dist/darwin/rescribe.zip dist/windows/rescribe.exe

dist/linux/rescribe: $(GODEPS)
	go generate
	mkdir -p dist/linux
	GOOS=linux GOARCH=amd64 go build -tags embed -o $@ .

build/darwin_amd64/rescribe: $(GODEPS)
	go generate
	mkdir -p build/darwin_amd64
	PATH="$(PATH):$(OSXCROSSBIN)" CC="o64-clang" CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -tags embed -o $@ .

build/darwin_arm64/rescribe: $(GODEPS)
	go generate
	mkdir -p build/darwin_arm64
	PATH="$(PATH):$(OSXCROSSBIN)" CC="oa64-clang" CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -tags embed -o $@ .

build/darwin/rescribe: build/darwin_amd64/rescribe build/darwin_arm64/rescribe
	mkdir -p build/darwin
	PATH="$(PATH):$(OSXCROSSBIN)" lipo -create build/darwin_amd64/rescribe build/darwin_arm64/rescribe -output $@

build/darwin/Rescribe.app: build/darwin/rescribe
	go install fyne.io/fyne/v2/cmd/fyne@v2.1.2
	fyne package --name Rescribe --exe build/darwin/rescribe --os darwin --icon icon.png
	mv Rescribe.app $@

dist/darwin/rescribe.zip: build/darwin/Rescribe.app
	mkdir -p dist/darwin
	cd build/darwin; zip -r ../../dist/darwin/rescribe.zip Rescribe.app

build/windows/rescribe-bin.exe: $(GODEPS)
	go generate
	mkdir -p build/windows
	CC="x86_64-w64-mingw32-gcc" CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build -tags embed -o $@ .

dist/windows/rescribe.exe: build/windows/rescribe-bin.exe
	mkdir -p dist/windows
	CC="x86_64-w64-mingw32-gcc" fyne package --name Rescribe --exe build/windows/rescribe-bin.exe --os windows --icon icon.png
	mv rescribe.exe $@

clean:
	rm -rf dist build
