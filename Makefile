.PHONY: build desktop check export-source
VERSION ?= 1.0.0-dev
build:
	go run ./tools/build -version $(VERSION)
desktop:
	go run ./tools/build -gui -version $(VERSION)
check:
	python3 -m unittest discover -s test -p 'test_*.py'
	go mod verify
	go test -mod=readonly -race ./...
	go vet ./...

export-source:
	./scripts/export-source.sh
