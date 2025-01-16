package validate

import (
	"context"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// This package validates internal, external, intra and inter node DNS resolution

// getDNSInformation identifies the DNS configuration of the cluster
func getDNSInformation(clientset *kubernetes.Clientset) {

}

func dns(clientset *kubernetes.Clientset) error {

	log.Info("Getting cluster-dns ConfigMap")
	config, err := clientset.CoreV1().ConfigMaps("kube-system").Get(context.TODO(), "cluster-dns", metav1.GetOptions{})
	if err != nil {
		log.Error("Error getting CoreDNS ConfigMap", err)
		return err
	}

	//log.Info("CoreDNS ConfigMap", config)

	clusterDNSDomain := config.Data["clusterDomain"]
	clusterDNSEndpoint := config.Data["clusterDNS"]

	log.Infof("Cluster DNS Domain: %s", clusterDNSDomain)
	log.Infof("Cluster DNS Endpoint: %s", clusterDNSEndpoint)
	return nil

}
