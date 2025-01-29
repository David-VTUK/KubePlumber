package setup

import (
	"context"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func CreateNamespace(client kubernetes.Clientset, name string) error {

	namespaces, err := client.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, namespace := range namespaces.Items {
		if namespace.Name == name {
			log.Infof("%s namespace already exists, will not create", name)
			return nil
		}
	}

	_, err = client.CoreV1().Namespaces().Create(context.TODO(), &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}, metav1.CreateOptions{})

	if err != nil {
		return err
	}

	log.Infof("Namespace %s created", name)

	return nil
}
