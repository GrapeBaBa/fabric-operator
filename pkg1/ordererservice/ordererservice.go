package ordererservice

import (
	logrus "github.com/Sirupsen/logrus"
	"github.com/grapebaba/fabric-operator/pkg1/fabric"
	"k8s.io/client-go/kubernetes"
)

type Config struct {
	ServiceAccount string

	KubeCli kubernetes.Interface
}

type OrdererService struct {
	logger *logrus.Entry

	config Config
}


func New(config fabric.Config) *OrdererService{
	return nil
}