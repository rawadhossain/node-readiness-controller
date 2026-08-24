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

package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

func TestBuildInfo(t *testing.T) {
	expected := `
# HELP node_readiness_build_info Build information for the node-readiness-controller binary.
# TYPE node_readiness_build_info gauge
node_readiness_build_info{version="unknown"} 1
`
	if err := testutil.CollectAndCompare(BuildInfo, strings.NewReader(expected), "node_readiness_build_info"); err != nil {
		t.Fatalf("unexpected collecting result:\n%s", err)
	}

	gathered, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	var found bool
	for _, mf := range gathered {
		if mf.GetName() == "node_readiness_build_info" {
			found = true
			if got := mf.GetType().String(); got != "GAUGE" {
				t.Fatalf("expected node_readiness_build_info to be a gauge, got %s", got)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected node_readiness_build_info to be registered with the controller-runtime metrics registry")
	}
}

func TestEnforcementLatency(t *testing.T) {
	EnforcementLatency.Reset()
	t.Cleanup(EnforcementLatency.Reset)
	EnforcementLatency.WithLabelValues("test-rule", string(EnforcementOperationAdd)).Observe(0.2)

	expected := `
# HELP node_readiness_enforcement_latency_seconds End-to-end latency from node condition change to taint operation completion
# TYPE node_readiness_enforcement_latency_seconds histogram
node_readiness_enforcement_latency_seconds_bucket{operation="add",rule="test-rule",le="0.01"} 0
node_readiness_enforcement_latency_seconds_bucket{operation="add",rule="test-rule",le="0.05"} 0
node_readiness_enforcement_latency_seconds_bucket{operation="add",rule="test-rule",le="0.1"} 0
node_readiness_enforcement_latency_seconds_bucket{operation="add",rule="test-rule",le="0.5"} 1
node_readiness_enforcement_latency_seconds_bucket{operation="add",rule="test-rule",le="1"} 1
node_readiness_enforcement_latency_seconds_bucket{operation="add",rule="test-rule",le="2"} 1
node_readiness_enforcement_latency_seconds_bucket{operation="add",rule="test-rule",le="5"} 1
node_readiness_enforcement_latency_seconds_bucket{operation="add",rule="test-rule",le="10"} 1
node_readiness_enforcement_latency_seconds_bucket{operation="add",rule="test-rule",le="30"} 1
node_readiness_enforcement_latency_seconds_bucket{operation="add",rule="test-rule",le="60"} 1
node_readiness_enforcement_latency_seconds_bucket{operation="add",rule="test-rule",le="120"} 1
node_readiness_enforcement_latency_seconds_bucket{operation="add",rule="test-rule",le="300"} 1
node_readiness_enforcement_latency_seconds_bucket{operation="add",rule="test-rule",le="+Inf"} 1
node_readiness_enforcement_latency_seconds_sum{operation="add",rule="test-rule"} 0.2
node_readiness_enforcement_latency_seconds_count{operation="add",rule="test-rule"} 1
`
	assertObservationReflected(t, EnforcementLatency, "node_readiness_enforcement_latency_seconds", expected)

	assertMetricRegistered(t, metrics.Registry,
		"node_readiness_enforcement_latency_seconds",
		"HISTOGRAM",
		"End-to-end latency from node condition change to taint operation completion")
}

// assertMetricRegistered checks that the metric is registered correctly.
func assertMetricRegistered(t *testing.T, registry prometheus.Gatherer, name, wantType, wantHelp string) {
	t.Helper()

	gathered, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	for _, mf := range gathered {
		if mf.GetName() != name {
			continue
		}
		if got := mf.GetType().String(); got != wantType {
			t.Fatalf("expected %s to be a %s, got %s", name, wantType, got)
		}
		if got := mf.GetHelp(); got != wantHelp {
			t.Fatalf("unexpected help text for %s: got %q, want %q", name, got, wantHelp)
		}
		return
	}
	t.Fatalf("expected %s to be registered with the controller-runtime metrics registry", name)
}

// assertObservationReflected checks the collected metric.
func assertObservationReflected(t *testing.T, collector prometheus.Collector, name, expectedExposition string) {
	t.Helper()

	if err := testutil.CollectAndCompare(collector, strings.NewReader(expectedExposition), name); err != nil {
		t.Fatalf("unexpected collecting result:\n%s", err)
	}
}
