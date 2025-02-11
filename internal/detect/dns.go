package detect

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/David-VTUK/KubePlumber/common"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func DetectDNSImplementation(clients *common.Clients, clusterDNSConfig *common.ClusterDNSConfig) error {
	log.Info("Attempting to detect K8s internal DNS Service")
	found, err := DetectCoreDNSByService(clients, clusterDNSConfig)
	if err != nil {
		return fmt.Errorf("error detecting CoreDNS service: %v", err)
	}
	if !found {
		return errors.New("no DNS service found in the cluster")
	}
	log.Info("DNS Service Found")

	log.Info("Attempting to acquire DNS corefile")
	err = GetDNSCoreFile(clients, clusterDNSConfig)
	if err != nil {
		return fmt.Errorf("error acquiring Corefile: %v", err)
	}

	if clusterDNSConfig.DNSServiceDomain == "" {
		return errors.New("cluster domain not found in CoreDNS configuration")
	}

	return nil
}
func DetectCoreDNSByService(clients *common.Clients, clusterDNSConfig *common.ClusterDNSConfig) (bool, error) {
	// First try kube-system namespace
	kubeSystemServices := []string{"kube-dns", "coredns", "dns-default"}
	log.Debug("Checking kube-system namespace for DNS services")
	for _, serviceName := range kubeSystemServices {
		log.Debugf("Looking for DNS service: %s in kube-system namespace", serviceName)
		service, err := clients.KubeClient.CoreV1().Services("kube-system").Get(context.TODO(), serviceName, metav1.GetOptions{})
		if err == nil {
			log.Infof("Found DNS service %s in kube-system namespace", serviceName)
			clusterDNSConfig.DNSServiceEndpointIP = service.Spec.ClusterIP
			clusterDNSConfig.DNSServiceServiceName = service.Name
			clusterDNSConfig.DNSServiceNamespace = service.Namespace
			clusterDNSConfig.DNSLabelSelector = mapToString(service.Spec.Selector)
			return true, nil
		}
	}

	// If not found in kube-system, try openshift-dns namespace
	openshiftServices := []string{"dns-default"}
	log.Debug("Checking openshift-dns namespace for DNS services")
	for _, serviceName := range openshiftServices {
		log.Debugf("Looking for DNS service: %s in openshift-dns namespace", serviceName)
		service, err := clients.KubeClient.CoreV1().Services("openshift-dns").Get(context.TODO(), serviceName, metav1.GetOptions{})
		if err == nil {
			log.Infof("Found DNS service %s in openshift-dns namespace", serviceName)
			clusterDNSConfig.DNSServiceEndpointIP = service.Spec.ClusterIP
			clusterDNSConfig.DNSServiceServiceName = service.Name
			clusterDNSConfig.DNSServiceNamespace = service.Namespace
			clusterDNSConfig.DNSLabelSelector = mapToString(service.Spec.Selector)
			return true, nil
		}
	}

	// If still not found, try listing all services as a fallback
	log.Debug("DNS service not found in standard namespaces, searching all namespaces")
	serviceList, err := clients.KubeClient.CoreV1().Services("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return false, errors.New("unable to list services")
	}

	for _, service := range serviceList.Items {
		if len(service.Spec.Ports) == 0 {
			continue
		}

		for _, port := range service.Spec.Ports {
			// First check if it's port 53 and has "dns" in the name
			if port.Port == 53 && strings.Contains(strings.ToLower(port.Name), "dns") {
				clusterDNSConfig.DNSServiceEndpointIP = service.Spec.ClusterIP
				clusterDNSConfig.DNSServiceServiceName = service.Name
				clusterDNSConfig.DNSServiceNamespace = service.Namespace
				clusterDNSConfig.DNSLabelSelector = mapToString(service.Spec.Selector)
				log.Debugf("Found DNS service with DNS port name: %s in namespace %s", service.Name, service.Namespace)
				return true, nil
			}
			// If not found, check if it's just port 53 (some distributions might not include "dns" in port name)
			if port.Port == 53 {
				clusterDNSConfig.DNSServiceEndpointIP = service.Spec.ClusterIP
				clusterDNSConfig.DNSServiceServiceName = service.Name
				clusterDNSConfig.DNSServiceNamespace = service.Namespace
				clusterDNSConfig.DNSLabelSelector = mapToString(service.Spec.Selector)
				log.Debugf("Found DNS service: %s in namespace %s", service.Name, service.Namespace)
				return true, nil
			}
		}
	}

	return false, errors.New("no DNS service found in kube-system, openshift-dns, or any other namespace")
}

