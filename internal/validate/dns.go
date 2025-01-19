package validate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	testDNSNamespace = "default"
)

func RunDNSTests(clientset *kubernetes.Clientset, clusterDNSConfigMapName, clusterDNSNamespace, configFile *string) error {

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
	err = checkInternalDNSResolution(clientset, *clusterDNSNamespace, dnsLabelSelector, *configFile)
	if err != nil {
		return err
	}

	// Check the corresponding Pods are correctly resolving external DNS requests
	err = checkExternalDNSResolution(clientset, *clusterDNSNamespace, dnsLabelSelector, *configFile)
	if err != nil {
		return err
	}

	return err
}

func checkDNSConfigMap(clientset *kubernetes.Clientset, clusterDNSNamespace, clusterDNSConfigMapName string) (string, error) {

	log.Info("Getting cluster-dns ConfigMap")

	config, err := clientset.CoreV1().ConfigMaps(clusterDNSNamespace).Get(context.TODO(), clusterDNSConfigMapName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	clusterDNSDomain := config.Data["clusterDomain"]
	clusterDNSEndpoint := config.Data["clusterDNS"]

	log.Debugf("Cluster DNS Domain: %s", clusterDNSDomain)
	log.Debugf("Cluster DNS Endpoint: %s", clusterDNSEndpoint)

	serviceList, err := clientset.CoreV1().Services(clusterDNSNamespace).List(context.TODO(), metav1.ListOptions{
		FieldSelector: "spec.clusterIP=" + clusterDNSEndpoint,
	})

	if err != nil {
		return "", err
	}

	if len(serviceList.Items) == 0 {
		return "", errors.New("no DNS Service found with the provided clusterIP")
	}

	if len(serviceList.Items) > 1 {
		return "", errors.New("multiple DNS Services found with the provided clusterIP")
	}

	dnsService := serviceList.Items[0]

	endpoints, err := clientset.CoreV1().Endpoints(dnsService.Namespace).Get(context.TODO(), dnsService.Name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	if len(endpoints.Subsets) == 0 {
		return "", errors.New("no active endpoints found for DNS Service")
	}

	log.Debugf("DNS Service currently has %d active endpoint(s)", len(endpoints.Subsets))

	dnsLabelSelector := mapToString(dnsService.Spec.Selector)

	log.Debugf("Identifying DNS Pods by Service information")

	podList, err := clientset.CoreV1().Pods(dnsService.Namespace).List(context.TODO(), metav1.ListOptions{
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

	var dnsConfig DNSConfig
	var intraOrInter string

	t := table.NewWriter()
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

	fmt.Println(dnsConfig.InternalDNS)

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

	for _, node := range nodes.Items {
		for _, dnsPod := range dnsPods.Items {
			for _, internalDNS := range dnsConfig.InternalDNS {
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
								Image: "busybox",
								Command: []string{
									"nslookup",
									internalDNS.Name,
									dnsPod.Status.PodIP,
								},
							},
						},
					},
				}, metav1.CreateOptions{})

				if err != nil {
					return err
				}

				for {
					time.Sleep(1 * time.Second)
					pod, err = clientset.CoreV1().Pods(testDNSNamespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})

					if pod.Spec.NodeName == dnsPod.Spec.NodeName {
						intraOrInter = "intra"
					} else {
						intraOrInter = "inter"
					}

					if err != nil {
						return err
					}

					if pod.Status.Phase == "Pending" {
						log.Info("Pod is still pending")
						continue
					}

					if pod.Status.Phase == "Succeeded" {
						log.Info("Pod has succeeded")
						err = clientset.CoreV1().Pods(testDNSNamespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
						if err != nil {
							return err
						}
						log.Info("Deleted Pod")

						t.AppendRow(table.Row{node.Name, pod.Name, dnsPod.Spec.NodeName, dnsPod.Name, intraOrInter, "Success", internalDNS.Name})
						break
					}

					if pod.Status.Phase == "Failed" {
						log.Info("Pod has failed")
						err = clientset.CoreV1().Pods(testDNSNamespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
						if err != nil {
							return err
						}
						log.Info("Deleted Pod")
						t.AppendRow(table.Row{node.Name, pod.Name, dnsPod.Spec.NodeName, dnsPod.Name, intraOrInter, "Failed", internalDNS.Name})
						break
					}
				}

			}
		}
	}

	t.SetStyle(table.StyleColoredDark)

	//render table
	t.Render()
	return nil
}

func checkExternalDNSResolution(clientset *kubernetes.Clientset, clusterDNSNamespace, dnsLabelSelector, configFile string) error {

	var dnsConfig DNSConfig

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"From (Node)", "From (Pod)", "To (Node)", "To (Pod)", "Status", "Domain"})

	data, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(data, &dnsConfig)
	if err != nil {
		fmt.Println(err)
		return err
	}

	fmt.Println(dnsConfig.InternalDNS)

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

	for _, node := range nodes.Items {
		for _, dnsPod := range dnsPods.Items {
			for _, externalDNS := range dnsConfig.ExternalDNS {
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
								Image: "busybox",
								Command: []string{
									"nslookup",
									externalDNS.Name,
									dnsPod.Status.PodIP,
								},
							},
						},
					},
				}, metav1.CreateOptions{})

				if err != nil {
					return err
				}

				for {
					time.Sleep(1 * time.Second)
					pod, err = clientset.CoreV1().Pods(testDNSNamespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})

					if err != nil {
						return err
					}

					if pod.Status.Phase == "Pending" {
						log.Info("Pod is still pending")
						continue
					}

					if pod.Status.Phase == "Succeeded" {
						log.Info("Pod has succeeded")
						err = clientset.CoreV1().Pods(testDNSNamespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
						if err != nil {
							return err
						}
						log.Info("Deleted Pod")

						t.AppendRow(table.Row{node.Name, pod.Name, dnsPod.Spec.NodeName, dnsPod.Name, "Success", externalDNS.Name})
						break
					}

					if pod.Status.Phase == "Failed" {
						log.Info("Pod has failed")
						err = clientset.CoreV1().Pods(testDNSNamespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
						if err != nil {
							return err
						}
						log.Info("Deleted Pod")
						t.AppendRow(table.Row{node.Name, pod.Name, dnsPod.Spec.NodeName, dnsPod.Name, "Failed", externalDNS.Name})
						break
					}
				}

			}
		}
	}

	t.SetStyle(table.StyleColoredDark)

	//render table
	t.Render()
	return nil
}

func mapToString(m map[string]string) string {
	var sb strings.Builder
	for key, value := range m {
		sb.WriteString(fmt.Sprintf("%s=%s ", key, value))
	}
	return strings.TrimSpace(sb.String())
}
