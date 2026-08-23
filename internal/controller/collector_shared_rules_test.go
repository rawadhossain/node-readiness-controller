/*
Copyright The Kubernetes Authors.

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

package controller

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
	"sigs.k8s.io/node-readiness-controller/internal/metrics"
)

func countRuleListCalls(fc client.WithWatch, onList func(ctx context.Context, c client.WithWatch)) (client.WithWatch, *atomic.Int32) {
	var calls atomic.Int32
	wrapped := interceptor.NewClient(fc, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*readinessv1alpha1.NodeReadinessRuleList); ok {
				calls.Add(1)

				if err := c.List(ctx, list, opts...); err != nil {
					return err
				}
				if onList != nil {
					onList(ctx, c)
				}
				return nil
			}
			return c.List(ctx, list, opts...)
		},
	})
	return wrapped, &calls
}

func collectMetrics(c *metrics.ReadinessCollector) []prometheus.Metric {
	ch := make(chan prometheus.Metric, 32)
	c.Collect(ch)
	close(ch)
	var out []prometheus.Metric
	for m := range ch {
		out = append(out, m)
	}
	return out
}

func TestCollect_ListsRulesExactlyOnce(t *testing.T) {
	g := NewWithT(t)

	rule := gpuRuleWithConditions("GPUDriverReady")
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		rule,
		withNodeConditions(gpuNode("held-1", true), nodeCondition("GPUDriverReady", corev1.ConditionFalse)),
		gpuNode("released-1", false),
	).Build()

	wrapped, ruleListCalls := countRuleListCalls(fc, nil)
	rc := &RuleReadinessController{Client: wrapped}
	collector := metrics.NewReadinessCollector(rc)

	collectMetrics(collector)

	g.Expect(ruleListCalls.Load()).To(Equal(int32(1)),
		"Collect() must list NodeReadinessRuleList exactly once; got %d calls", ruleListCalls.Load())
}

func TestSharedRuleSnapshot_ConcurrentMutationCannotDiverge(t *testing.T) {
	g := NewWithT(t)

	rule := gpuRuleWithConditions("GPUDriverReady")
	fc := fakeclient.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(
		rule,
		withNodeConditions(gpuNode("held-1", true), nodeCondition("GPUDriverReady", corev1.ConditionFalse)),
	).Build()

	wrapped, ruleListCalls := countRuleListCalls(fc, func(ctx context.Context, c client.WithWatch) {
		current := &readinessv1alpha1.NodeReadinessRule{}
		g.Expect(c.Get(ctx, types.NamespacedName{Name: "gpu-ready"}, current)).To(Succeed())
		current.Finalizers = []string{"readiness.node.x-k8s.io/cleanup-taints"}
		g.Expect(c.Update(ctx, current)).To(Succeed())
		g.Expect(c.Delete(ctx, current)).To(Succeed())
	})

	rc := &RuleReadinessController{Client: wrapped}
	collector := metrics.NewReadinessCollector(rc)

	metricsOut := collectMetrics(collector)

	g.Expect(ruleListCalls.Load()).To(Equal(int32(1)))

	var sawRuleNodes, sawBlockedNodes bool
	for _, m := range metricsOut {
		var pb dto.Metric
		g.Expect(m.Write(&pb)).To(Succeed())

		var isGPURule, hasStateLabel, hasConditionLabel bool
		for _, l := range pb.GetLabel() {
			switch l.GetName() {
			case "rule":
				isGPURule = l.GetValue() == "gpu-ready"
			case "state":
				hasStateLabel = true
			case "condition":
				hasConditionLabel = true
			}
		}
		if !isGPURule {
			continue
		}
		if hasStateLabel {
			sawRuleNodes = true
		}
		if hasConditionLabel {
			sawBlockedNodes = true
		}
	}
	g.Expect(sawRuleNodes).To(BeTrue(), "expected gpu-ready to appear in node_readiness_rule_nodes")
	g.Expect(sawBlockedNodes).To(BeTrue(), "expected gpu-ready to still appear in node_readiness_blocked_nodes despite the concurrent deletion racing the fetch")
}
