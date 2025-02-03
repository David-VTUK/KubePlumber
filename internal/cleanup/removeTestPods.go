package cleanup

import (
	"context"
	"time"

	"github.com/David-VTUK/KubePlumber/common"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func RemoveTestPods(clients common.Clients, runConfig common.RunConfig) error {
	// Remove all test pods

	log.Infoln("Removing all test pods")

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

	return nil

}
