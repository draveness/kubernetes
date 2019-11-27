/*
Copyright 2019 The Kubernetes Authors.

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

package nodestatus

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"
)

var (
	pluginsOrdering = []string{"NodeAddress"}
)

// Registry preserves the invoking order of node statuts plugin.
type Registry struct {
	plugins map[string]Plugin
}

// NewRegistry returns a node status plugin registry.
func NewRegistry() *Registry {
	return &Registry{
		plugins: map[string]Plugin{},
	}
}

// Register appends a plugin to the registry.
func (r *Registry) Register(plugin Plugin) {
	r.plugins[plugin.Name()] = plugin
}

// UpdateNode updates node.
func (r *Registry) UpdateNode(node *v1.Node) {
	for _, pluginName := range pluginsOrdering {
		plugin, ok := r.plugins[pluginName]
		if !ok {
			continue
		}

		klog.V(5).Infof("Setting node status with plugin %s", pluginName)

		if err := plugin.Update(node); err != nil {
			klog.Errorf("Failed to set some node status fields: %s", err.Error())
		}
	}
}
