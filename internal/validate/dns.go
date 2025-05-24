package validate

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
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

func RunDNSTests(clients common.Clients, runConfig common.RunConfig, clusterDNSConfig common.ClusterDNSConfig) (string, error) {
	if clusterDNSConfig.DNSServiceNamespace == "" || clusterDNSConfig.DNSServiceServiceName == "" {
		return "", errors.New("DNS service information is missing. DNS detection may have failed")
	}

	log.Info("Checking active DNS Endpoint")
	err := checkDNSEndpoints(clients, clusterDNSConfig)
	if err != nil {
		return "", fmt.Errorf("DNS endpoint check failed: %v", err)
	}

	// Check the corresponding service endpoints for running DNS pods
	log.Info("Checking associated endpoint Pods")
	err = checkDNSPods(clients, clusterDNSConfig)
	if err != nil {
		return "", err
	}

	// Check the corresponding Pods are correctly resolving internal DNS requests
	log.Info("Checking internal DNS resolution, please wait")
	err, html := checkInternalDNSResolution(clients, clusterDNSConfig, runConfig)
	if err != nil {
		return "", err
	}

	// Check the corresponding Pods are correctly resolving external DNS requests
	log.Info("Checking external DNS resolution, please wait")
	err = checkExternalDNSResolution(clients, clusterDNSConfig, runConfig)
	if err != nil {
		return "", err
	}

	return html, nil
}

func checkDNSEndpoints(clients common.Clients, clusterDNSConfig common.ClusterDNSConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), clients.Timeout*time.Second)
	defer cancel()

	endpoints, err := clients.KubeClient.CoreV1().Endpoints(clusterDNSConfig.DNSServiceNamespace).Get(ctx, clusterDNSConfig.DNSServiceServiceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get DNS endpoints: %v", err)
	}

	if len(endpoints.Subsets) == 0 {
		return errors.New("no active endpoints found for DNS Service")
	}

	podList, err := clients.KubeClient.CoreV1().Pods(clusterDNSConfig.DNSServiceNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: clusterDNSConfig.DNSLabelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list DNS pods: %v", err)
	}

	if len(podList.Items) == 0 {
		return errors.New("no DNS Pods found with the provided label selector")
	}

	return nil
}

func checkDNSPods(clients common.Clients, clusterDNSConfig common.ClusterDNSConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), clients.Timeout*time.Second)
	defer cancel()

	log.Debug("Getting DNS Pods")
	pods, err := clients.KubeClient.CoreV1().Pods(clusterDNSConfig.DNSServiceNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: clusterDNSConfig.DNSLabelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list DNS pods: %v", err)
	}

	for _, pod := range pods.Items {
		log.Debugf("Checking DNS Pod: %s", pod.Name)
		if pod.Status.Phase != "Running" {
			return fmt.Errorf("DNS Pod %s not in Running state: %s", pod.Name, pod.Status.Phase)
		}

		if pod.Status.ContainerStatuses[0].RestartCount > 0 {
			log.Warnf("DNS Pod %s Restart Count: %d", pod.Name, pod.Status.ContainerStatuses[0].RestartCount)
		}
	}

	return nil
}

