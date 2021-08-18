# See LICENSE file for copyright and license details.

all: rescribe-osx rescribe-osx-m1 rescribe.exe rescribe

rescribe:
	GOOS=linux GOARCH=amd64 go build -o $@ .

rescribe-osx:
	GOOS=darwin GOARCH=amd64 go build -o $@ .

rescribe-osx-m1:
	GOOS=darwin GOARCH=arm64 go build -o $@ .

rescribe.exe:
	GOOS=windows GOARCH=386 go build -o $@ .
