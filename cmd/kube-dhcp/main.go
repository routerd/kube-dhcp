/*
Copyright 2021 The routerd authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	dhcpv1alpha1 "routerd.net/kube-dhcp/api/v1alpha1"
	"routerd.net/kube-dhcp/internal/server"
	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = dhcpv1alpha1.AddToScheme(scheme)
	_ = ipamv1alpha1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var (
		// Kubernetes Namespace this DHCP Server is deployed in.
		namespace = os.Getenv("KUBERNETES_NAMESPACE")
		// Interface Name the DHCP Server should bind to.
		bindInterface = os.Getenv("DHCP_BIND_INTERFACE")
		// Name of the DHCPServer object in Kubernetes.
		dhcpServerName = os.Getenv("DHCP_SERVER_NAME")
	)

	if len(namespace) == 0 ||
		len(bindInterface) == 0 ||
		len(dhcpServerName) == 0 {
		err := fmt.Errorf(
			"env vars KUBERNETES_NAMESPACE, DHCP_BIND_INTERFACE and DHCP_SERVER_NAME are required")
		exitOnError("invalid configuration:", err)
	}

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		// disable metrics as we are not running any workers.
		MetricsBindAddress: "0",
		Namespace:          namespace,
	})
	exitOnError("creating manager", err)

	s := server.NewServer(server.Config{
		Logger:         ctrl.Log.WithName("dhcp"),
		Client:         mgr.GetClient(),
		BindInterface:  bindInterface,
		Namespace:      namespace,
		DHCPServerName: dhcpServerName,
	})
	exitOnError("adding server to manager", mgr.Add(s))

	setupLog.Info("starting DHCP server")
	exitOnError("starting DHCP server", mgr.Start(ctrl.SetupSignalHandler()))
}

func exitOnError(msg string, err error) {
	if err == nil {
		return
	}

	fmt.Fprintf(os.Stderr, "%s: %v", msg, err)
	os.Exit(1)
}
