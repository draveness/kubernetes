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
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

// WaitForRCPodToDisappear returns nil if the pod from the given replication controller (described by rcName) no longer exists.
// In case of failure or too long waiting time, an error is returned.
func WaitForRCPodToDisappear(c clientset.Interface, ns, rcName, podName string) error {
	label := labels.SelectorFromSet(labels.Set(map[string]string{"name": rcName}))
	// NodeController evicts pod after 5 minutes, so we need timeout greater than that to observe effects.
	// The grace period must be set to 0 on the pod for it to be deleted during the partition.
	// Otherwise, it goes to the 'Terminating' state till the kubelet confirms deletion.
	return WaitForPodToDisappear(c, ns, podName, label, 20*time.Second, 10*time.Minute)
}

// WaitForReplicationController waits until the RC appears (exist == true), or disappears (exist == false)
func WaitForReplicationController(c clientset.Interface, namespace, name string, exist bool, interval, timeout time.Duration) error {
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		_, err := c.CoreV1().ReplicationControllers(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			Logf("Get ReplicationController %s in namespace %s failed (%v).", name, namespace, err)
			return !exist, nil
		}
		Logf("ReplicationController %s in namespace %s found.", name, namespace)
		return exist, nil
	})
	if err != nil {
		stateMsg := map[bool]string{true: "to appear", false: "to disappear"}
		return fmt.Errorf("error waiting for ReplicationController %s/%s %s: %v", namespace, name, stateMsg[exist], err)
	}
	return nil
}

// WaitForReplicationControllerwithSelector waits until any RC with given selector appears (exist == true), or disappears (exist == false)
func WaitForReplicationControllerwithSelector(c clientset.Interface, namespace string, selector labels.Selector, exist bool, interval,
	timeout time.Duration) error {
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		rcs, err := c.CoreV1().ReplicationControllers(namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
		switch {
		case len(rcs.Items) != 0:
			Logf("ReplicationController with %s in namespace %s found.", selector.String(), namespace)
			return exist, nil
		case len(rcs.Items) == 0:
			Logf("ReplicationController with %s in namespace %s disappeared.", selector.String(), namespace)
			return !exist, nil
		default:
			Logf("List ReplicationController with %s in namespace %s failed: %v", selector.String(), namespace, err)
			return false, nil
		}
	})
	if err != nil {
		stateMsg := map[bool]string{true: "to appear", false: "to disappear"}
		return fmt.Errorf("error waiting for ReplicationControllers with %s in namespace %s %s: %v", selector.String(), namespace, stateMsg[exist], err)
	}
	return nil
}
