package common

const (
	// Reduced QPS and burst to avoid rate limiting
	K8sClientBurst = 20
	K8sClientqps   = 10
	// Increased timeout to allow for DNS resolution tests
	K8sClientTimeout = 120
)
