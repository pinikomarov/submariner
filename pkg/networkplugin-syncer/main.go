package main

import (
	"flag"
	"os"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/submariner-io/submariner/pkg/event"
	"github.com/submariner-io/submariner/pkg/event/controller"
	"github.com/submariner-io/submariner/pkg/event/logger"
	"github.com/submariner-io/submariner/pkg/networkplugin-syncer/handlers/ovn"

	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

var (
	masterURL  string
	kubeconfig string
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	klog.Info("Starting submariner-networkplugin-syncer")
	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	registry := event.NewRegistry("networkplugin-syncer", os.Getenv("NETWORK_PLUGIN"))
	if err := registry.AddHandlers(logger.NewHandler(), ovn.NewSyncHandler(getK8sClient())); err != nil {
		klog.Fatalf("Error registering the handlers: %s", err.Error())
	}

	ctl, err := controller.New(&controller.Config{
		Registry:   registry,
		MasterURL:  masterURL,
		Kubeconfig: kubeconfig})

	if err != nil {
		klog.Fatalf("Error creating controller for event handling %v", err)
	}

	err = ctl.Start(stopCh)
	if err != nil {
		klog.Fatalf("Error starting controller: %v", err)
	}

	<-stopCh
	ctl.Stop()

	klog.Info("All controllers stopped or exited. Stopping submariner-networkplugin-syncer")
}

func getK8sClient() kubernetes.Interface {
	var cfg *rest.Config
	var err error
	if masterURL == "" && kubeconfig == "" {
		cfg, err = rest.InClusterConfig()
		if err != nil {
			klog.Fatalf("Error getting in-cluster-config, please set kubeconfig && master parameters")
		}
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
		if err != nil {
			klog.Fatalf("Error building kubeconfig: %s", err.Error())
		}
	}

	clientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building clientset: %s", err.Error())
	}

	return clientSet
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "",
		"The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
}
