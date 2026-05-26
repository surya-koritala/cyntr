# Kubernetes — Pod stuck in Pending

A Pending pod has been accepted by the API server but the scheduler hasn't
placed it on a node, or the kubelet hasn't started the container. The cause is
almost always one of four things.

## 1. No node fits the resource request

```
kubectl describe pod <pod> -n <ns>
```

Read the bottom Events. The scheduler emits messages like:

```
0/12 nodes are available: 3 Insufficient cpu, 9 Insufficient memory.
```

Action: look at the pod's `resources.requests` vs `kubectl top nodes`. The
pod is asking for more than any node has free. Fix is either smaller requests,
larger nodes, or `cluster-autoscaler` provisioning capacity.

## 2. PVC unbound

```
kubectl get pvc -n <ns>
```

A pod referencing a `PersistentVolumeClaim` won't schedule until the claim is
`Bound`. If the PVC is `Pending`, check the StorageClass — most often the
provisioner failed (wrong zone, quota exhausted, IAM permission missing on the
EBS CSI driver). Look at:

```
kubectl describe pvc <pvc> -n <ns>
```

## 3. Taints, tolerations, node selectors

The events will say `untolerated taint <key>=<value>:NoSchedule` or `node(s)
didn't match Pod's node affinity/selector`. Either:

- The deployment is targeting a node pool that doesn't exist (typo in
  `nodeSelector`).
- A taint was added recently and the pod spec doesn't tolerate it.
- All matching nodes are cordoned for maintenance.

## 4. ImagePullSecrets / private registry

The pod scheduled but the kubelet can't pull the image. Status will flip
between Pending and ImagePullBackOff. Check:

```
kubectl get pod <pod> -n <ns> -o jsonpath='{.spec.imagePullSecrets}'
kubectl get secret <secret> -n <ns> -o yaml
```

The secret either doesn't exist in the namespace or its `.dockerconfigjson` is
expired/invalid.

## Decision tree

1. Events mention `Insufficient` → resources problem. Right-size or scale.
2. Events mention `unbound PVC` → storage problem. Check StorageClass.
3. Events mention `taint` or `affinity` → scheduling constraint. Check spec vs nodes.
4. Pod oscillates Pending/ImagePullBackOff → registry auth. Check pull secret.

If none of the four match, escalate — this is one of the unusual cases
(admission webhook rejecting, scheduler down, network plugin not ready).
