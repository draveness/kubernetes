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
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
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

type podAffinityPriorityMap struct {
	sync.Mutex

	// nodes contain all nodes that should be considered
	nodes []*v1.Node
	// counts store the mapping from node name to so-far computed score of
	// the node.
	counts map[string]*int64
	// The first error that we faced.
	firstError error
}

func newPodAffinityPriorityMap(nodes []*v1.Node) *podAffinityPriorityMap {
	return &podAffinityPriorityMap{
		nodes:  nodes,
		counts: make(map[string]*int64, len(nodes)),
	}
}

func (p *podAffinityPriorityMap) setError(err error) {
	p.Lock()
	defer p.Unlock()
	if p.firstError == nil {
		p.firstError = err
	}
}

func (p *podAffinityPriorityMap) processTerm(term *v1.PodAffinityTerm, podDefiningAffinityTerm, podToCheck *v1.Pod, fixedNode *v1.Node, weight int64) {
	namespaces := priorityutil.GetNamespacesFromPodAffinityTerm(podDefiningAffinityTerm, term)
	selector, err := metav1.LabelSelectorAsSelector(term.LabelSelector)
	if err != nil {
		p.setError(err)
		return
	}
	match := priorityutil.PodMatchesTermsNamespaceAndSelector(podToCheck, namespaces, selector)
	if match {
		for _, node := range p.nodes {
			if priorityutil.NodesHaveSameTopologyKey(node, fixedNode, term.TopologyKey) {
				atomic.AddInt64(p.counts[node.Name], weight)
			}
		}
	}
}

func (p *podAffinityPriorityMap) processTerms(terms []v1.WeightedPodAffinityTerm, podDefiningAffinityTerm, podToCheck *v1.Pod, fixedNode *v1.Node, multiplier int) {
	for i := range terms {
		term := &terms[i]
		p.processTerm(&term.PodAffinityTerm, podDefiningAffinityTerm, podToCheck, fixedNode, int64(term.Weight*int32(multiplier)))
	}
}

// CalculateInterPodAffinityPriorityMap returns error when node not found.
func (ipa *InterPodAffinity) CalculateInterPodAffinityPriorityMap(pod *v1.Pod, meta interface{}, nodeInfo *schedulernodeinfo.NodeInfo) (schedulerapi.HostPriority, error) {
	node := nodeInfo.Node()
	if node == nil {
		return schedulerapi.HostPriority{}, fmt.Errorf("node not found")
	}

	return schedulerapi.HostPriority{
		Host: node.Name,
	}, nil
}

