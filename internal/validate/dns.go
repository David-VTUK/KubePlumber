package validate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/David-VTUK/KubePlumber/common"
	"github.com/jedib0t/go-pretty/v6/table"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
)

const (
	testDNSNamespace = "default"
)

func RunDNSTests(clients common.Clients, config common.Config) error {

	var err error

	// Check the existence of the DNS config map and extract the service endpoint IP and domain
	dnsLabelSelector, err := checkDNSConfigMap(clients, config.DNSConfigNamespace, config.DNSConfigMapName, config.IsOpenShift)
	if err != nil {
		return err
	}

	log.Info("DNS Label Selector: ", dnsLabelSelector)

	// Check the corresponding service endpoints for running DNS pods
	err = checkDNSPods(clients.KubeClient, config.DNSConfigNamespace, dnsLabelSelector)
	if err != nil {
		return err
	}

	// Check the corresponding Pods are correctly resolving internal DNS requests
	err = checkInternalDNSResolution(clients.KubeClient, config.DNSConfigNamespace, dnsLabelSelector, config.ConfigFile)
	if err != nil {
		return err
	}

	// Check the corresponding Pods are correctly resolving external DNS requests
	err = checkExternalDNSResolution(clients.KubeClient, config.DNSConfigNamespace, dnsLabelSelector, config.ConfigFile)
	if err != nil {
		return err
	}

	return err
}

func checkDNSConfigMap(clients common.Clients, clusterDNSNamespace, clusterDNSConfigMapName string, isOpenShift bool) (string, error) {

	var clusterDNSDomain, clusterDNSEndpoint string
	var dnsService corev1.Service

	log.Info("Getting Cluster DNS Configuration")

	if isOpenShift {

		log.Info("Cluster is OpenShift")

		config, err := clients.DynamicClient.Resource(common.OpenShiftDNSGVR).Get(context.TODO(), clusterDNSConfigMapName, metav1.GetOptions{})
		if err != nil {
			return "", err
		}

		// Extract the status.clusterDomain field
		status, found, err := unstructured.NestedMap(config.Object, "status")
		if !found || err != nil {
			return "", fmt.Errorf("status not found in dnses object")
		}

		clusterDNSDomain, found, err = unstructured.NestedString(status, "clusterDomain")
		if !found || err != nil {
			return "", fmt.Errorf("clusterDomain not found in status")
		}

		clusterDNSEndpoint, found, err = unstructured.NestedString(status, "clusterIP")
		if !found || err != nil {
			return "", fmt.Errorf("clusterIP not found in status")
		}

		log.Info("Cluster DNS Domain: ", clusterDNSDomain)
		log.Info("Cluster DNS Endpoint: ", clusterDNSEndpoint)

	} else {

		config, err := clients.KubeClient.CoreV1().ConfigMaps(clusterDNSNamespace).Get(context.TODO(), clusterDNSConfigMapName, metav1.GetOptions{})
		if err != nil {
			return "", err
		}

		clusterDNSDomain = config.Data["clusterDomain"]
		clusterDNSEndpoint = config.Data["clusterDNS"]

		log.Info("Cluster DNS Domain: ", clusterDNSDomain)
		log.Info("Cluster DNS Endpoint: ", clusterDNSEndpoint)
	}

	serviceList, err := clients.KubeClient.CoreV1().Services(clusterDNSNamespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", errors.New("unable to list services")
	}

	for _, service := range serviceList.Items {
		if service.Spec.ClusterIP == clusterDNSEndpoint {
			log.Info("DNS Service found: ", service.Name)
			dnsService = service

		}
	}

	endpoints, err := clients.KubeClient.CoreV1().Endpoints(dnsService.Namespace).Get(context.TODO(), dnsService.Name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	if len(endpoints.Subsets) == 0 {
		return "", errors.New("no active endpoints found for DNS Service")
	}

	log.Debugf("DNS Service currently has %d active endpoint(s)", len(endpoints.Subsets))

	dnsLabelSelector := mapToString(dnsService.Spec.Selector)

	log.Debugf("Identifying DNS Pods by Service information")

	podList, err := clients.KubeClient.CoreV1().Pods(dnsService.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: dnsLabelSelector,
	})

	if err != nil {
		return "", err
	}

	if len(podList.Items) == 0 {
		return "", errors.New("no DNS Pods found with the provided label selector")
	}

	return dnsLabelSelector, nil
}

func checkDNSPods(clientset *kubernetes.Clientset, clusterDNSNamespace, dnsLabelSelector string) error {

	log.Debugf("Getting DNS Pods")
	pods, err := clientset.CoreV1().Pods(clusterDNSNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: dnsLabelSelector,
	})

	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		log.Debugf("Checking DNS Pod: %s", pod.Name)
		if pod.Status.Phase != "Running" {
			return errors.New("DNS Pod not in Running state")
		}

		if pod.Status.ContainerStatuses[0].RestartCount > 0 {
			log.Warnf("DNS Pod Restart Count: %d", pod.Status.ContainerStatuses[0].RestartCount)
		}
	}

	return nil
}

