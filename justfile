# Only release tags count. The tag "nightly" moves, so it would name every local
# build "nightly".
version := `git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev`

default:
    @just --list

build:
    CGO_ENABLED=0 go build -ldflags "-X main.version={{ version }}" -o bin/oku ./cmd/oku

# The cli tests run as parallel processes, which Windows skips.
test:
    {{ if os() == "windows" { "go test ./..." } else { ".github/scripts/go-test.sh" } }}

lint:
    golangci-lint run

fmt:
    gofumpt -w .
    golines -w .
