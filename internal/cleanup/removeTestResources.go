package cleanup

import (
	"context"
	"time"

	"github.com/David-VTUK/KubePlumber/common"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func RemoveTestPods(clients common.Clients, runConfig common.RunConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), clients.Timeout*time.Second)
	defer cancel()

	err := clients.KubeClient.AppsV1().DaemonSets(runConfig.TestNamespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "kubeplumber=true"})
	if err != nil {
		return err
	}

	err = clients.KubeClient.CoreV1().Pods(runConfig.TestNamespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "kubeplumber=true"})
	if err != nil {
		return err
	}

	deletePolicy := metav1.DeletePropagationBackground
	err = clients.KubeClient.BatchV1().Jobs(runConfig.TestNamespace).DeleteCollection(ctx, metav1.DeleteOptions{PropagationPolicy: &deletePolicy}, metav1.ListOptions{LabelSelector: "kubeplumber=true"})
	if err != nil {
		return err
	}

	namespaceList, err := clients.KubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: "kubeplumber=true",
	})

	if err != nil {
		return err
	}

	for _, ns := range namespaceList.Items {
		log.Infof("Deleting namespace %s", ns.Name)
		err := clients.KubeClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
		if err != nil {
			log.Errorf("Failed to delete namespace %s: %s", ns.Name, err)
		}
	}

	return nil

}