func checkInternalDNSResolution(clientset *kubernetes.Clientset, clusterDNSNamespace, dnsLabelSelector, configFile string) error {

	var dnsConfig common.DNSConfig

	t := table.NewWriter()
	t.SetStyle(table.StyleColoredDark)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"From (Node)", "From (Pod)", "To (Node)", "To (Pod)", "Intra/Inter", "Status", "Domain"})

	data, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(data, &dnsConfig)
	if err != nil {
		fmt.Println(err)
		return err
	}

	dnsPods, err := clientset.CoreV1().Pods(clusterDNSNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: dnsLabelSelector,
	})
	if err != nil {
		return err
	}

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for _, node := range nodes.Items {
		for _, dnsPod := range dnsPods.Items {
			for _, internalDNS := range dnsConfig.InternalDNS {

				wg.Add(1)

				go func(node corev1.Node, dnsPod corev1.Pod, internalDNS common.DNSRecord) error {
					defer wg.Done()
					sem <- struct{}{}        // acquire semaphore
					defer func() { <-sem }() // release semaphore

					err := createTestDNSPods(node, dnsPod, clientset, internalDNS, t)
					if err != nil {
						return err
					}
					return nil
				}(node, dnsPod, internalDNS)
			}

		}
	}

	wg.Wait() // wait for all goroutines to finish

	t.Render()

	return nil
}

func checkExternalDNSResolution(clientset *kubernetes.Clientset, clusterDNSNamespace, dnsLabelSelector, configFile string) error {

	var dnsConfig common.DNSConfig

	t := table.NewWriter()
	t.SetStyle(table.StyleColoredDark)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"From (Node)", "From (Pod)", "To (Node)", "To (Pod)", "Intra/Inter", "Status", "Domain"})

	data, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(data, &dnsConfig)
	if err != nil {
		fmt.Println(err)
		return err
	}

	dnsPods, err := clientset.CoreV1().Pods(clusterDNSNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: dnsLabelSelector,
	})
	if err != nil {
		return err
	}

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for _, node := range nodes.Items {
		for _, dnsPod := range dnsPods.Items {
			for _, externalDNS := range dnsConfig.ExternalDNS {

				wg.Add(1)

				go func(node corev1.Node, dnsPod corev1.Pod, externalDNS common.DNSRecord) error {
					defer wg.Done()
					sem <- struct{}{}        // acquire semaphore
					defer func() { <-sem }() // release semaphore

					err := createTestDNSPods(node, dnsPod, clientset, externalDNS, t)
					if err != nil {
						return err
					}
					return nil
				}(node, dnsPod, externalDNS)

				if err != nil {
					return err
				}

			}
		}
	}

	wg.Wait()

	t.Render()
	return nil
}

func createTestDNSPods(node corev1.Node, dnsPod corev1.Pod, clientset *kubernetes.Clientset, dnsRecords common.DNSRecord, t table.Writer) error {

	pod, err := clientset.CoreV1().Pods(testDNSNamespace).Create(context.TODO(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dns-test-",
			Namespace:    testDNSNamespace,
		},
		Spec: corev1.PodSpec{
			NodeName:      node.Name,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:  "dns-test",
					Image: "quay.io/quay/busybox",
					Command: []string{
						"nslookup",
						dnsRecords.Name,
						fmt.Sprintf("%s:%s", dnsPod.Status.PodIP, strconv.Itoa(int(dnsPod.Spec.Containers[0].Ports[0].ContainerPort))),
					},
				},
			},
		},
	}, metav1.CreateOptions{})

	if err != nil {
		return err
	}
	var intraOrInter string

	for {
		time.Sleep(500 * time.Millisecond)
		pod, err = clientset.CoreV1().Pods(testDNSNamespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})

		if pod.Spec.NodeName == dnsPod.Spec.NodeName {
			intraOrInter = "intra"
		} else {
			intraOrInter = "inter"
		}

		if err != nil {
			return err
		}

		if pod.Status.Phase != "Succeeded" && pod.Status.Phase != "Failed" {
			log.Infof("Pod %s is in %s state. Waiting for it to complete DNS resolution", pod.Name, pod.Status.Phase)
			continue
		}

		if pod.Status.Phase == "Succeeded" {
			log.Info("Pod has succeeded")
			t.AppendRow(table.Row{pod.Spec.NodeName, pod.Name, dnsPod.Spec.NodeName, dnsPod.Name, intraOrInter, pod.Status.Phase, dnsRecords.Name})
			err = clientset.CoreV1().Pods(testDNSNamespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			if err != nil {
				return err
			}
			log.Info("Deleted Pod")
			break
		}

		if pod.Status.Phase == "Failed" {
			log.Info("Pod has failed")
			t.AppendRow(table.Row{pod.Spec.NodeName, pod.Name, dnsPod.Spec.NodeName, dnsPod.Name, intraOrInter, pod.Status.Phase, dnsRecords.Name}, table.RowConfig{AutoMerge: true})
			err = clientset.CoreV1().Pods(testDNSNamespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			if err != nil {
				return err
			}
			log.Info("Deleted Pod")
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
