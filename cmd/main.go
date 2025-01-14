package main

import (
	"flag"
	"os"
	"path"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
)

const (
	defaultKubeconfig = "~/.kube/config"
)

func main() {

	// Get the kubeconfig file path
	kubeconfig, err := getConfig()
	if err != nil {
		panic(err)
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)

}

func getConfig() (string, error) {
	var filename string
	var err error

	// Check KUBECONFIG environment variable
	envKubeConfig := os.Getenv("KUBECONFIG")
	if envKubeConfig != "" {
		filename, err = homeDir(envKubeConfig)
		if err != nil {
			return "", err
		}
		if _, err = os.Stat(filename); err != nil {
			return "", err
		}
		return filename, nil
	}

	// Setup and parse the kubeconfig command-line flag
	flag.StringVar(&filename, "kubeconfig", "", "path to the kubeconfig file")
	flag.Parse()

	// Use default kubeconfig if no flag is set
	if filename == "" {
		filename = defaultKubeconfig
	}

	// Resolve home directory in path
	filename, err = homeDir(filename)
	if err != nil {
		return "", err
	}

	// Check if the resolved file path exists
	if _, err = os.Stat(filename); err != nil {
		return "", err
	}

	return filename, nil
}

func homeDir(filename string) (string, error) {
	if strings.Contains(filename, "~/") {
		homedir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		filename = strings.Replace(filename, "~/", "", 1)
		filename = path.Join(homedir, filename)
	}
	return filename, nil
}