func GetDNSCoreFile(clients *common.Clients, clusterDNSConfig *common.ClusterDNSConfig) error {
	// Try to get CoreDNS configmap from the same namespace as the DNS service
	if clusterDNSConfig.DNSServiceNamespace != "" {
		log.Debugf("Looking for CoreDNS configmap in DNS service namespace: %s", clusterDNSConfig.DNSServiceNamespace)
		configMap, err := clients.KubeClient.CoreV1().ConfigMaps(clusterDNSConfig.DNSServiceNamespace).Get(context.TODO(), "coredns", metav1.GetOptions{})
		if err == nil {
			log.Debug("Found CoreDNS configmap in DNS service namespace")
			// Try both cases for Corefile
			for key, value := range configMap.Data {
				if strings.ToLower(key) == "corefile" {
					log.Debugf("Found Corefile key: %s", key)
					clusterDomain, err := extractClusterDomain(value)
					if err == nil {
						log.Infof("Successfully extracted cluster domain: %s", clusterDomain)
						clusterDNSConfig.DNSServiceDomain = clusterDomain
						return nil
					}
					log.Debugf("Failed to extract cluster domain: %v", err)
				}
			}
		} else {
			log.Debugf("Error getting CoreDNS configmap from DNS service namespace: %v", err)
		}
	}

	// If not found in DNS service namespace, try kube-system
	log.Debug("Looking for CoreDNS configmap in kube-system namespace")
	configMap, err := clients.KubeClient.CoreV1().ConfigMaps("kube-system").Get(context.TODO(), "coredns", metav1.GetOptions{})
	if err == nil {
		log.Debug("Found CoreDNS configmap in kube-system namespace")
		for key, value := range configMap.Data {
			if strings.ToLower(key) == "corefile" {
				log.Debugf("Found Corefile key: %s", key)
				clusterDomain, err := extractClusterDomain(value)
				if err == nil {
					log.Infof("Successfully extracted cluster domain: %s", clusterDomain)
					clusterDNSConfig.DNSServiceDomain = clusterDomain
					return nil
				}
				log.Debugf("Failed to extract cluster domain: %v", err)
			}
		}
	} else {
		log.Debugf("Error getting CoreDNS configmap from kube-system namespace: %v", err)
	}

	// If still not found, try listing all configmaps as a fallback
	log.Debug("CoreDNS configmap not found in standard namespaces, searching all namespaces")
	configMaps, err := clients.KubeClient.CoreV1().ConfigMaps("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return errors.New("unable to list configmaps")
	}

	for _, configMap := range configMaps.Items {
		for key, value := range configMap.Data {
			if strings.ToLower(key) == "corefile" {
				log.Debugf("Found potential Corefile in namespace %s", configMap.Namespace)
				clusterDomain, err := extractClusterDomain(value)
				if err == nil {
					log.Infof("Successfully extracted cluster domain from namespace %s: %s", configMap.Namespace, clusterDomain)
					clusterDNSConfig.DNSServiceDomain = clusterDomain
					return nil
				}
				log.Debugf("Failed to extract cluster domain from namespace %s: %v", configMap.Namespace, err)
			}
		}
	}

	return errors.New("Corefile not found in any namespace")
}

func extractClusterDomain(corefile string) (string, error) {
	re := regexp.MustCompile(`kubernetes\s+([^\s]+)`)
	matches := re.FindStringSubmatch(corefile)
	if len(matches) < 2 {
		return "", errors.New("cluster domain not found in Corefile")
	}
	return matches[1], nil
}

func mapToString(m map[string]string) string {
	var sb strings.Builder
	for key, value := range m {
		sb.WriteString(fmt.Sprintf("%s=%s ", key, value))
	}
	return strings.TrimSpace(sb.String())
}
