package nodeaddress

import (
	"net"

	v1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/kubernetes/pkg/kubelet/nodestatus"
)

// NodeAddress is a node status plugin.
type NodeAddress struct {
	nodeIP                net.IP                           // typically Kubelet.nodeIP
	validateNodeIPFunc    func(net.IP) error               // typically Kubelet.nodeIPValidator
	hostname              string                           // typically Kubelet.hostname
	hostnameOverridden    bool                             // was the hostname force set?
	externalCloudProvider bool                             // typically Kubelet.externalCloudProvider
	cloud                 cloudprovider.Interface          // typically Kubelet.cloud
	nodeAddressesFunc     func() ([]v1.NodeAddress, error) // typically Kubelet.cloudResourceSyncManager.NodeAddresses
}

var _ nodestatus.Plugin = &NodeAddress{}

// Name is the plugin name.
func (p *NodeAddress) Name() string {
	return "NodeAddress"
}

// UpdateNode updates the original node object.
func (p *NodeAddress) UpdateNode(node *v1.Node) error {
}

// New returns a node status plugin.
func New(
	nodeIP net.IP, // typically Kubelet.nodeIP
	validateNodeIPFunc func(net.IP) error, // typically Kubelet.nodeIPValidator
	hostname string, // typically Kubelet.hostname
	hostnameOverridden bool, // was the hostname force set?
	externalCloudProvider bool, // typically Kubelet.externalCloudProvider
	cloud cloudprovider.Interface, // typically Kubelet.cloud
	nodeAddressesFunc func() ([]v1.NodeAddress, error), // typically Kubelet.cloudResourceSyncManager.NodeAddresses
) *NodeAddress {
	return &NodeAddress{}
}
