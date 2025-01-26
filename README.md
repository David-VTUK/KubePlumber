![img](./images/logo.png)

`KubePlumber` is a Kubernetes networking utility that validates/test network connectivity in a Kubernetes cluster, including:

* Internal DNS Resolution (Inter + Intra node)
* Internal traffic testing (Inter and Intra node)
* External DNS resolution

Future additions:

* MTU Validation between nodes
* Host NIC configuration

![img](./images/example1.png)
![img](./images/example2.png)


## Example Usage

`go run kubeplumber -kubeconfig ~/.kube/config -configFile config.yaml`

An example [config.yaml](https://github.com/David-VTUK/KubePlumber/blob/main/config.yaml)