package validate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func RunDNSTests(clientset *kubernetes.Clientset, clusterDNSConfigMapName *string, clusterDNSNamespace *string) error {

	var err error

	// Check the existence of the DNS config map and extract the service endpoint IP and domain
	dnsLabelSelector, err := checkDNSConfigMap(clientset, *clusterDNSNamespace, *clusterDNSConfigMapName)
	if err != nil {
		return err
	}

	// Check the corresponding service endpoints for running DNS pods
	err = checkDNSPods(clientset, *clusterDNSNamespace, dnsLabelSelector)
	if err != nil {
		return err
	}

	// Check the corresponding Pods are correctly resolving internal DNS requests

	log.Println("dnsLabelSelector", dnsLabelSelector)
	err = checkInternalDNSResolution(clientset, *clusterDNSNamespace, dnsLabelSelector)

	return err

}

func checkDNSConfigMap(clientset *kubernetes.Clientset, clusterDNSNamespace, clusterDNSConfigMapName string) (string, error) {

	log.Info("Getting cluster-dns ConfigMap")

	config, err := clientset.CoreV1().ConfigMaps(clusterDNSNamespace).Get(context.TODO(), clusterDNSConfigMapName, metav1.GetOptions{})
	if err != nil {
		log.Error("Error getting CoreDNS ConfigMap", err)
		return "", err
	}

	clusterDNSDomain := config.Data["clusterDomain"]
	clusterDNSEndpoint := config.Data["clusterDNS"]

	log.Infof("Cluster DNS Domain: %s", clusterDNSDomain)
	log.Infof("Cluster DNS Endpoint: %s", clusterDNSEndpoint)

	serviceList, err := clientset.CoreV1().Services(clusterDNSNamespace).List(context.TODO(), metav1.ListOptions{
		FieldSelector: "spec.clusterIP=" + clusterDNSEndpoint,
	})

	if err != nil {
		log.Error("Error getting k8s DNS Service", err)
	}

	if len(serviceList.Items) == 0 {
		log.Error("No DNS Service found")
		return "", err
	}

	if len(serviceList.Items) > 1 {
		log.Error("Multiple DNS Services found")
		return "", err
	}

	dnsService := serviceList.Items[0]

	endpoints, err := clientset.CoreV1().Endpoints(dnsService.Namespace).Get(context.TODO(), dnsService.Name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	if len(endpoints.Subsets) == 0 {
		return "", errors.New("no active endpoints found for DNS Service")
	}

	log.Infof("DNS Service currently has %d active endpoint(s)", len(endpoints.Subsets))

	dnsLabelSelector := mapToString(dnsService.Spec.Selector)

	log.Infoln("Identifying DNS Pods by Service information")

	podList, err := clientset.CoreV1().Pods(dnsService.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: dnsLabelSelector,
	})

	if err != nil {
		log.Error("Error getting DNS Pod", err)
		return "", err
	}

	if len(podList.Items) == 0 {
		log.Error("No active endpoints found for DNS Service")
		return "", err
	}

	return dnsLabelSelector, nil
}

func checkDNSPods(clientset *kubernetes.Clientset, clusterDNSNamespace, dnsLabelSelector string) error {

	log.Info("Getting DNS Pods")
	pods, err := clientset.CoreV1().Pods(clusterDNSNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: dnsLabelSelector,
	})

	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		log.Infof("DNS Pod: %s", pod.Name)
		log.Infof("DNS Pod IP: %s", pod.Status.PodIP)
		log.Infof("DNS Pod Node: %s", pod.Spec.NodeName)
		log.Infof("DNS Pod Phase: %s", pod.Status.Phase)

		if pod.Status.Phase != "Running" {
			return errors.New("DNS Pod not in Running state")
		}

		if pod.Status.ContainerStatuses[0].RestartCount > 0 {
			log.Warnf("DNS Pod Restart Count: %d", pod.Status.ContainerStatuses[0].RestartCount)
		}
	}

	return nil
}

func checkInternalDNSResolution(clientset *kubernetes.Clientset, clusterDNSNamespace, dnsLabelSelector string) error {

	dnsPods, err := clientset.CoreV1().Pods(clusterDNSNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: dnsLabelSelector,
	})

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, node := range nodes.Items {
		for _, dnsPod := range dnsPods.Items {

			log.Infof("From: %s To %s Residing on %s", node.Name, dnsPod.Name, dnsPod.Spec.NodeName)

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
