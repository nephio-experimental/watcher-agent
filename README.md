# watcheragent
Watcheragent runs on workload clusters, watches resources on the cluster based on requests, and reports back to a designated gRPC server on any resource status changes.

**Note**: Please open issues in the [nephio](https://github.com/nephio-project/nephio)
repository instead of here, and use the prefix "watcher-agent: " in the issue title.

## Description

![WatcherAgent in the system](./img/watcher-agent.jpeg)

WatcherAgent runs on workload clusters (installation of watcheragent at this point is manual), it watches the WatcherAgent CR, which gives information on the gRPC server to connect to and the resources to watch for. WatcherAgent would then create gRPC client to connect to the gRPC server specified by the CR, and starts to report status fields as requested by the WatcherAgent CR.

WatcherAgent periodically fetches the statuses of the target resources on the workload cluster, and tracks if the status values have changed, and only posts updates to these statuses to the gRPC server configured if there is any changes to those statuses under monitoring.

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Running on the cluster
1. Install Instances of Custom Resources:

```sh
make install
```

2. Build and push your image to the location specified by `IMG`:
	
```sh
make docker-build docker-push IMG=<some-registry>/watcheragent:tag
```
	
3. Deploy the controller to the cluster with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/watcheragent:tag
```

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller
UnDeploy the controller to the cluster:

```sh
make undeploy
```

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/) 
which provides a reconcile function responsible for synchronizing resources untile the desired state is reached on the cluster 

### Test It Out
1. Install the CRDs into the cluster:

```sh
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)
