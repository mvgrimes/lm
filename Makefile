APP=lk
VERSION_FILE=./cmd/root.go
VERSION:=$(shell grep -oE 'VERSION\s*=\s*".*"' ${VERSION_FILE} | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' )

build:
	go build -o lk main.go

# dev:
#		${HOME}/go/bin/air

lint:
	go vet ./... || true
	golangci-lint run ./... || true
	govulncheck ./...

deploy:
	git diff --exit-code

	cd ci && make build
	git tag "${VERSION}"
	echo "${VERSION}" > ci/VERSION
	cd ci && make tag
	# cd ci && make push
	# cd ci && make k8s

	git release
	git push --tags

generate:
	sqlc generate
