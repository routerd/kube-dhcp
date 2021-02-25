module routerd.net/kube-dhcp

go 1.13

require (
	github.com/go-logr/logr v0.3.0
	github.com/google/go-cmp v0.5.2
	github.com/insomniacslk/dhcp v0.0.0-20210120172423-cc9239ac6294
	github.com/k8snetworkplumbingwg/network-attachment-definition-client v1.1.0
	k8s.io/api v0.20.2
	k8s.io/apimachinery v0.20.2
	k8s.io/client-go v0.20.2
	k8s.io/utils v0.0.0-20210111153108-fddb29f9d009
	routerd.net/kube-ipam v0.0.0-20210217190427-198d0e54042a
	sigs.k8s.io/controller-runtime v0.8.2
)
