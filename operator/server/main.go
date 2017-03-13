package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/grapebaba/fabric-operator/controller"
	"github.com/grapebaba/fabric-operator/util/k8sutil"
	"github.com/grapebaba/fabric-operator/util/k8sutil/election"
	"github.com/grapebaba/fabric-operator/util/k8sutil/election/resourcelock"
	"github.com/grapebaba/fabric-operator/util/retryutil"
	"github.com/grapebaba/fabric-operator/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/tools/record"
)

var (
	pvProvisioner string
	namespace     string
	name          string

	printVersion bool
)

var (
	leaseDuration = 15 * time.Second
	renewDuration = 5 * time.Second
	retryPeriod   = 3 * time.Second
)

func init() {
	flag.BoolVar(&printVersion, "version", false, "Show version and quit")
	flag.Parse()

	// Workaround for watching TPR resource.
	restCfg, err := k8sutil.InClusterConfig()
	if err != nil {
		panic(err)
	}
	controller.MasterHost = restCfg.Host
	restcli, err := k8sutil.NewTPRClient()
	if err != nil {
		panic(err)
	}
	controller.KubeHttpCli = restcli.Client
}

func main() {
	namespace = os.Getenv("MY_POD_NAMESPACE")
	if len(namespace) == 0 {
		logrus.Fatalf("must set env MY_POD_NAMESPACE")
	}
	name = os.Getenv("MY_POD_NAME")
	if len(name) == 0 {
		logrus.Fatalf("must set env MY_POD_NAME")
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c)
	go func() {
		logrus.Infof("received signal: %v", <-c)
		os.Exit(1)
	}()

	if printVersion {
		fmt.Println("etcd-operator Version:", version.Version)
		fmt.Println("Git SHA:", version.GitSHA)
		fmt.Println("Go Version:", runtime.Version())
		fmt.Printf("Go OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	logrus.Infof("fabric-operator Version: %v", version.Version)
	logrus.Infof("Git SHA: %s", version.GitSHA)
	logrus.Infof("Go Version: %s", runtime.Version())
	logrus.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)

	id, err := os.Hostname()
	if err != nil {
		logrus.Fatalf("failed to get hostname: %v", err)
	}

	// TODO: replace this to client-go once leader election pacakge is imported
	//       https://github.com/kubernetes/client-go/issues/28
	rl := &resourcelock.EndpointsLock{
		EndpointsMeta: api.ObjectMeta{
			Namespace: namespace,
			Name:      "fabric-operator",
		},
		Client: k8sutil.MustNewKubeClient(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: &record.FakeRecorder{},
		},
	}

	election.RunOrDie(election.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: leaseDuration,
		RenewDeadline: renewDuration,
		RetryPeriod:   retryPeriod,
		Callbacks: election.LeaderCallbacks{
			OnStartedLeading: run,
			OnStoppedLeading: func() {
				logrus.Fatalf("leader election lost")
			},
		},
	})
	panic("unreachable")
}

func run(stop <-chan struct{}) {
	cfg := newControllerConfig()
	if err := cfg.Validate(); err != nil {
		logrus.Fatalf("invalid operator config: %v", err)
	}

	for {
		c := controller.New(cfg)
		err := c.Run()
		switch err {
		case controller.ErrVersionOutdated:
		default:
			logrus.Fatalf("controller Run() ended with failure: %v", err)
		}
	}
}

func newControllerConfig() controller.Config {
	kubecli := k8sutil.MustNewKubeClient()

	serviceAccount, err := getMyPodServiceAccount(kubecli)
	if err != nil {
		logrus.Fatalf("fail to get my pod's service account: %v", err)
	}

	cfg := controller.Config{
		Namespace:      namespace,
		ServiceAccount: serviceAccount,
		PVProvisioner:  pvProvisioner,
		KubeCli:        kubecli,
	}

	return cfg
}

func getMyPodServiceAccount(kubecli kubernetes.Interface) (string, error) {
	var sa string
	err := retryutil.Retry(5*time.Second, 100, func() (bool, error) {
		pod, err := kubecli.CoreV1().Pods(namespace).Get(name)
		if err != nil {
			logrus.Errorf("fail to get operator pod (%s): %v", name, err)
			return false, nil
		}
		sa = pod.Spec.ServiceAccountName
		return true, nil
	})
	return sa, err
}
