package main

import (
	"flag"
	"os"

	"github.com/David-VTUK/KubePlumber/common"
	"github.com/David-VTUK/KubePlumber/internal/cleanup"
	"github.com/David-VTUK/KubePlumber/internal/setup"
	"github.com/David-VTUK/KubePlumber/internal/validate"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {

	runConfig := common.RunConfig{}
	flag.StringVar(&runConfig.LogLevel, "loglevel", "debug", "Log level (debug, info, warn, error, fatal, panic)")
	flag.StringVar(&runConfig.ConfigFile, "config", "config.yaml", "Path to the config file")
	flag.StringVar(&runConfig.Kubeconfig, "kubeconfig", "", "(required) absolute path to the kubeconfig file")
	flag.StringVar(&runConfig.TestNamespace, "namespace", "default", "Namespace to run tests in")
	flag.Parse()

	if runConfig.Kubeconfig == "" {
		log.Info("Kubeconfig not provided, attempting to use KUBECONFIG environment variable")
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig != "" {
			runConfig.Kubeconfig = kubeconfig
			log.Infof("Using KUBECONFIG from environment: %s", kubeconfig)
		} else {
			log.Info("KUBECONFIG environment variable not set, attempting to use load ~/.kube/config")
			homedir, err := os.UserHomeDir()
			if err != nil {
				log.Fatalf("Error getting home directory: %v", err)
			}

			_, err = os.Stat(homedir + "/.kube/config")
			if err != nil {
				log.Fatalf("Kubeconfig not provided and default kubeconfig not found: %v", err)
			} else {
				runConfig.Kubeconfig = homedir + "/.kube/config"
			}
		}
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

	var clusterDNSConfig common.ClusterDNSConfig

	/*
		log.Info("Detecting DNS Service")
		err = detect.DetectDNSImplementation(&clients, &clusterDNSConfig)
		if err != nil {
			log.Errorf("Tests Aborted due to: %s", err)
			cleanup.RemoveTestPods(clients, runConfig)
			os.Exit(1)
		}

		log.Info("Running Internal and External DNS Tests")
		err = validate.RunDNSTests(clients, runConfig, clusterDNSConfig)
		if err != nil {
			log.Errorf("Tests Aborted due to: %s", err)
			cleanup.RemoveTestPods(clients, runConfig)
			os.Exit(1)
		}

		log.Info("Running Overlay Network Tests")
		err = validate.RunOverlayNetworkTests(clients, restConfig, clusterDNSConfig.DNSServiceDomain, runConfig)
		if err != nil {
			log.Errorf("Tests Aborted due to: %s", err)
			cleanup.RemoveTestPods(clients, runConfig)
			os.Exit(1)
		}

		err = detect.NICAttributes(clients, runConfig)
		if err != nil {
			log.Errorf("Tests Aborted due to: %s", err)
			cleanup.RemoveTestPods(clients, runConfig)
			os.Exit(1)
		}
	*/

	log.Info("Running Overlay Network Speed Tests")
	err = validate.RunOverlayNetworkSpeedTests(clients, restConfig, clusterDNSConfig.DNSServiceDomain, runConfig)
	if err != nil {
		log.Errorf("Tests Aborted due to: %s", err)
		cleanup.RemoveTestPods(clients, runConfig)
		os.Exit(1)
	}

}
