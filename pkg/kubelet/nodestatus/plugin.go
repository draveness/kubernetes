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

import v1 "k8s.io/api/core/v1"

// Plugin is the node status plugin which update the node's
// status.
type Plugin interface {
	// Name is the name of the plugin.
	Name() string

	// Update updates the status of the node.
	Update(node *v1.Node) error
}
