package common

import (
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Clients struct contains the default Kubernetes client as well as a optional, dynamic client

type Clients struct {
	KubeClient    *kubernetes.Clientset
	DynamicClient *dynamic.DynamicClient
	Timeout       time.Duration
}

// DNSConfig is the top-level struct corresponding to the YAML structure.
type DNSConfig struct {
	InternalDNS []DNSRecord `yaml:"internal_dns"`
	ExternalDNS []DNSRecord `yaml:"external_dns"`
}

// DNSRecord represents each DNS entry with a "name" field.
type DNSRecord struct {
	Name string `yaml:"name"`
}

// Config struct contains the configuration options for querying the Kubernetes cluster.
type RunConfig struct {
	Kubeconfig    string
	LogLevel      string
	ConfigFile    string
	TestNamespace string
}

type ClusterDNSConfig struct {
	DNSServiceEndpointIP  string
	DNSServiceServiceName string
	DNSServiceDomain      string
	DNSServiceNamespace   string
	DNSLabelSelector      string
}

var OpenShiftDNSGVR = schema.GroupVersionResource{
	Group:    "operator.openshift.io",
	Version:  "v1",
	Resource: "dnses",
}

type NetworkInterface struct {
	Name string `json:"name"`
	//Flags            string `json:"flags"`
	MTU          int    `json:"mtu"`
	MAC          string `json:"mac"`
	Up           bool   `json:"up"`
	Broadcast    bool   `json:"broadcast"`
	Loopback     bool   `json:"loopback"`
	PointToPoint bool   `json:"pointtopoint"`
	Multicast    bool   `json:"multicast"`
	Running      bool   `json:"running"`
}

type NetworkInterfaces struct {
	Interfaces []NetworkInterface `json:"interfaces"`
}
