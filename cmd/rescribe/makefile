# See LICENSE file for copyright and license details.

all: dist/linux/rescribe dist/darwin_amd64/rescribe dist/darwin_arm64/rescribe dist/windows/rescribe.exe

dist/linux/rescribe:
	mkdir -p dist/linux
	GOOS=linux GOARCH=amd64 go build -o $@ .

dist/darwin_amd64/rescribe:
	mkdir -p dist/darwin_amd64
	GOOS=darwin GOARCH=amd64 go build -o $@ .

dist/darwin_arm64/rescribe:
	mkdir -p dist/darwin_arm64
	GOOS=darwin GOARCH=arm64 go build -o $@ .

dist/windows/rescribe.exe:
	mkdir -p dist/windows
	GOOS=windows GOARCH=386 go build -o $@ .
