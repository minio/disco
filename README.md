Disco
=====
Disco is a Service Discovery and Internal Custom DNS for Kubernetes.

It allows to customize Pod and Service discovery by annotating them with an annotation `disco.min.io`, this annotation supports jsonpath expressions, and either adding `Disco` as a DNS service either at cluster level or pod level via `dnsPolicy`

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
        disco.min.io: '{.metadata.name}.{.metadata.labels.controller}'
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

You could have Disco announce a service by annotating it as well.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: myservice
  annotations:
    disco.min.io: mycustom.domain.xyz
  namespace: default
spec:
  clusterIP: 10.0.11.208
  ports:
  - name: http-minio
    port: 9000
    protocol: TCP
    targetPort: 9000
  selector:
    v1.min.io/instance: bigdata
  sessionAffinity: None
  type: ClusterIP
status:
  loadBalancer: {}
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
Afterwards, restart the coredns pods on the `kube-system` namespace
```bash
$ kubectl -n kube-system delete pod $(kubectl -n kube-system get pods  | grep coredns | awk '{print $1}')
```

## License
Use of `disco` is governed by the AGPLv3 license that can be found in the [LICENSE](./LICENSE) file.
