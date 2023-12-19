# NodePort LoadBalancer

This tool provides a tautological implementation of a Kubernetes LoadBalancer.
Rather than allocating dynamic IP addresses and ports on cloud infrastructure,
it simply sets the LoadBalancer ingresses to the node IP addresses on the
nodePort's assigned by Kubernetes.
This allows constrained environments like KinD, k3s, etc, to have load balancer
support.

## Build

```bash
go build main.go
# Or
docker build .
```

## Tests

Requires ginkgo, docker, kind, kubectl, and helm to be on the path, and docker to be running.

```bash
ginkgo run -vv .
```

## Deploying

```bash
helm upgrade --install ./deploy/helm/nodeport-load-balancer
```
