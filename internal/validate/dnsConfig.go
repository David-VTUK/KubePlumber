package validate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var clusterDNSDomain string
var clusterDNSEndpoint string

func RunDNSTests(clientset *kubernetes.Clientset) error {

	log.Info("Getting cluster-dns ConfigMap")
	config, err := clientset.CoreV1().ConfigMaps("kube-system").Get(context.TODO(), "cluster-dns", metav1.GetOptions{})
	if err != nil {
		log.Error("Error getting CoreDNS ConfigMap", err)
		return err
	}

	//log.Info("CoreDNS ConfigMap", config)

	clusterDNSDomain = config.Data["clusterDomain"]
	clusterDNSEndpoint = config.Data["clusterDNS"]

	log.Infof("Cluster DNS Domain: %s", clusterDNSDomain)
	log.Infof("Cluster DNS Endpoint: %s", clusterDNSEndpoint)

	serviceList, err := clientset.CoreV1().Services("").List(context.TODO(), metav1.ListOptions{
		FieldSelector: "spec.clusterIP=" + clusterDNSEndpoint,
	})
	if err != nil {
		log.Error("Error getting k8s DNS Service", err)
	}

	if len(serviceList.Items) == 0 {
		log.Error("No DNS Service found")
		return err
	}

	if len(serviceList.Items) > 1 {
		log.Error("Multiple DNS Services found")
		return err
	}

	dnsService := serviceList.Items[0]

	log.Infof("DNS Service: %s", dnsService.Name)

	if dnsService.Spec.Selector == nil {
		log.Error("DNS Service has no selector")
		return err
	}

	if len(dnsService.Spec.Selector) > 1 {
		log.Error("Multipe selectors found for DNS Service")
		return err
	}

	dnsLabelSelector := mapToString(dnsService.Spec.Selector)

	log.Infoln("Identifying DNS Pods by Service information")

	podList, err := clientset.CoreV1().Pods(dnsService.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: dnsLabelSelector,
	})

	if err != nil {
		log.Error("Error getting DNS Pod", err)
		return err
	}

	if len(podList.Items) == 0 {
		log.Error("No active endpoints found for DNS Service")
		return err
	}

	checkDNSPods(podList)
	checkInternalDNSResolution(clientset)
	return nil

}

func checkDNSPods(p *v1.PodList) error {

	for _, pod := range p.Items {
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

func checkInternalDNSResolution(clientset *kubernetes.Clientset) error {

	log.Info("Creating DNS Test Jobs")

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dns-test",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:    "dns-test",
					Image:   "busybox",
					Command: []string{"sh", "-c", "sleep infinity"},
				},
			},
			RestartPolicy: v1.RestartPolicyNever,
		},
	}

	log.Info("Creating Pod")

	pod, err := clientset.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})

	if err != nil {
		return err
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
