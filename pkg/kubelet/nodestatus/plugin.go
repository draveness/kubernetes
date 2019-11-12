package nodestatus

import v1 "k8s.io/api/core/v1"

// Plugin is the node status plugin which updates the node object.
type Plugin interface {
	Name() string
	UpdateNode(node *v1.Node) error
}
