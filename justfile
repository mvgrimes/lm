APP      := "lk"
VER_FILE := "./cmd/root.go"
# VERSION  := `grep -oE 'VERSION\s*=\s*".*"' ./cmd/root.go | grep -oE '[0-9]+\.[0-9]+\.[0-9]+'`
# VERSION  := shell('perl -nE\'m{VERSION\s*=\s*"(\d+\.\d+.\d+)"} && print \$1\' $1', VER_FILE)
VERSION  := `perl -nE'm{VERSION\s*=\s*"(\d+\.\d+.\d+)"} && print $1' ./cmd/root.go`

build:
  echo "Building verions {{VERSION}} of {{APP}}"
  go build -o lk main.go

# dev:
#   ${HOME}/go/bin/air

lint:
  go vet ./... || true
  golangci-lint run ./... || true
  govulncheck ./...

deploy:
  git diff --exit-code

  cd ci && make build
  git tag "{{VERSION}}"
  echo "{{VERSION}}" > ci/VERSION
  cd ci && make tag
  cd ci && make push
  # cd ci && make k8s

  git release
  git push --tags

generate:
  sqlc generate
