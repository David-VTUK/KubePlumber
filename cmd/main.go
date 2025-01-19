package main

import (
	"flag"
	"os"
	"path/filepath"

	"github.com/David-VTUK/KubePlumber/internal/validate"
	"github.com/sirupsen/logrus"
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
	logLevel := flag.String("logLevel", "debug", "Log level (debug, info, warn, error, fatal, panic)")
	configFile := flag.String("configFile", "config.yaml", "Path to the config file")

	flag.Parse()

	// Check if config file exists
	_, err := os.Stat(*configFile)
	if os.IsNotExist(err) {
		logrus.Fatalf("Config file %s does not exist", *configFile)
	}

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.Fatalf("Invalid log level: %v", err)
	}
	logrus.SetLevel(level)

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

	err = validate.RunDNSTests(clientset, dnsConfigMapName, dnsConfigNamespace, configFile)
	if err != nil {
		panic(err.Error())
	}
}