func checkInternalDNSResolution(clients common.Clients, clusterDNSConfig common.ClusterDNSConfig, runConfig common.RunConfig) (error, string) {
	ctx, cancel := context.WithTimeout(context.Background(), clients.Timeout*time.Second)
	defer cancel()

	var dnsConfig common.DNSConfig

	t := table.NewWriter()
	t.SetStyle(table.StyleColoredDark)
	t.SetOutputMirror(os.Stdout)
	t.SetTitle("DNS Networking Tests (Internal)")
	t.Style().Title.Align = text.AlignCenter
	t.AppendHeader(table.Row{"From (Node)", "From (Pod)", "To (Node)", "To (Pod)", "Intra/Inter", "Status", "Domain", "Attempt"})

	data, err := os.ReadFile(runConfig.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err), ""
	}

	err = yaml.Unmarshal(data, &dnsConfig)
	if err != nil {
		return fmt.Errorf("failed to parse config file: %v", err), ""
	}

	// Replace cluster.local with the detected cluster domain in internal DNS records
	for i, record := range dnsConfig.InternalDNS {
		if strings.HasSuffix(record.Name, "cluster.local") {
			dnsConfig.InternalDNS[i].Name = strings.TrimSuffix(record.Name, "cluster.local") + clusterDNSConfig.DNSServiceDomain
			log.Debugf("Adjusted internal DNS record from %s to %s", record.Name, dnsConfig.InternalDNS[i].Name)
		}
	}

	dnsPods, err := clients.KubeClient.CoreV1().Pods(clusterDNSConfig.DNSServiceNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: clusterDNSConfig.DNSLabelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list DNS pods: %v", err), ""
	}

	nodes, err := clients.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %v", err), ""
	}

	// Use a smaller concurrency limit to avoid rate limiting
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	errChan := make(chan error, len(nodes.Items)*len(dnsPods.Items)*len(dnsConfig.InternalDNS))

	for _, node := range nodes.Items {
		for _, dnsPod := range dnsPods.Items {
			for _, internalDNS := range dnsConfig.InternalDNS {
				wg.Add(1)
				go func(node corev1.Node, dnsPod corev1.Pod, internalDNS common.DNSRecord) {
					defer wg.Done()
					sem <- struct{}{}        // acquire semaphore
					defer func() { <-sem }() // release semaphore

					select {
					case <-ctx.Done():
						errChan <- fmt.Errorf("timeout while testing DNS resolution")
						return
					default:
						if err := createTestDNSPods(node, dnsPod, clients, runConfig.TestNamespace, internalDNS, t); err != nil {
							log.Debugf("Internal DNS test failed for node %s: %v", node.Name, err)
							errChan <- err
						}
					}
				}(node, dnsPod, internalDNS)
			}
		}
	}

	// Wait for all goroutines to finish or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("internal DNS tests timed out after %d seconds", clients.Timeout), ""
	case <-done:
		close(errChan)
		// Check for any errors
		for err := range errChan {
			if err != nil {
				return fmt.Errorf("internal DNS test failed: %v", err), ""
			}
		}
	}

	t.SortBy([]table.SortBy{
		{Name: "From (Node)", Mode: table.Asc},
	})
	t.Render()

	t.Style().HTML.CSSClass = "table"

	return nil, t.RenderHTML()
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
	t.AppendHeader(table.Row{"From (Node)", "From (Pod)", "To (Node)", "To (Pod)", "Intra/Inter", "Status", "Domain", "Attempt"})

	data, err := os.ReadFile(runConfig.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err)
	}

	err = yaml.Unmarshal(data, &dnsConfig)
	if err != nil {
		return fmt.Errorf("failed to parse config file: %v", err)
	}

	dnsPods, err := clients.KubeClient.CoreV1().Pods(clusterDNSConfig.DNSServiceNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: clusterDNSConfig.DNSLabelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list DNS pods: %v", err)
	}

	nodes, err := clients.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %v", err)
	}

	// Use a smaller concurrency limit to avoid rate limiting
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	errChan := make(chan error, len(nodes.Items)*len(dnsPods.Items)*len(dnsConfig.ExternalDNS))

	for _, node := range nodes.Items {
		for _, dnsPod := range dnsPods.Items {
			for _, externalDNS := range dnsConfig.ExternalDNS {
				wg.Add(1)
				go func(node corev1.Node, dnsPod corev1.Pod, externalDNS common.DNSRecord) {
					defer wg.Done()
					sem <- struct{}{}        // acquire semaphore
					defer func() { <-sem }() // release semaphore

					select {
					case <-ctx.Done():
						errChan <- fmt.Errorf("timeout while testing DNS resolution")
						return
					default:
						if err := createTestDNSPods(node, dnsPod, clients, runConfig.TestNamespace, externalDNS, t); err != nil {
							log.Debugf("External DNS test failed for node %s: %v", node.Name, err)
							errChan <- err
						}
					}
				}(node, dnsPod, externalDNS)
			}
		}
	}

	// Wait for all goroutines to finish or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("external DNS tests timed out after %d seconds", clients.Timeout)
	case <-done:
		close(errChan)
		// Check for any errors
		for err := range errChan {
			if err != nil {
				return fmt.Errorf("external DNS test failed: %v", err)
			}
		}
	}

	t.SortBy([]table.SortBy{
		{Name: "From (Node)", Mode: table.Asc},
	})
	t.Render()
	return nil
}

