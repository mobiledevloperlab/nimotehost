.PHONY: build desktop release check export-source
VERSION ?= 2.5.0-community
build:
	go run ./tools/build -version $(VERSION)
desktop:
	go run ./tools/build -gui -version $(VERSION)
release:
	python3 scripts/build-release.py --version $(VERSION)
check:
	python3 -m unittest discover -s test -p 'test_*.py'
	go mod verify
	go test -mod=readonly -race ./...
	go vet ./...

export-source:
	./scripts/export-source.sh
