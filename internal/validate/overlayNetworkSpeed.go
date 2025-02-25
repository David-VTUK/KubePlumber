package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/David-VTUK/KubePlumber/common"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func RunOverlayNetworkSpeedTests(clients common.Clients, restConfig *rest.Config, clusterDomain string, runConfig common.RunConfig) error {

	log.Info("Checking overlay network speed")
	err := CheckOverlayNetworkSpeed(clients, restConfig, clusterDomain, runConfig.TestNamespace)
	if err != nil {
		return fmt.Errorf("error checking overlay network speed: %s", err)
	}

	return nil

}

func CheckOverlayNetworkSpeed(clients common.Clients, restConfig *rest.Config, clusterDomain string, namespace string) error {

	log.Info("Checking overlay network speed")

	t := table.NewWriter()
	t.SetStyle(table.StyleColoredDark)
	t.SetOutputMirror(os.Stdout)
	t.SetTitle("Overlay Networking Tests")
	t.Style().Title.Align = text.AlignCenter
	t.AppendHeader(table.Row{"From (Node)", "From (Pod)", "To (Node)", "To (Pod)", "Bitrate (Sender) mbit", "Bitrate (Receiver) mbit"})

	err := createNicTestPods(clients, namespace, t)
	if err != nil {
		return fmt.Errorf("error creating iperf pods: %s", err)
	}

	t.Render()
	return nil
}

func createNicTestPods(clients common.Clients, namespace string, t table.Writer) error {
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
					"app": "iperf-server",
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
						"app": "iperf-client",
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

				log.Info("Getting job" + job.Name)
				job, err = clients.KubeClient.BatchV1().Jobs(namespace).Get(ctx, job.GetName(), metav1.GetOptions{})

				if err != nil {
					log.Info("YEET")
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

						t.AppendRow(table.Row{node.Name, pod.Name, serverPod.Spec.NodeName, serverPod.Name, fmt.Sprintf("%.2f", result.End.SumSent.BitsPerSecond/1000000), fmt.Sprintf("%.2f", result.End.SumReceived.BitsPerSecond/1000000)})

						jobCompleted = true

						break
					}
				}

			}
		}

	}

	return nil
}
