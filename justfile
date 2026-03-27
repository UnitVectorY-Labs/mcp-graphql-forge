
# Commands for mcp-graphql-forge
default:
  @just --list
# Build mcp-graphql-forge with Go
build:
  go build ./...

# Run tests for mcp-graphql-forge with Go
test:
  go clean -testcache
  go test ./...