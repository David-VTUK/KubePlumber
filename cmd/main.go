package main

import (
	"flag"
	"path/filepath"

	"github.com/David-VTUK/KubePlumber/internal/validate"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {

	var kubeconfig *string
	var dnsConfigMapName *string
	var dnsConfigNamespace *string

	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}

	dnsConfigNamespace = flag.String("dnsConfigNamespace", "kube-system", "Namespace of the DNS ConfigMap")
	dnsConfigMapName = flag.String("dnsConfigMapName", "cluster-dns", "Name of the DNS ConfigMap")

	flag.Parse()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	err = validate.RunDNSTests(clientset, dnsConfigMapName, dnsConfigNamespace)
	if err != nil {
		panic(err.Error())
	}
}
