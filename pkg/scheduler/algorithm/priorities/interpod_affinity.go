/*
Copyright 2016 The Kubernetes Authors.

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

package priorities

import (
	"fmt"
	"math"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/scheduler/algorithm"
	"k8s.io/kubernetes/pkg/scheduler/algorithm/predicates"
	priorityutil "k8s.io/kubernetes/pkg/scheduler/algorithm/priorities/util"
	schedulerapi "k8s.io/kubernetes/pkg/scheduler/api"
	schedulernodeinfo "k8s.io/kubernetes/pkg/scheduler/nodeinfo"

	"k8s.io/klog"
)

// InterPodAffinity contains information to calculate inter pod affinity.
type InterPodAffinity struct {
	info                  predicates.NodeInfo
	nodeLister            algorithm.NodeLister
	podLister             algorithm.PodLister
	hardPodAffinityWeight int32
}

// NewInterPodAffinityPriority creates an InterPodAffinity.
func NewInterPodAffinityPriority(
	info predicates.NodeInfo,
	nodeLister algorithm.NodeLister,
	podLister algorithm.PodLister,
	hardPodAffinityWeight int32) (PriorityMapFunction, PriorityReduceFunction) {
	interPodAffinity := &InterPodAffinity{
		info:                  info,
		nodeLister:            nodeLister,
		podLister:             podLister,
		hardPodAffinityWeight: hardPodAffinityWeight,
	}
	return interPodAffinity.CalculateInterPodAffinityPriorityMap, interPodAffinity.CalculateInterPodAffinityPriorityReduce
}

func (ipa *InterPodAffinity) processTerm(term *v1.PodAffinityTerm, podDefiningAffinityTerm, podToCheck *v1.Pod, fixedNode *v1.Node, weight int64) (int64, error) {
	namespaces := priorityutil.GetNamespacesFromPodAffinityTerm(podDefiningAffinityTerm, term)
	selector, err := metav1.LabelSelectorAsSelector(term.LabelSelector)
	if err != nil {
		return weight, err
	}

	totalWeight := int64(0)
	match := priorityutil.PodMatchesTermsNamespaceAndSelector(podToCheck, namespaces, selector)
	if match {
		nodes, err := ipa.nodeLister.List()
		if err != nil {
			return 0, err
		}
		for _, node := range nodes {
			if priorityutil.NodesHaveSameTopologyKey(node, fixedNode, term.TopologyKey) {
				totalWeight += weight
			}
		}
	}

	return totalWeight, nil
}

func (ipa *InterPodAffinity) processTerms(terms []v1.WeightedPodAffinityTerm, podDefiningAffinityTerm, podToCheck *v1.Pod, fixedNode *v1.Node, multiplier int) (int64, error) {
	totalWeight := int64(0)
	for i := range terms {
		term := &terms[i]
		weight, err := ipa.processTerm(&term.PodAffinityTerm, podDefiningAffinityTerm, podToCheck, fixedNode, int64(term.Weight*int32(multiplier)))
		if err != nil {
			return 0, err
		}

		totalWeight += weight
	}

	return totalWeight, nil
}

// CalculateInterPodAffinityPriorityMap returns error when node not found.
func (ipa *InterPodAffinity) CalculateInterPodAffinityPriorityMap(pod *v1.Pod, meta interface{}, nodeInfo *schedulernodeinfo.NodeInfo) (schedulerapi.HostPriority, error) {
	node := nodeInfo.Node()
	if node == nil {
		return schedulerapi.HostPriority{}, fmt.Errorf("node not found")
	}

	affinity := pod.Spec.Affinity
	hasAffinityConstraints := affinity != nil && affinity.PodAffinity != nil
	hasAntiAffinityConstraints := affinity != nil && affinity.PodAntiAffinity != nil

	processPod := func(existingPod *v1.Pod) (int64, error) {
		existingPodNode, err := ipa.info.GetNodeInfo(existingPod.Spec.NodeName)
		if err != nil {
			if apierrors.IsNotFound(err) {
				klog.Errorf("Node not found, %v", existingPod.Spec.NodeName)
				return 0, nil
			}
			return 0, err
		}
		existingPodAffinity := existingPod.Spec.Affinity
		existingHasAffinityConstraints := existingPodAffinity != nil && existingPodAffinity.PodAffinity != nil
		existingHasAntiAffinityConstraints := existingPodAffinity != nil && existingPodAffinity.PodAntiAffinity != nil

		totalWeight := int64(0)
		if hasAffinityConstraints {
			// For every soft pod affinity term of <pod>, if <existingPod> matches the term,
			// increment <pm.counts> for every node in the cluster with the same <term.TopologyKey>
			// value as that of <existingPods>`s node by the term`s weight.
			terms := affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			weight, err := ipa.processTerms(terms, pod, existingPod, existingPodNode, 1)
			if err != nil {
				return 0, err
			}

			totalWeight += weight
		}
		if hasAntiAffinityConstraints {
			// For every soft pod anti-affinity term of <pod>, if <existingPod> matches the term,
			// decrement <pm.counts> for every node in the cluster with the same <term.TopologyKey>
			// value as that of <existingPod>`s node by the term`s weight.
			terms := affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			weight, err := ipa.processTerms(terms, pod, existingPod, existingPodNode, -1)
			if err != nil {
				return 0, err
			}

			totalWeight += weight
		}

		if existingHasAffinityConstraints {
			// For every hard pod affinity term of <existingPod>, if <pod> matches the term,
			// increment <pm.counts> for every node in the cluster with the same <term.TopologyKey>
			// value as that of <existingPod>'s node by the constant <ipa.hardPodAffinityWeight>
			if ipa.hardPodAffinityWeight > 0 {
				terms := existingPodAffinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution
				// TODO: Uncomment this block when implement RequiredDuringSchedulingRequiredDuringExecution.
				//if len(existingPodAffinity.PodAffinity.RequiredDuringSchedulingRequiredDuringExecution) != 0 {
				//	terms = append(terms, existingPodAffinity.PodAffinity.RequiredDuringSchedulingRequiredDuringExecution...)
				//}
				for _, term := range terms {
					weight, err := ipa.processTerm(&term, existingPod, pod, existingPodNode, int64(ipa.hardPodAffinityWeight))
					if err != nil {
						return 0, err
					}

					totalWeight += weight
				}
			}
			// For every soft pod affinity term of <existingPod>, if <pod> matches the term,
			// increment <pm.counts> for every node in the cluster with the same <term.TopologyKey>
			// value as that of <existingPod>'s node by the term's weight.
			terms := existingPodAffinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			weight, err := ipa.processTerms(terms, existingPod, pod, existingPodNode, 1)
			if err != nil {
				return 0, err
			}

			totalWeight += weight
		}
		if existingHasAntiAffinityConstraints {
			// For every soft pod anti-affinity term of <existingPod>, if <pod> matches the term,
			// decrement <pm.counts> for every node in the cluster with the same <term.TopologyKey>
			// value as that of <existingPod>'s node by the term's weight.
			terms := existingPodAffinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			weight, err := ipa.processTerms(terms, existingPod, pod, existingPodNode, -1)
			if err != nil {
				return 0, err
			}

			totalWeight += weight
		}
		return totalWeight, nil
	}

	var score int64
	var err error
	if hasAffinityConstraints || hasAntiAffinityConstraints {
		// We need to process all the pods.
		for _, existingPod := range nodeInfo.Pods() {
			if score, err = processPod(existingPod); err != nil {
				return schedulerapi.HostPriority{}, err
			}
		}
	} else {
		// The pod doesn't have any constraints - we need to check only existing
		// ones that have some.
		for _, existingPod := range nodeInfo.PodsWithAffinity() {
			if score, err = processPod(existingPod); err != nil {
				return schedulerapi.HostPriority{}, err
			}
		}
	}

	return schedulerapi.HostPriority{
		Host:  node.Name,
		Score: int(score),
	}, nil
}

// CalculateInterPodAffinityPriorityReduce compute a sum by iterating through the elements of weightedPodAffinityTerm and adding
// "weight" to the sum if the corresponding PodAffinityTerm is satisfied for
// that node; the node(s) with the highest sum are the most preferred.
// Symmetry need to be considered for preferredDuringSchedulingIgnoredDuringExecution from podAffinity & podAntiAffinity,
// symmetry need to be considered for hard requirements from podAffinity
func (ipa *InterPodAffinity) CalculateInterPodAffinityPriorityReduce(pod *v1.Pod, meta interface{}, nodeNameToInfo map[string]*schedulernodeinfo.NodeInfo, result schedulerapi.HostPriorityList) error {
	// convert the topology key based weights to the node name based weights
	var maxCount, minCount float64

	for _, hostPriority := range result {
		maxCount = math.Max(maxCount, float64(hostPriority.Score))
		minCount = math.Min(minCount, float64(hostPriority.Score))
	}

	// calculate final priority score for each node
	maxMinDiff := maxCount - minCount
	for i, hostPriority := range result {
		fScore := float64(0)
		if maxMinDiff > 0 && hostPriority.Score != 0 {
			fScore = float64(schedulerapi.MaxPriority) * (float64(hostPriority.Score) - minCount) / (maxCount - minCount)
		}

		result[i].Host = hostPriority.Host
		result[i].Score = int(fScore)
		if klog.V(10) {
			klog.Infof("%v -> %v: InterPodAffinityPriority, Score: (%d)", pod.Name, hostPriority.Host, int(fScore))
		}
	}
	return nil
}
