package validate

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/David-VTUK/KubePlumber/common"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

func RunOverlayNetworkTests(clients common.Clients, restConfig *rest.Config, clusterDomain string, runConfig common.RunConfig, results *common.ResultsStore) error {

	log.Info("Checking overlay network")
	err := CheckOverlayNetwork(clients, restConfig, clusterDomain, runConfig.TestNamespace, results)
	if err != nil {
		return err
	}

	return nil
}

func CheckOverlayNetwork(clients common.Clients, restConfig *rest.Config, clusterDomain string, namespace string, results *common.ResultsStore) error {

	ctx, cancel := context.WithTimeout(context.Background(), clients.Timeout*time.Second)
	defer cancel()

	daemonSet, err := CreateDaemonSet(clients, namespace)
	if err != nil {
		return err
	}

	podList, err := clients.KubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + daemonSet.GenerateName,
	})
	if err != nil {
		return err
	}

	results.MarkRunning(common.SectionOverlay)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

	for _, pod := range podList.Items {
		for _, targetPod := range podList.Items {
			wg.Add(1)

			go func(clients common.Clients, restConfig *rest.Config, pod corev1.Pod, targetPod corev1.Pod) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				if pod.Name != targetPod.Name {
					_ = RunCurlCommand(clients, restConfig, pod, targetPod, results)
				}
			}(clients, restConfig, pod, targetPod)
		}
	}

	wg.Wait()
	results.MarkComplete(common.SectionOverlay)

	err = clients.KubeClient.AppsV1().DaemonSets(namespace).Delete(ctx, daemonSet.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}

	return nil
}

func CreateDaemonSet(clients common.Clients, namespace string) (appsv1.DaemonSet, error) {

	ctx, cancel := context.WithTimeout(context.Background(), clients.Timeout*time.Second)
	defer cancel()

	daemonSet, err := clients.KubeClient.AppsV1().DaemonSets(namespace).Create(ctx, &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "overlay-network-test",
			Namespace:    namespace,
			Labels: map[string]string{
				"kubeplumber": "true",
			},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "overlay-network-test",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "overlay-network-test",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "overlay-network-test",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
	}, metav1.CreateOptions{})

	if err != nil {
		log.Error("Error creating DaemonSet: ", err)
	}

	for {
		time.Sleep(time.Second)
		daemonSet, err = clients.KubeClient.AppsV1().DaemonSets(namespace).Get(ctx, daemonSet.Name, metav1.GetOptions{})

		if err != nil {
			return appsv1.DaemonSet{}, err
		}

		if daemonSet.Status.NumberReady == daemonSet.Status.DesiredNumberScheduled {
			break
		}
	}

	return *daemonSet, nil
}

func RunCurlCommand(clients common.Clients, restConfig *rest.Config, sourcePod corev1.Pod, targetPod corev1.Pod, results *common.ResultsStore) error {

	command := []string{"curl", "-o", "/dev/null", "-s", "-w", "%{http_code}", targetPod.Status.PodIP}

	req := clients.KubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(sourcePod.Name).
		Namespace(sourcePod.Namespace).
		SubResource("exec")

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return err
	}

	parameterCodec := runtime.NewParameterCodec(scheme)

	req.VersionedParams(&corev1.PodExecOptions{
		Command:   command,
		Container: sourcePod.Spec.Containers[0].Name,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, parameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return err
	}

	var stdout, stderr bytes.Buffer

	err = exec.StreamWithContext(context.Background(), remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})

	if err != nil {
		log.Error("Error running command: ", err)
	}

	if stdout.String() == "200" {
		results.AddRow(common.SectionOverlay, common.ResultRow{
			sourcePod.Spec.NodeName, sourcePod.Name, targetPod.Spec.NodeName, targetPod.Name, "Success", "TCP 80",
		})
	} else {
		results.AddRow(common.SectionOverlay, common.ResultRow{
			sourcePod.Spec.NodeName, sourcePod.Name, targetPod.Spec.NodeName, targetPod.Name, "Failed", "TCP 80",
		})
	}

	return nil
}
