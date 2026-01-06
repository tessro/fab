---
id: fa-i53
status: closed
deps: []
links: []
created: 2026-01-03T12:45:54.856610705-08:00
type: task
priority: 2
---
# FAB-65: Set up GitHub Actions release workflow

Set up release workflow in .github/workflows/release.yml:
- Trigger on vX.Y.Z tags
- Use goreleaser for cross-platform builds
- Use latest actions versions (v6 where available)
- Publish binaries to GitHub releases


