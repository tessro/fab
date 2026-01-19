# {Component Name}

## Purpose
<!-- What is this? What problem does it solve? 1-3 sentences. -->
<!-- Include non-goals: what this component does NOT do. -->

## Interface
<!-- How do you use it? Entry points, commands, schemas. -->
<!-- Use tables for CLI commands, API endpoints, config keys. -->

## Configuration
<!-- Config keys, environment variables, file formats. -->

## Verification
<!-- How do you know it's working? Include test commands with expected output. -->
<!-- Commands in bash blocks are executable via `pave verify`. -->

Run the unit tests:
```bash
$ cargo test
test result: ok
```

Check the health endpoint:
```bash
$ curl -s http://localhost:8080/health
{"status":"healthy"}
```

## Examples
<!-- Concrete, copy-paste examples. -->
<!-- Include: happy path, realistic use case, failure case. -->

## Gotchas
<!-- Common pitfalls and how to avoid them. -->

## Decisions
<!-- Why does this design exist? What must not change? -->
<!-- Tradeoffs and constraints. -->
