package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/David-VTUK/KubePlumber/common"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func RunOverlayNetworkSpeedTests(clients common.Clients, restConfig *rest.Config, clusterDomain string, runConfig common.RunConfig) error {

	err := CheckOverlayNetworkSpeed(clients, restConfig, clusterDomain, runConfig.TestNamespace)
	if err != nil {
		return fmt.Errorf("error checking overlay network speed: %s", err)
	}

	return nil

}

func CheckOverlayNetworkSpeed(clients common.Clients, restConfig *rest.Config, clusterDomain string, namespace string) error {

	testResults := common.TestResults{}
	testResults.Title = "Overlay Network Tests - Bandwidth"

	log.Info("Checking overlay network speed")

	err := createNicTestPods(clients, namespace, testResults)
	if err != nil {
		return fmt.Errorf("error creating iperf pods: %s", err)
	}

	return nil
}

func createNicTestPods(clients common.Clients, namespace string, results common.TestResults) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	var serverPods []*corev1.Pod

	defer cancel()

	// create iperf server
	nodes, err := clients.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error getting nodes: %s", err)
	}

	// create iperf servers
	for _, node := range nodes.Items {
		pod, err := clients.KubeClient.CoreV1().Pods(namespace).Create(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("kubeplumber-iperf-server-%s", node.Name),
				Namespace: namespace,
				Labels: map[string]string{
					"kubeplumber": "true",
				},
			},
			Spec: corev1.PodSpec{
				NodeName: node.Name,
				Containers: []corev1.Container{
					{
						Name:  "iperf-server",
						Image: "networkstatic/iperf3",
						Args:  []string{"iperf3", "-s"},
					},
				},
			},
		}, metav1.CreateOptions{})

		if err != nil {
			return fmt.Errorf("error creating iperf server pod: %s", err)
		}

		for {
			time.Sleep(time.Second)
			pod, err = clients.KubeClient.CoreV1().Pods(namespace).Get(ctx, pod.Name, metav1.GetOptions{})

			if err != nil {
				return fmt.Errorf("error getting iperf server pod: %s", err)
			}

			if pod.Status.Phase == corev1.PodRunning {
				serverPods = append(serverPods, pod)
				break
			}
		}

	}

	// create iperf clients as Kubernetes Jobs
	for _, node := range nodes.Items {
		for _, serverPod := range serverPods {
			if node.Name == serverPod.Spec.NodeName {
				continue
			}

			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: fmt.Sprintf("kubeplumber-iperf-client-%s-", node.Name),
					Namespace:    namespace,
					Labels: map[string]string{
						"kubeplumber": "true",
					},
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							NodeName: node.Name,
							Containers: []corev1.Container{
								{
									Name:  "iperf-client",
									Image: "networkstatic/iperf3",
									Args:  []string{"iperf3", "-J", "-P 4", "-c", serverPod.Status.PodIP},
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			}

			job, err := clients.KubeClient.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
			if err != nil {
				return err
			}

			jobCompleted := false
			for !jobCompleted {
				time.Sleep(time.Second * 5)

				log.Infof("Getting job %s", job.Name)
				job, err = clients.KubeClient.BatchV1().Jobs(namespace).Get(ctx, job.GetName(), metav1.GetOptions{})

				if err != nil {
					return err
				}

				for _, condition := range job.Status.Conditions {
					if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
						var result common.IperfResult

						podList, err := clients.KubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
							LabelSelector: fmt.Sprintf("job-name=%s", job.GetName()),
						})

						if err != nil {
							return err
						}

						if len(podList.Items) == 0 {
							return fmt.Errorf("no pods found for job %s", job.Name)
						}

						pod := podList.Items[0]

						podLogOpts := corev1.PodLogOptions{}
						req := clients.KubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOpts)
						podLogs, err := req.Stream(ctx)
						if err != nil {
							return fmt.Errorf("error getting logs for pod %s: %s", pod.Name, err)
						}
						defer podLogs.Close()
						buf := new(bytes.Buffer)
						_, err = io.Copy(buf, podLogs)
						if err != nil {
							return err
						}

						err = json.Unmarshal(buf.Bytes(), &result)
						if err != nil {
							return fmt.Errorf("error unmarshalling iperf result: %s", err)
						}

						results.Results = append(results.Results, map[string]any{
							"From (Node)":    node.Name,
							"From (Pod)":     pod.Name,
							"To (Node)":      serverPod.Spec.NodeName,
							"To (Pod)":       serverPod.Name,
							"Send (Mbps)":    fmt.Sprintf("%.2f", result.End.SumSent.BitsPerSecond/1000000),
							"Receive (Mbps)": fmt.Sprintf("%.2f", result.End.SumReceived.BitsPerSecond/1000000),
						})

						jobCompleted = true
						break
					}
				}

			}
		}

	}

	return nil
}
