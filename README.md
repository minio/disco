Disco
=====
Disco is a service discovery for MinIO.

The motivation is that some stateful services have the need for a static IP address to discover peers it's peers, Kubernetes offers this functionality however `kube-dns` has a large delay in announcing the new Pods, therefore we propose this layer to act immediately upon the creation of the desired pod.

Pods are automatically added to the DNS as long at the have the annotation `io.min.disco`. This annotation supports jsonpath expressions.

For example, after setting up `Disco` as a service we can configure a new Statefulset to use it to resolve the name of the peer replicas. So if we setup a `MinIO` instance that looks for it's peers at at hostname `zone-1-{0...3}.zone-1` we can indicate this to the `Disco` via the annotation:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: zone-1
  namespace: default
spec:
  podManagementPolicy: Parallel
  replicas: 4
  selector:
    matchLabels:
      app: minio
      controller: zone-1
  serviceName: zone-1
  template:
    metadata:
      labels:
        app: minio
        controller: zone-1
      annotations:
        io.min.disco: '{.metadata.name}.{.metadata.labels.controller}'
    spec:
      dnsPolicy: "None"
      dnsConfig:
        nameservers:
          - 10.110.109.99
      containers:
        - name: minio
          args:
            - server
            - http://zone-1-{0...3}.zone-1/data{1...4}
```

Deploy
---
You can deploy with [kustomize](https://github.com/kubernetes-sigs/kustomize)

```bash
kustomize build deployment/base | kubectl apply -f -
```

After deploying `disco` you should find out the IP for the `minio-disco` service since that is going to be used to configure the top level `minio.local` domain.

```bash
$ kubectl get svc minio-disco -o wide
NAME          TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)         AGE   SELECTOR
minio-disco   ClusterIP   10.109.234.52   <none>        53/UDP,53/TCP   12m   app=minio-disco

```

Here we can see the IP is `10.109.234.52` so we are going to add that to the `Corefile` stored in the `coredns` configmap inside the `kube-system` namespace.

```bash
$ kubectl -n kube-system edit configmap corends
```

and add at the end of `Corefile`

```yaml
    minio.local:53 {
        errors
        cache 30
        forward . 10.109.234.52
    }
```

The file should look like this

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: coredns
  namespace: kube-system
data:
  Corefile: |
    .:53 {
        errors
        health {
           lameduck 5s
        }
        ready
        kubernetes cluster.local in-addr.arpa ip6.arpa {
           pods insecure
           fallthrough in-addr.arpa ip6.arpa
           ttl 30
        }
        prometheus :9153
        forward . /etc/resolv.conf
        cache 30
        loop
        reload
        loadbalance
    }
    minio.local:53 {
        errors
        cache 30
        forward . 10.109.234.52
    }
```

