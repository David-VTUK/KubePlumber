package detect

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/David-VTUK/KubePlumber/common"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NICAttributes(clients common.Clients, runConfig common.RunConfig) error {
	log.Info("Checking NIC Attributes")

	testResults := common.TestResults{}
	testResults.Title = "NIC Information"

	ctx, cancel := context.WithTimeout(context.Background(), clients.Timeout*time.Second)
	defer cancel()

	// Get Nodes
	nodes, err := clients.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, len(nodes.Items))

	for _, node := range nodes.Items {

		wg.Add(1)
		go func(node corev1.Node) error {
			defer wg.Done()
			sem <- struct{}{}        // acquire semaphore
			defer func() { <-sem }() // release semaphore

			err := createNicTestPods(node, clients, runConfig, testResults)
			if err != nil {
				return err
			}
			return nil
		}(node)
	}

	wg.Wait()
	return nil

}

func createNicTestPods(node corev1.Node, clients common.Clients, runConfig common.RunConfig, results common.TestResults) error {

	ctx, cancel := context.WithTimeout(context.Background(), clients.Timeout*time.Second)
	defer cancel()

	pod, err := clients.KubeClient.CoreV1().Pods(runConfig.TestNamespace).Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "nic-check-",
			Namespace:    runConfig.TestNamespace,
		},
		Spec: corev1.PodSpec{
			NodeName:      node.Name,
			RestartPolicy: corev1.RestartPolicyNever,
			HostNetwork:   true,
			Containers: []corev1.Container{
				{
					Name:  "nic-check",
					Image: "virtualthoughts/kubeplumber-niccheck:latest",
				},
			},
		},
	}, metav1.CreateOptions{})

	if err != nil {
		return err
	}

	for {
		time.Sleep(time.Second)
		pod, err := clients.KubeClient.CoreV1().Pods(runConfig.TestNamespace).Get(ctx, pod.Name, metav1.GetOptions{})

		if pod.Status.Phase != "Succeeded" && pod.Status.Phase != "Failed" {
			continue
		}

		if err != nil {
			return err
		}

		if pod.Status.Phase == "Succeeded" {

			podLogOpts := corev1.PodLogOptions{}
			req := clients.KubeClient.CoreV1().Pods(runConfig.TestNamespace).GetLogs(pod.Name, &podLogOpts)
			podLogs, err := req.Stream(ctx)
			if err != nil {
				return err
			}
			defer podLogs.Close()
			buf := new(bytes.Buffer)
			_, err = io.Copy(buf, podLogs)
			if err != nil {
				return err
			}

			var interfaces common.NetworkInterfaces

			err = json.Unmarshal(buf.Bytes(), &interfaces)
			if err != nil {
				log.Info(err)
			}

			for _, iface := range interfaces.Interfaces {
				results.Results = append(results.Results, map[string]any{
					"Node":         node.Name,
					"Iface":        iface.Name,
					"MAC":          iface.MAC,
					"MTU":          iface.MTU,
					"Up":           iface.Up,
					"Broadcast":    iface.Broadcast,
					"Loopback":     iface.Loopback,
					"PointToPoint": iface.PointToPoint,
					"Multicast":    iface.Multicast,
				})
			}

		}

		err = clients.KubeClient.CoreV1().Pods(runConfig.TestNamespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
		if err != nil {
			return err
		}

		break
	}

	return nil
}
