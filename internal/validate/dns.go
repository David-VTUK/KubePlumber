package validate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/David-VTUK/KubePlumber/common"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	testDNSNamespace = "default"
)

func RunDNSTests(clients common.Clients, runConfig common.RunConfig, clusterDNSConfig common.ClusterDNSConfig) error {

	// Check the existence of the DNS config map and extract the service endpoint IP and domain
	err := checkDNSEndpoints(clients, clusterDNSConfig)
	if err != nil {
		return err
	}

	// Check the corresponding service endpoints for running DNS pods
	err = checkDNSPods(clients.KubeClient, clusterDNSConfig)
	if err != nil {
		return err
	}

	// Check the corresponding Pods are correctly resolving internal DNS requests
	err = checkInternalDNSResolution(clients.KubeClient, clusterDNSConfig, runConfig)
	if err != nil {
		return err
	}

	// Check the corresponding Pods are correctly resolving external DNS requests
	err = checkExternalDNSResolution(clients.KubeClient, clusterDNSConfig, runConfig)
	if err != nil {
		return err
	}

	return err
}

func checkDNSEndpoints(clients common.Clients, clusterDNSConfig common.ClusterDNSConfig) error {

	endpoints, err := clients.KubeClient.CoreV1().Endpoints(clusterDNSConfig.DNSServiceNamespace).Get(context.TODO(), clusterDNSConfig.DNSServiceServiceName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if len(endpoints.Subsets) == 0 {
		return errors.New("no active endpoints found for DNS Service")
	}

	podList, err := clients.KubeClient.CoreV1().Pods(clusterDNSConfig.DNSServiceNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: clusterDNSConfig.DNSLabelSelector,
	})

	if err != nil {
		return err
	}

	if len(podList.Items) == 0 {
		return errors.New("no DNS Pods found with the provided label selector")
	}

	return nil
}

func checkDNSPods(clientset *kubernetes.Clientset, clusterDNSConfig common.ClusterDNSConfig) error {

	log.Debugf("Getting DNS Pods")
	pods, err := clientset.CoreV1().Pods(clusterDNSConfig.DNSServiceNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: clusterDNSConfig.DNSLabelSelector,
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

func checkInternalDNSResolution(clientset *kubernetes.Clientset, clusterDNSConfig common.ClusterDNSConfig, runConfig common.RunConfig) error {

	var dnsConfig common.DNSConfig

	t := table.NewWriter()
	t.SetStyle(table.StyleColoredDark)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"From (Node)", "From (Pod)", "To (Node)", "To (Pod)", "Intra/Inter", "Status", "Domain"})

	data, err := os.ReadFile(runConfig.ConfigFile)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(data, &dnsConfig)
	if err != nil {
		fmt.Println(err)
		return err
	}

	dnsPods, err := clientset.CoreV1().Pods(clusterDNSConfig.DNSServiceNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: clusterDNSConfig.DNSLabelSelector,
	})
	if err != nil {
		return err
	}

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

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

func checkExternalDNSResolution(clientset *kubernetes.Clientset, clusterDNSConfig common.ClusterDNSConfig, runConfig common.RunConfig) error {

	var dnsConfig common.DNSConfig

	t := table.NewWriter()
	t.SetStyle(table.StyleColoredDark)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"From (Node)", "From (Pod)", "To (Node)", "To (Pod)", "Intra/Inter", "Status", "Domain"})

	data, err := os.ReadFile(runConfig.ConfigFile)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(data, &dnsConfig)
	if err != nil {
		fmt.Println(err)
		return err
	}

	dnsPods, err := clientset.CoreV1().Pods(clusterDNSConfig.DNSServiceNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: clusterDNSConfig.DNSLabelSelector,
	})
	if err != nil {
		return err
	}

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

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
			t.AppendRow(table.Row{pod.Spec.NodeName, pod.Name, dnsPod.Spec.NodeName, dnsPod.Name, intraOrInter, text.FgRed.Sprint(pod.Status.Phase), dnsRecords.Name})
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
