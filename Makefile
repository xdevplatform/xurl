.PHONY: build
build:
	go build -o xurl

.PHONY: install
install:
	go install

.PHONY: clean
clean:
	rm -f xurl

.PHONY: test
test:
	go test -v ./...

.PHONY: format
format:
	go fmt ./...

.PHONY: release
release:
	goreleaser release --clean