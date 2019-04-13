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

package replication

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RcByNamePort returns a ReplicationController with specified name and port
func RcByNamePort(name string, replicas int32, image string, port int, protocol v1.Protocol,
	labels map[string]string, gracePeriod *int64) *v1.ReplicationController {

	return RcByNameContainer(name, replicas, image, labels, v1.Container{
		Name:  name,
		Image: image,
		Ports: []v1.ContainerPort{{ContainerPort: int32(port), Protocol: protocol}},
	}, gracePeriod)
}

// RcByNameContainer returns a ReplicationController with specified name and container
func RcByNameContainer(name string, replicas int32, image string, labels map[string]string, c v1.Container,
	gracePeriod *int64) *v1.ReplicationController {

	zeroGracePeriod := int64(0)

	// Add "name": name to the labels, overwriting if it exists.
	labels["name"] = name
	if gracePeriod == nil {
		gracePeriod = &zeroGracePeriod
	}
	return &v1.ReplicationController{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ReplicationController",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ReplicationControllerSpec{
			Replicas: func(i int32) *int32 { return &i }(replicas),
			Selector: map[string]string{
				"name": name,
			},
			Template: &v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: v1.PodSpec{
					Containers:                    []v1.Container{c},
					TerminationGracePeriodSeconds: gracePeriod,
				},
			},
		},
	}
}