func createTestDNSPods(node corev1.Node, dnsPod corev1.Pod, clients common.Clients, namespace string, dnsRecords common.DNSRecord, t table.Writer) error {
	// Create context with shorter timeout for individual operations
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log.Debugf("Creating DNS test pod on node %s to resolve %s", node.Name, dnsRecords.Name)
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
		return fmt.Errorf("failed to create test pod: %v", err)
	}

	var intraOrInter string
	if pod.Spec.NodeName == dnsPod.Spec.NodeName {
		intraOrInter = "intra"
	} else {
		intraOrInter = "inter"
	}

	maxRetries := 3
	retryCount := 0
	checkInterval := time.Second * 2

	for retryCount < maxRetries {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for test pod completion")
		case <-time.After(checkInterval):
			pod, err = clients.KubeClient.CoreV1().Pods(namespace).Get(ctx, pod.Name, metav1.GetOptions{})
			if err != nil {
				log.Debugf("Error getting test pod status: %v, retry %d/%d", err, retryCount+1, maxRetries)
				retryCount++
				continue
			}

			log.Debugf("Pod %s status: %s", pod.Name, pod.Status.Phase)

			if pod.Status.ContainerStatuses != nil {
				for _, containerStatus := range pod.Status.ContainerStatuses {
					if containerStatus.State.Waiting != nil {
						reason := containerStatus.State.Waiting.Reason
						log.Debugf("Pod %s container waiting: %s", pod.Name, reason)
						if reason != "ContainerCreating" && reason != "PodInitializing" {
							// Clean up the pod
							_ = clients.KubeClient.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
							return fmt.Errorf("pod %s not in expected state: %s", pod.Name, reason)
						}
					}
					if containerStatus.State.Terminated != nil {
						log.Debugf("Pod %s container terminated with exit code: %d", pod.Name, containerStatus.State.Terminated.ExitCode)
					}
				}
			}

			// Successful DNS resolution
			if pod.Status.Phase == "Succeeded" {
				log.Debugf("DNS resolution succeeded for %s on node %s", dnsRecords.Name, node.Name)
				t.AppendRow(table.Row{pod.Spec.NodeName, pod.Name, dnsPod.Spec.NodeName, dnsPod.Name, intraOrInter, pod.Status.Phase, dnsRecords.Name, retryCount + 1})
				_ = clients.KubeClient.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
				return nil
			}

			// Unsuccessful DNS resolution
			if pod.Status.Phase == "Failed" {
				log.Debugf("DNS resolution failed for %s on node %s", dnsRecords.Name, node.Name)
				t.AppendRow(table.Row{pod.Spec.NodeName, pod.Name, dnsPod.Spec.NodeName, dnsPod.Name, intraOrInter, text.FgRed.Sprint(pod.Status.Phase), dnsRecords.Name, retryCount + 1})
				_ = clients.KubeClient.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})

				if retryCount < maxRetries-1 {
					log.Debugf("Retrying DNS test for %s on node %s (%d/%d)", dnsRecords.Name, node.Name, retryCount+1, maxRetries)
					retryCount++
					// Create a new pod for retry
					pod, err = clients.KubeClient.CoreV1().Pods(namespace).Create(ctx, &corev1.Pod{
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
						return fmt.Errorf("failed to create retry test pod: %v", err)
					}
					continue
				}
			}
		}
	}
	return nil
}
