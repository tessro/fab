# Getting Started

## Installation

### With Go

```bash
go install github.com/tessro/fab/cmd/fab@latest
```

### From Source

```bash
git clone https://github.com/tessro/fab
cd fab
go build -o fab ./cmd/fab
```

## Quick Start

### 1. Start the daemon

```bash
fab server start
```

### 2. Add a project

```bash
# From a local path
fab project add /path/to/your/project --name myproject --max-agents 3

# Or from a git URL
fab project add git@github.com:user/repo.git --name myproject
```

### 3. Configure permissions (optional)

Enable autonomous mode with LLM-based authorization:

```bash
fab project config set myproject permissions-checker llm
```

### 4. Start orchestration

```bash
fab project start myproject
```

### 5. Watch the magic happen

```bash
fab tui
```

Kick back while the TUI shows all running agents claiming tasks and shipping code. Grab a snack, you've earned it.

## Configuration

Global config lives at `~/.config/fab/config.toml`:

```toml
# Logging level: debug, info, warn, error
log_level = "info"

# API Provider Configuration
[providers.anthropic]
api-key = "sk-ant-..."  # Or use ANTHROPIC_API_KEY env var

# LLM Authorization Settings
[llm_auth]
provider = "anthropic"
model = "claude-haiku-4-5"
```

For full configuration options, see the [Configuration](./components/configuration.md) documentation.

## Issue Backends

fab can pull tasks from different issue tracking systems:

### GitHub Issues

```bash
fab project config set myproject issue-backend gh
```

### tk (ticket)

```bash
fab project config set myproject issue-backend tk
```

For more details on issue backends, see the [Issue Backends](./components/issue-backends.md) documentation.

## Next Steps

- Browse the [component documentation](./index.md) for detailed guides
- Check out the [Architecture](./components/architecture.md) guide
- Questions? [Talk to Tess](https://x.com/ptr)
