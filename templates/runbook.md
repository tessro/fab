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
<!-- How to confirm success. Commands in bash blocks are executable via `pave verify`. -->

Verify the deployment completed:
```bash
$ kubectl get pods -l app=myapp | grep Running
myapp-1234   1/1     Running
```

Check the service is responding:
```bash
$ curl -s http://myapp.internal/health
OK
```

## Escalation
<!-- Who to contact if this doesn't work. -->

## Examples
<!-- Example invocations of this runbook. -->

Example deployment:
```bash
$ kubectl apply -f manifests/deployment.yaml
deployment.apps/myapp created
```
