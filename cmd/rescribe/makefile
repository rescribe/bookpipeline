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

EMBEDS=embed_darwin_amd64.go embed_linux.go embed_windows.go gui.go embed_darwin_arm64.go embed_other.go
GODEPS=gui.go main.go $(EMBEDS)

all: dist/linux/rescribe dist/darwin_universal/Rescribe.app dist/windows/rescribe.exe

dist/linux/rescribe: $(GODEPS)
	go generate
	mkdir -p dist/linux
	GOOS=linux GOARCH=amd64 go build -o $@ .

dist/darwin_amd64/rescribe: $(GODEPS)
	go generate
	mkdir -p dist/darwin_amd64
	PATH="$(PATH):$(OSXCROSSBIN)" CC="o64-clang" CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -o $@ .

dist/darwin_arm64/rescribe: $(GODEPS)
	go generate
	mkdir -p dist/darwin_arm64
	PATH="$(PATH):$(OSXCROSSBIN)" CC="oa64-clang" CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -o $@ .

dist/darwin_universal/rescribe: dist/darwin_amd64/rescribe dist/darwin_arm64/rescribe
	mkdir -p dist/darwin_universal
	PATH="$(PATH):$(OSXCROSSBIN)" lipo -create dist/darwin_amd64/rescribe dist/darwin_arm64/rescribe -output $@

dist/darwin_universal/Rescribe.app: dist/darwin_universal/rescribe
	go install fyne.io/fyne/v2/cmd/fyne@latest
	fyne package --name Rescribe --exe dist/darwin_universal/rescribe --os darwin --icon icon.png
	mv Rescribe.app $@

dist/windows/rescribe-bin.exe: $(GODEPS)
	go generate
	mkdir -p dist/windows
	CC="x86_64-w64-mingw32-gcc" CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build -o $@ .

dist/windows/rescribe.exe: dist/windows/rescribe-bin.exe
	CC="x86_64-w64-mingw32-gcc" fyne package --name Rescribe --exe dist/windows/rescribe-bin.exe --os windows --icon icon.png
	mv rescribe.exe $@
