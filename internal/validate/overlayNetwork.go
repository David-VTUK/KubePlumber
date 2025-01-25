package validate

import (
	"context"
	"os"
	"time"

	"github.com/David-VTUK/KubePlumber/common"
	"github.com/jedib0t/go-pretty/v6/table"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func RunOverlayNetworkTests(clients common.Clients, restConfig *rest.Config, clusterDomain string) {

	log.Info("Running Overlay Network Tests")
	err := CheckOverlayNetwork(clients.KubeClient, restConfig, clusterDomain)
	if err != nil {
		log.Info("Error running Overlay Network tests: ", err)
	}

}

func CheckOverlayNetwork(clientset *kubernetes.Clientset, restConfig *rest.Config, clusterDomain string) error {

	t := table.NewWriter()
	t.SetStyle(table.StyleColoredDark)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"From (Node)", "From (Pod)", "To (Node)", "To (Pod)", "Intra/Inter", "Status", "Domain"})

	podList, err := CreateDaemonSet(clientset)

	if err != nil {
		log.Error("Error creating DaemonSet: ", err)
		return err
	}

	for _, pod := range podList.Items {
		for _, targetPod := range podList.Items {

			log.Info("Pod: ", pod.Name, " Target Pod: ", targetPod.Name)
			if pod.Name == targetPod.Name {
				continue
			} else {

				// run curl command
				_ = RunCurlCommand(clientset, restConfig, pod, targetPod, clusterDomain)
			}

		}
	}
	//t.Render()
	return nil
}

func CreateDaemonSet(clientset *kubernetes.Clientset) (v1.PodList, error) {

	// Create Daemonset
	daemonSet, err := clientset.AppsV1().DaemonSets("default").Create(context.TODO(), &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "overlay-network-test",
			Namespace:    "default",
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "overlay-network-test",
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "overlay-network-test",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
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
		//return v1.PodList{}, err
	}

	// Wait for DaemonSet to be ready
	for {
		time.Sleep(time.Second)
		log.Info("Waiting for DaemonSet to be ready")
		daemonSet, err = clientset.AppsV1().DaemonSets("default").Get(context.TODO(), daemonSet.Name, metav1.GetOptions{})

		if err != nil {
			log.Error("Error getting DaemonSet: ", err)
			return v1.PodList{}, err
		}

		if daemonSet.Status.NumberReady == daemonSet.Status.DesiredNumberScheduled {
			log.Info("DaemonSet Ready")
			break
		}
	}

	podList, err := clientset.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app=" + daemonSet.GenerateName,
	})

	for _, pod := range podList.Items {
		// output pod IP Address
		log.Info("Pod: ", pod.Status.PodIP)
	}

	if err != nil {
		log.Error("Error getting pod list: ", err)
		return v1.PodList{}, err
	}

	return *podList, nil
}

func RunCurlCommand(clientset *kubernetes.Clientset, restConfig *rest.Config, pod v1.Pod, targetPod v1.Pod, clusterDomain string) error {

	command := []string{"curl", targetPod.Status.PodIP}
	log.Infof("Command: %v", command)

	/*
		req := clientset.CoreV1().RESTClient().Post().
			Resource("pods").
			Name(pod.Name).
			Namespace(pod.Namespace).
			SubResource("exec")

		scheme := runtime.NewScheme()
		if err := corev1.AddToScheme(scheme); err != nil {
			return err
		}

		parameterCodec := runtime.NewParameterCodec(scheme)

		req.VersionedParams(&corev1.PodExecOptions{
			Command:   command,
			Container: pod.Spec.Containers[0].Name,
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
			return err
		}
	*/

	return nil
}
