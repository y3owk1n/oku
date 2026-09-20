version := `git describe --tags --always --dirty 2>/dev/null || echo dev`

default:
    @just --list

build:
    go build -ldflags "-X main.version={{ version }}" -o bin/oku ./cmd/oku

test:
    go test ./...

lint:
    golangci-lint run

fmt:
    gofumpt -w .
    golines -w .
