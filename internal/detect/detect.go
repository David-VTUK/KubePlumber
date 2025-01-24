package detect

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/David-VTUK/KubePlumber/common"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func DetectDNSImplementation(clients *common.Clients, clusterDNSConfig *common.ClusterDNSConfig) error {

	var err error

	err = DetectStandardCoreDNSDeployment(clients, clusterDNSConfig)

	if err != nil {
		log.Info("Unable to detect CoreDNS deployment, attempting to detect DNS Operator deployment")
		err = DetectOperatorDNSDeployment(clients, clusterDNSConfig)
	}

	if err != nil {
		return err
	}

	return nil
}

func DetectStandardCoreDNSDeployment(clients *common.Clients, clusterDNSConfig *common.ClusterDNSConfig) error {

	var found bool
	// Find the DNS ConfigMap
	log.Info("Attempting to find the CoreDNS ConfigMap")

	configMaps, err := clients.KubeClient.CoreV1().ConfigMaps("").List(context.TODO(), metav1.ListOptions{})

	if err != nil {
		log.Errorf("Error listing ConfigMaps: %v", err)
	}

	for _, c := range configMaps.Items {
		if c.Name == common.CoreDNSConfigMapName {
			log.Infof("Found cluster-dns ConfigMap: %v", c.Name)
			clusterDNSConfig.DNSConfigObjectName = c.Name
			clusterDNSConfig.DNSServiceNamespace = c.Namespace
			clusterDNSConfig.DNSServiceEndpointIP = c.Data["clusterDNS"]
			clusterDNSConfig.DNSServiceDomain = c.Data["clusterDomain"]
			found = true
		}
	}

	if found {
		serviceList, err := clients.KubeClient.CoreV1().Services(clusterDNSConfig.DNSServiceNamespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}

		for _, service := range serviceList.Items {
			if service.Spec.ClusterIP == clusterDNSConfig.DNSServiceEndpointIP {
				log.Info("DNS Service found: ", service.Name)
				clusterDNSConfig.DNSServiceServiceName = service.Name
				clusterDNSConfig.DNSLabelSelector = mapToString(service.Spec.Selector)
				break
			}
		}
	}

	if !found {
		return errors.New("unable to find CoreDNS ConfigMap")
	}

	return nil

}

func DetectOperatorDNSDeployment(clients *common.Clients, clusterDNSConfig *common.ClusterDNSConfig) error {

	configList, err := clients.DynamicClient.Resource(common.OpenShiftDNSGVR).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	if len(configList.Items) > 1 {
		return errors.New("more than one DNS Operator config found")
	}

	config := configList.Items[0]

	status, found, err := unstructured.NestedMap(config.Object, "status")
	if !found || err != nil {
		return fmt.Errorf("status not found in DNS object")
	}

	clusterDNSConfig.DNSConfigObjectName = config.GetName()
	clusterDNSConfig.DNSServiceEndpointIP = status["clusterIP"].(string)
	clusterDNSConfig.DNSServiceDomain = status["clusterDomain"].(string)

	serviceList, err := clients.KubeClient.CoreV1().Services("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return errors.New("unable to list services")
	}

	for _, service := range serviceList.Items {
		if service.Spec.ClusterIP == clusterDNSConfig.DNSServiceEndpointIP {
			log.Info("DNS Service found: ", service.Name)
			clusterDNSConfig.DNSServiceServiceName = service.Name
			clusterDNSConfig.DNSServiceNamespace = service.Namespace
			clusterDNSConfig.DNSLabelSelector = mapToString(service.Spec.Selector)
			break
		}
	}

	return nil
}

func mapToString(m map[string]string) string {
	var sb strings.Builder
	for key, value := range m {
		sb.WriteString(fmt.Sprintf("%s=%s ", key, value))
	}
	return strings.TrimSpace(sb.String())
}
