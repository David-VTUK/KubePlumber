package main

import (
	"flag"
	"os"

	"github.com/David-VTUK/KubePlumber/common"
	"github.com/David-VTUK/KubePlumber/internal/validate"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {

	config := common.Config{}
	flag.StringVar(&config.DNSConfigNamespace, "dnsConfigNamespace", "kube-system", "Namespace of the DNS ConfigMap")
	flag.StringVar(&config.DNSConfigMapName, "dnsConfigMapName", "cluster-dns", "Name of the DNS ConfigMap")
	flag.StringVar(&config.LogLevel, "logLevel", "debug", "Log level (debug, info, warn, error, fatal, panic)")
	flag.StringVar(&config.ConfigFile, "configFile", "config.yaml", "Path to the config file")
	flag.StringVar(&config.Kubeconfig, "kubeconfig", "", "(required) absolute path to the kubeconfig file")
	flag.BoolVar(&config.IsOpenShift, "isOpenShift", false, "Set to true if the cluster is OpenShift")

	flag.Parse()

	// Check if config file exists
	_, err := os.Stat(config.ConfigFile)
	if os.IsNotExist(err) {
		logrus.Fatalf("Config file %s does not exist", config.ConfigFile)
	}

	level, err := logrus.ParseLevel(config.LogLevel)
	if err != nil {
		logrus.Fatalf("Invalid log level: %v", err)
	}
	logrus.SetLevel(level)

	// use the current context in kubeconfig
	restConfig, err := clientcmd.BuildConfigFromFlags("", config.Kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		panic(err.Error())
	}

	// Create the dynamic client
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		panic(err.Error())
	}

	// Create a new instance of the Clients struct
	clients := common.Clients{
		KubeClient:    clientset,
		DynamicClient: dynamicClient,
	}

	err = validate.RunDNSTests(clients, config)
	if err != nil {
		panic(err.Error())
	}
}
