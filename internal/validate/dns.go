package validate

import (
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
	"golang.org/x/net/context"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func RunDNSTests(clients common.Clients, runConfig common.RunConfig, clusterDNSConfig common.ClusterDNSConfig) error {

	// Check the existence of the DNS config map and extract the service endpoint IP and domain

	log.Info("Checking active DNS Endpoint")
	err := checkDNSEndpoints(clients, clusterDNSConfig)
	if err != nil {
		return err
	}

	// Check the corresponding service endpoints for running DNS pods
	log.Info("Checking associated endpoint Pods")
	err = checkDNSPods(clients, clusterDNSConfig)
	if err != nil {
		return err
	}

	// Check the corresponding Pods are correctly resolving internal DNS requests
	log.Info("Checking internal DNS resolution, please wait")
	err = checkInternalDNSResolution(clients, clusterDNSConfig, runConfig)
	if err != nil {
		return err
	}

	// Check the corresponding Pods are correctly resolving external DNS requests
	log.Info("Checking external DNS resolution, please wait")
	err = checkExternalDNSResolution(clients, clusterDNSConfig, runConfig)
	if err != nil {
		return err
	}

	return err
}

func checkDNSEndpoints(clients common.Clients, clusterDNSConfig common.ClusterDNSConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), clients.Timeout*time.Second)
	defer cancel()

	endpoints, err := clients.KubeClient.CoreV1().Endpoints(clusterDNSConfig.DNSServiceNamespace).Get(ctx, clusterDNSConfig.DNSServiceServiceName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if len(endpoints.Subsets) == 0 {
		return errors.New("no active endpoints found for DNS Service")
	}

	podList, err := clients.KubeClient.CoreV1().Pods(clusterDNSConfig.DNSServiceNamespace).List(ctx, metav1.ListOptions{
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

func checkDNSPods(clients common.Clients, clusterDNSConfig common.ClusterDNSConfig) error {

	ctx, cancel := context.WithTimeout(context.Background(), clients.Timeout*time.Second)
	defer cancel()

	log.Debugf("Getting DNS Pods")
	pods, err := clients.KubeClient.CoreV1().Pods(clusterDNSConfig.DNSServiceNamespace).List(ctx, metav1.ListOptions{
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

func checkInternalDNSResolution(clients common.Clients, clusterDNSConfig common.ClusterDNSConfig, runConfig common.RunConfig) error {

	ctx, cancel := context.WithTimeout(context.Background(), clients.Timeout*time.Second)
	defer cancel()

	var dnsConfig common.DNSConfig

	t := table.NewWriter()
	t.SetStyle(table.StyleColoredDark)
	t.SetOutputMirror(os.Stdout)
	t.SetTitle("DNS Networking Tests (Internal)")
	t.Style().Title.Align = text.AlignCenter
	t.AppendHeader(table.Row{"From (Node)", "From (Pod)", "To (Node)", "To (Pod)", "Intra/Inter", "Status", "Domain"})

	data, err := os.ReadFile(runConfig.ConfigFile)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(data, &dnsConfig)
	if err != nil {
		return err
	}

	dnsPods, err := clients.KubeClient.CoreV1().Pods(clusterDNSConfig.DNSServiceNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: clusterDNSConfig.DNSLabelSelector,
	})
	if err != nil {
		return err
	}

	nodes, err := clients.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)
	errChan := make(chan error, 1)

	for _, node := range nodes.Items {
		for _, dnsPod := range dnsPods.Items {
			for _, internalDNS := range dnsConfig.InternalDNS {

				wg.Add(1)

				go func(node corev1.Node, dnsPod corev1.Pod, internalDNS common.DNSRecord) {
					defer wg.Done()
					sem <- struct{}{}        // acquire semaphore
					defer func() { <-sem }() // release semaphore

					err := createTestDNSPods(node, dnsPod, clients, runConfig.TestNamespace, internalDNS, t)
					if err != nil {
						errChan <- err
					}
					errChan <- nil
				}(node, dnsPod, internalDNS)
			}

		}
	}
	// Wait for all goroutines to finish
	go func() {
		wg.Wait()
		close(errChan)
	}()

	for err := range errChan {
		if err != nil {
			return err
		}
	}

	t.SortBy([]table.SortBy{
		{Name: "From (Node)", Mode: table.Asc},
	})
	t.Render()

	return nil
}

func checkExternalDNSResolution(clients common.Clients, clusterDNSConfig common.ClusterDNSConfig, runConfig common.RunConfig) error {

	ctx, cancel := context.WithTimeout(context.Background(), clients.Timeout*time.Second)
	defer cancel()

	var dnsConfig common.DNSConfig

	t := table.NewWriter()
	t.SetStyle(table.StyleColoredDark)
	t.SetOutputMirror(os.Stdout)
	t.SetTitle("DNS Networking Tests (External)")
	t.Style().Title.Align = text.AlignCenter
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

	dnsPods, err := clients.KubeClient.CoreV1().Pods(clusterDNSConfig.DNSServiceNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: clusterDNSConfig.DNSLabelSelector,
	})
	if err != nil {
		return err
	}

	nodes, err := clients.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
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

					err := createTestDNSPods(node, dnsPod, clients, runConfig.TestNamespace, externalDNS, t)
					if err != nil {
						return err
					}
					return nil
				}(node, dnsPod, externalDNS)

			}
		}
	}

	wg.Wait()
	t.SortBy([]table.SortBy{
		{Name: "From (Node)", Mode: table.Asc},
	})
	t.Render()
	return nil
}

func createTestDNSPods(node corev1.Node, dnsPod corev1.Pod, clients common.Clients, namespace string, dnsRecords common.DNSRecord, t table.Writer) error {

	ctx, cancel := context.WithTimeout(context.Background(), clients.Timeout*time.Second)
	defer cancel()

	pod, err := clients.KubeClient.CoreV1().Pods(namespace).Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dns-test-",
			Namespace:    namespace,
			Labels: map[string]string{
				"kubeplumber": "true",
			},
		},
		Spec: corev1.PodSpec{
			NodeName:      node.Name,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:  "dns-test",
					Image: "busybox",
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

	if pod.Spec.NodeName == dnsPod.Spec.NodeName {
		intraOrInter = "intra"
	} else {
		intraOrInter = "inter"
	}

	for {

		time.Sleep(time.Second)

		pod, err = clients.KubeClient.CoreV1().Pods(namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if pod.Status.ContainerStatuses != nil {
			for _, containerStatus := range pod.Status.ContainerStatuses {
				if containerStatus.State.Waiting != nil {
					if containerStatus.State.Waiting.Reason != "ContainerCreating" && containerStatus.State.Waiting.Reason != "PodInitializing" {
						return errors.New("Pod " + pod.Name + " not in Running state:" + containerStatus.State.Waiting.Reason)
					}
				}
			}
		}

		// Successful DNS resolution
		if pod.Status.Phase == "Succeeded" {
			t.AppendRow(table.Row{pod.Spec.NodeName, pod.Name, dnsPod.Spec.NodeName, dnsPod.Name, intraOrInter, pod.Status.Phase, dnsRecords.Name})
			err = clients.KubeClient.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
			if err != nil {
				return err
			}
			break
		}

		// unsuccessful DNS resolution
		if pod.Status.Phase == "Failed" {
			t.AppendRow(table.Row{pod.Spec.NodeName, pod.Name, dnsPod.Spec.NodeName, dnsPod.Name, intraOrInter, text.FgRed.Sprint(pod.Status.Phase), dnsRecords.Name})
			err = clients.KubeClient.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
			if err != nil {
				return err
			}
			break
		}

	}

	return nil

}
