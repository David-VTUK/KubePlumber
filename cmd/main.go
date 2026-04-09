package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/David-VTUK/KubePlumber/common"
	"github.com/David-VTUK/KubePlumber/internal/cleanup"
	"github.com/David-VTUK/KubePlumber/internal/detect"
	"github.com/David-VTUK/KubePlumber/internal/setup"
	"github.com/David-VTUK/KubePlumber/internal/validate"
	"github.com/David-VTUK/KubePlumber/internal/webserver"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// version is set at build time via -ldflags "-X main.version=<tag>".
var version = "dev"

// openBrowser launches the user's default browser after a short delay to allow
// the HTTP server time to begin listening.
func openBrowser(url string) {
	go func() {
		time.Sleep(500 * time.Millisecond)
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "linux":
			cmd = exec.Command("xdg-open", url)
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			log.Debugf("Unsupported OS for auto browser open: %s", runtime.GOOS)
			return
		}
		if err := cmd.Start(); err != nil {
			log.Debugf("Could not open browser: %v", err)
		}
	}()
}

func main() {

	runConfig := common.RunConfig{}
	flag.StringVar(&runConfig.LogLevel, "loglevel", "info", "Log level (debug, info, warn, error, fatal, panic)")
	flag.StringVar(&runConfig.ConfigFile, "config", "config.yaml", "Path to the config file")
	flag.StringVar(&runConfig.Kubeconfig, "kubeconfig", "", "(required) absolute path to the kubeconfig file")
	flag.StringVar(&runConfig.TestNamespace, "namespace", "default", "Namespace to run tests in")
	flag.IntVar(&runConfig.WebPort, "webport", 8080, "Port for the web results UI")
	flag.BoolVar(&runConfig.NoWait, "no-wait", false, "Exit immediately after tests complete instead of keeping the web server running")
	flag.Parse()

	// GitHub Actions and most CI systems set CI=true; treat that the same as --no-wait.
	if os.Getenv("CI") == "true" {
		runConfig.NoWait = true
	}

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

	// Initialise the results store and register all test sections up front so
	// the web UI can show the full section list immediately on load.
	results := common.NewResultsStore()
	results.AddSection(common.SectionDNSInternal, []string{
		"From (Node)", "From (Pod)", "To (Node)", "To (Pod)", "Intra/Inter", "Status", "Domain", "Attempt",
	})
	results.AddSection(common.SectionDNSExternal, []string{
		"From (Node)", "From (Pod)", "To (Node)", "To (Pod)", "Intra/Inter", "Status", "Domain", "Attempt",
	})
	results.AddSection(common.SectionOverlay, []string{
		"From (Node)", "From (Pod)", "To (Node)", "To (Pod)", "Status", "Protocol",
	})
	results.AddSection(common.SectionNIC, []string{
		"Node", "Interface", "MAC", "MTU", "Up", "Broadcast", "Loopback", "PointToPoint", "Multicast",
	})
	results.AddSection(common.SectionSpeedTest, []string{
		"From (Node)", "From (Pod)", "To (Node)", "To (Pod)", "Bitrate (Sender) megabit/sec", "Bitrate (Receiver) megabit/sec",
	})

	// Start the web UI server in the background.
	ws := webserver.New(results, runConfig.WebPort)
	ws.Start()
	if !runConfig.NoWait {
		openBrowser(fmt.Sprintf("http://localhost:%d", runConfig.WebPort))
	}

	// Intercept SIGINT/SIGTERM so that Ctrl+C always triggers a clean removal
	// of test resources regardless of which stage the run is at.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Infof("Received signal %s — cleaning up test resources before exit", sig)
		if err := cleanup.RemoveTestPods(clients, runConfig); err != nil {
			log.Errorf("Error during cleanup: %v", err)
		}
		os.Exit(130)
	}()

	// Create namespace if it does not exist
	err = setup.CreateNamespace(*clients.KubeClient, runConfig.TestNamespace)

	if err != nil {
		log.Info(err)
	}

	var clusterDNSConfig common.ClusterDNSConfig

	log.Info("Detecting DNS Service")
	err = detect.DetectDNSImplementation(&clients, &clusterDNSConfig)
	if err != nil {
		log.Errorf("Tests Aborted due to: %s", err)
		cleanup.RemoveTestPods(clients, runConfig)
		os.Exit(1)
	}

	log.Info("Running Internal and External DNS Tests")
	err = validate.RunDNSTests(clients, runConfig, clusterDNSConfig, results)
	if err != nil {
		log.Errorf("Tests Aborted due to: %s", err)
		cleanup.RemoveTestPods(clients, runConfig)
		os.Exit(1)
	}

	log.Info("Running Overlay Network Tests")
	err = validate.RunOverlayNetworkTests(clients, restConfig, clusterDNSConfig.DNSServiceDomain, runConfig, results)
	if err != nil {
		log.Errorf("Tests Aborted due to: %s", err)
		cleanup.RemoveTestPods(clients, runConfig)
		os.Exit(1)
	}

	err = detect.NICAttributes(clients, runConfig, results)
	if err != nil {
		log.Errorf("Tests Aborted due to: %s", err)
		cleanup.RemoveTestPods(clients, runConfig)
		os.Exit(1)
	}

	log.Info("Running Overlay Network Speed Tests")
	err = validate.RunOverlayNetworkSpeedTests(clients, restConfig, clusterDNSConfig.DNSServiceDomain, runConfig, results)
	if err != nil {
		log.Errorf("Tests Aborted due to: %s", err)
		cleanup.RemoveTestPods(clients, runConfig)
		os.Exit(1)
	}

	log.Info("Cleaning up test resources")
	err = cleanup.RemoveTestPods(clients, runConfig)
	if err != nil {
		log.Errorf("Error cleaning up test resources: %s", err)
	}

	results.SetAllComplete()

	if runConfig.NoWait {
		log.Info("All tests complete — exiting")
		return
	}

	log.Infof("All tests complete — results available at http://localhost:%d (press Ctrl+C to exit)", runConfig.WebPort)

	// Block until the user presses Ctrl+C. The signal goroutine above handles
	// cleanup and exits, so we just wait here.
	<-sigCh
	log.Info("Shutting down")
}
