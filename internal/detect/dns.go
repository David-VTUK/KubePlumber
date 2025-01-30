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

	if found {
		log.Info("DNS Service Found")
	}

	if err != nil {
		log.Errorf("Error detecting CoreDNS by ConfigMap: %v", err)
	}

	log.Info("Attempting to acquire DNS corefile")
	err = GetDNSCoreFile(clients, clusterDNSConfig)
	if err != nil {
		log.Errorf("Error acquiring Corefile: %v", err)
	}

	return nil
}
func DetectCoreDNSByService(clients *common.Clients, clusterDNSConfig *common.ClusterDNSConfig) (bool, error) {

	serviceList, err := clients.KubeClient.CoreV1().Services("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return false, errors.New("unable to list services")
	}

	for _, service := range serviceList.Items {
		if len(service.Spec.Ports) == 0 {
			continue
		}

		for _, port := range service.Spec.Ports {
			if port.Port == 53 && strings.Contains(strings.ToLower(port.Name), "dns") {
				clusterDNSConfig.DNSServiceEndpointIP = service.Spec.ClusterIP
				clusterDNSConfig.DNSServiceServiceName = service.Name
				clusterDNSConfig.DNSServiceNamespace = service.Namespace
				clusterDNSConfig.DNSLabelSelector = mapToString(service.Spec.Selector)
				return true, nil
			}
		}
	}

	return false, nil
}

func GetDNSCoreFile(clients *common.Clients, clusterDNSConfig *common.ClusterDNSConfig) error {

	configMaps, err := clients.KubeClient.CoreV1().ConfigMaps("").List(context.TODO(), metav1.ListOptions{})

	if err != nil {
		return errors.New("unable to list configmaps")
	}

	for _, configMap := range configMaps.Items {
		if corefile, exists := configMap.Data["Corefile"]; exists {
			clusterDomain, err := extractClusterDomain(corefile)
			if err != nil {
				return err
			}
			clusterDNSConfig.DNSServiceDomain = clusterDomain
			return nil
		}

	}

	log.Info("Corefile not found")
	return nil
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
