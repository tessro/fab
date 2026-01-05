---
id: fa-23m
status: closed
deps: []
links: []
created: 2026-01-03T12:45:54.48034414-08:00
type: task
priority: 2
---
# FAB-64: Set up GitHub Actions CI workflow

Set up CI workflow in .github/workflows/ci.yml:
- Use latest actions versions (v6 where available)
- Run on push and PR to main
- golangci-lint for linting
- go test for unit tests
- go build to verify compilation


