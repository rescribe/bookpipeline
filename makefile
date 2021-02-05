# See LICENSE file for copyright and license details.

default:
	@echo "To build and install use the basic go tools like so: go install ./..."
	@echo "This makefile is just for cross compiling (for which the"
	@echo "targets rescribe-osx and rescribe-w32 exist)"

rescribe-osx:
	GOOS=darwin GOARCH=amd64 go build -o $@ ./cmd/rescribe

rescribe.exe:
	GOOS=windows GOARCH=386 go build -o $@ ./cmd/rescribe