// CalculateInterPodAffinityPriorityReduce compute a sum by iterating through the elements of weightedPodAffinityTerm and adding
// "weight" to the sum if the corresponding PodAffinityTerm is satisfied for
// that node; the node(s) with the highest sum are the most preferred.
// Symmetry need to be considered for preferredDuringSchedulingIgnoredDuringExecution from podAffinity & podAntiAffinity,
// symmetry need to be considered for hard requirements from podAffinity
func (ipa *InterPodAffinity) CalculateInterPodAffinityPriorityReduce(pod *v1.Pod, meta interface{}, nodeNameToInfo map[string]*schedulernodeinfo.NodeInfo, result schedulerapi.HostPriorityList) error {
	affinity := pod.Spec.Affinity
	hasAffinityConstraints := affinity != nil && affinity.PodAffinity != nil
	hasAntiAffinityConstraints := affinity != nil && affinity.PodAntiAffinity != nil

	nodes := []*v1.Node{}
	for _, nodeInfo := range nodeNameToInfo {
		node := nodeInfo.Node()
		if node == nil {
			continue
		}
		nodes = append(nodes, node)
	}

	// priorityMap stores the mapping from node name to so-far computed score of
	// the node.
	pm := newPodAffinityPriorityMap(nodes)
	allNodeNames := make([]string, 0, len(nodeNameToInfo))
	lazyInit := hasAffinityConstraints || hasAntiAffinityConstraints

	for name := range nodeNameToInfo {
		allNodeNames = append(allNodeNames, name)
		// if pod has affinity defined, or target node has affinityPods
		if lazyInit || len(nodeNameToInfo[name].PodsWithAffinity()) != 0 {
			pm.counts[name] = new(int64)
		}
	}

	// convert the topology key based weights to the node name based weights
	var maxCount, minCount int64

	processPod := func(existingPod *v1.Pod) error {
		existingPodNode, err := ipa.info.GetNodeInfo(existingPod.Spec.NodeName)
		if err != nil {
			if apierrors.IsNotFound(err) {
				klog.Errorf("Node not found, %v", existingPod.Spec.NodeName)
				return nil
			}
			return err
		}
		existingPodAffinity := existingPod.Spec.Affinity
		existingHasAffinityConstraints := existingPodAffinity != nil && existingPodAffinity.PodAffinity != nil
		existingHasAntiAffinityConstraints := existingPodAffinity != nil && existingPodAffinity.PodAntiAffinity != nil

		if hasAffinityConstraints {
			// For every soft pod affinity term of <pod>, if <existingPod> matches the term,
			// increment <pm.counts> for every node in the cluster with the same <term.TopologyKey>
			// value as that of <existingPods>`s node by the term`s weight.
			terms := affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			pm.processTerms(terms, pod, existingPod, existingPodNode, 1)
		}
		if hasAntiAffinityConstraints {
			// For every soft pod anti-affinity term of <pod>, if <existingPod> matches the term,
			// decrement <pm.counts> for every node in the cluster with the same <term.TopologyKey>
			// value as that of <existingPod>`s node by the term`s weight.
			terms := affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			pm.processTerms(terms, pod, existingPod, existingPodNode, -1)
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
					pm.processTerm(&term, existingPod, pod, existingPodNode, int64(ipa.hardPodAffinityWeight))
				}
			}
			// For every soft pod affinity term of <existingPod>, if <pod> matches the term,
			// increment <pm.counts> for every node in the cluster with the same <term.TopologyKey>
			// value as that of <existingPod>'s node by the term's weight.
			terms := existingPodAffinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			pm.processTerms(terms, existingPod, pod, existingPodNode, 1)
		}
		if existingHasAntiAffinityConstraints {
			// For every soft pod anti-affinity term of <existingPod>, if <pod> matches the term,
			// decrement <pm.counts> for every node in the cluster with the same <term.TopologyKey>
			// value as that of <existingPod>'s node by the term's weight.
			terms := existingPodAffinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			pm.processTerms(terms, existingPod, pod, existingPodNode, -1)
		}
		return nil
	}
	processNode := func(i int) {
		nodeInfo := nodeNameToInfo[allNodeNames[i]]
		if nodeInfo.Node() != nil {
			if hasAffinityConstraints || hasAntiAffinityConstraints {
				// We need to process all the pods.
				for _, existingPod := range nodeInfo.Pods() {
					if err := processPod(existingPod); err != nil {
						pm.setError(err)
					}
				}
			} else {
				// The pod doesn't have any constraints - we need to check only existing
				// ones that have some.
				for _, existingPod := range nodeInfo.PodsWithAffinity() {
					if err := processPod(existingPod); err != nil {
						pm.setError(err)
					}
				}
			}
		}
	}
	workqueue.ParallelizeUntil(context.TODO(), 16, len(allNodeNames), processNode)
	if pm.firstError != nil {
		return pm.firstError
	}

	for _, node := range nodes {
		if pm.counts[node.Name] == nil {
			continue
		}
		if *pm.counts[node.Name] > maxCount {
			maxCount = *pm.counts[node.Name]
		}
		if *pm.counts[node.Name] < minCount {
			minCount = *pm.counts[node.Name]
		}
	}

	// calculate final priority score for each node
	maxMinDiff := maxCount - minCount
	for i, hostPriority := range result {
		fScore := float64(0)
		if maxMinDiff > 0 && pm.counts[hostPriority.Host] != nil {
			fScore = float64(schedulerapi.MaxPriority) * (float64(*pm.counts[hostPriority.Host]-minCount) / float64(maxCount-minCount))
		}

		result[i].Host = hostPriority.Host
		result[i].Score = int(fScore)
		if klog.V(10) {
			klog.Infof("%v -> %v: InterPodAffinityPriority, Score: (%d)", pod.Name, hostPriority.Host, int(fScore))
		}
		fmt.Println(result, hostPriority.Host, fScore)
	}
	return nil
}
