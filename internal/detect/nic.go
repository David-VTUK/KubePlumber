package detect

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/David-VTUK/KubePlumber/common"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NICAttributes(clients common.Clients, runConfig common.RunConfig, results *common.ResultsStore) error {
	log.Info("Checking NIC Attributes")

	ctx, cancel := context.WithTimeout(context.Background(), clients.Timeout*time.Second)
	defer cancel()

	nodes, err := clients.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	results.MarkRunning(common.SectionNIC)

	var wg sync.WaitGroup
	sem := make(chan struct{}, len(nodes.Items))

	for _, node := range nodes.Items {
		wg.Add(1)
		go func(node corev1.Node) error {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			err := createNicTestPods(node, clients, runConfig, results)
			if err != nil {
				return err
			}
			return nil
		}(node)
	}

	wg.Wait()
	results.MarkComplete(common.SectionNIC)
	return nil
}

func createNicTestPods(node corev1.Node, clients common.Clients, runConfig common.RunConfig, results *common.ResultsStore) error {

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
				results.AddRow(common.SectionNIC, common.ResultRow{
					node.Name,
					iface.Name,
					iface.MAC,
					strconv.Itoa(iface.MTU),
					strconv.FormatBool(iface.Up),
					strconv.FormatBool(iface.Broadcast),
					strconv.FormatBool(iface.Loopback),
					strconv.FormatBool(iface.PointToPoint),
					strconv.FormatBool(iface.Multicast),
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
