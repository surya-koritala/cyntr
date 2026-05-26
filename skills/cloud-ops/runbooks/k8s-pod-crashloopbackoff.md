# Kubernetes — Pod in CrashLoopBackOff

A pod enters CrashLoopBackOff when its container exits non-zero and the
kubelet has restarted it enough times to start backing off. The diagnosis
flow below isolates the cause in under five minutes 90% of the time.

## Standard diagnosis flow

### 1. Get the pod state

```
kubectl get pod <pod> -n <ns> -o wide
```

Note the `RESTARTS` count and `AGE`. A pod with 5 restarts in 2 minutes is a
fast-fail (startup error). 5 restarts over 6 hours is a slow-fail (memory
leak, dependency timeout, periodic crash).

### 2. Describe — read the Events and container state

```
kubectl describe pod <pod> -n <ns>
```

Look at:

- `State` and `Last State`. `Last State` includes the **exit code** and
  **reason** — these are the highest-signal fields in the entire output.
- `Events` at the bottom — image pull, scheduling, liveness probe failures.

Exit code lookups:

| Code | Meaning |
|------|---------|
| 0    | Clean exit (something is killing the process intentionally) |
| 1    | Generic application error |
| 137  | SIGKILL — usually OOMKilled. Check `Reason: OOMKilled`. |
| 139  | SIGSEGV — segfault. Check the binary or a native dep. |
| 143  | SIGTERM — pod was asked to terminate. Check liveness probe. |

### 3. Read logs from the previous instance

```
kubectl logs <pod> -n <ns> --previous --tail=200
```

`--previous` is essential — the current container is still starting; its logs
won't include the actual crash. If multi-container, add `-c <container>`.

### 4. Check resources and the image

If `OOMKilled`: pull `resources.limits.memory` from the spec, compare with the
working set the app actually needs. The fix is usually raising the limit OR
fixing a leak — don't just bump the number.

If `ImagePullBackOff` in the events: the image tag doesn't exist or the
registry credentials are missing. Check the imagePullSecrets and the digest.

### 5. The single remediation

A CrashLoopBackOff fix is almost never "delete the pod." It's one of:

- Fix the config/secret/configmap the container reads on boot.
- Roll back the deployment to the previous revision.
- Bump memory limit (only after confirming the working set).
- Fix the liveness probe path/port/timeout if the probe was killing a healthy app.

Propose ONE step. Then stop and let a human apply it.
