# Runbook: {Task Name}

## When to Use
<!-- Circumstances that trigger this runbook. -->

## Preconditions
<!-- What must be true before starting. -->

## Steps
<!-- Numbered steps with commands that actually run. -->

## Rollback
<!-- How to undo if something goes wrong. -->

## Verification
<!-- How to confirm success. -->

Verify the daemon is running:
```bash
$ fab server status
🚌 fab daemon is running (pid: 12345)
```

Check the service is responding:
```bash
$ curl -s http://localhost:8080/health
{"status":"healthy"}
```

## Escalation
<!-- Who to contact if this doesn't work. -->

## Examples
<!-- Example invocations of this runbook. -->

Example service start:
```bash
$ fab server start
🚌 fab daemon started
```
