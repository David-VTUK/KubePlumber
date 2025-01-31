package main

import (
	"flag"
	"os"

	"github.com/David-VTUK/KubePlumber/common"
	"github.com/David-VTUK/KubePlumber/internal/detect"
	"github.com/David-VTUK/KubePlumber/internal/setup"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {

	runConfig := common.RunConfig{}
	flag.StringVar(&runConfig.LogLevel, "loglevel", "debug", "Log level (debug, info, warn, error, fatal, panic)")
	flag.StringVar(&runConfig.ConfigFile, "config", "config.yaml", "Path to the config file")
	flag.StringVar(&runConfig.Kubeconfig, "kubeconfig", "~/.kube/config", "(required) absolute path to the kubeconfig file")
	flag.StringVar(&runConfig.TestNamespace, "namespace", "default", "Namespace to run tests in")

	flag.Parse()

	// Check if config file exists
	_, err := os.Stat(runConfig.ConfigFile)
	if os.IsNotExist(err) {
		log.Fatalf("Config file %s does not exist", runConfig.ConfigFile)
	}

	level, err := log.ParseLevel(runConfig.LogLevel)
	if err != nil {
		log.Fatalf("Invalid log level: %v", err)
	}
	log.SetLevel(level)

	// use the current context in kubeconfig
	restConfig, err := clientcmd.BuildConfigFromFlags("", runConfig.Kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	restConfig.QPS = common.K8sClientqps
	restConfig.Burst = common.K8sClientBurst

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
		Timeout:       common.K8sClientTimeout,
	}

	// Create namespace if it does not exist
	err = setup.CreateNamespace(*clients.KubeClient, runConfig.TestNamespace)

	if err != nil {
		log.Info(err)
	}

	/*

		var clusterDNSConfig common.ClusterDNSConfig

		log.Info("Detecting DNS Service")
		err = detect.DetectDNSImplementation(&clients, &clusterDNSConfig)
		if err != nil {
			log.Info(err)
		}

		log.Info("Running Internal and External DNS Tests")
		err = validate.RunDNSTests(clients, runConfig, clusterDNSConfig)
		if err != nil {
			log.Info(err)
		}

		log.Info("Running Overlay Network Tests")
		err = validate.RunOverlayNetworkTests(clients, restConfig, clusterDNSConfig.DNSServiceDomain, runConfig)
		if err != nil {
			log.Info(err)
		}

	*/

	err = detect.NICAttributes(&clients, runConfig)
	if err != nil {
		log.Info(err)
	}

}
