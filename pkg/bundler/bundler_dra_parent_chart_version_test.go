// Copyright (c) 2026, NVIDIA CORPORATION & AFFILIATES.  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bundler

import (
	"testing"

	"github.com/NVIDIA/aicr/pkg/recipe"
)

// TestInjectDRAParentChartVersionValue_PositiveCase pins the happy
// path: gpu-operator + nvidia-dra-driver-gpu both enabled, gpu-operator
// version is normalized (leading 'v' stripped) and written under
// gpu-operator componentValues at the documented key, so the
// synthesized gpu-operator-post chart's manifest template can read it
// at Helm install time as
// `{{ index .Values "gpu-operator" "_aicrParentChartVersion" }}`.
func TestInjectDRAParentChartVersionValue_PositiveCase(t *testing.T) {
	b, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	componentValues := map[string]map[string]any{
		"gpu-operator":          {},
		"nvidia-dra-driver-gpu": {},
	}
	rr := &recipe.RecipeResult{
		ComponentRefs: []recipe.ComponentRef{
			{Name: "gpu-operator", Version: "v26.4.0"},
			{Name: "nvidia-dra-driver-gpu", Version: "25.12.0"},
		},
	}

	b.injectDRAParentChartVersionValue(componentValues, rr)

	got, ok := componentValues["gpu-operator"][draParentChartVersionValueKey].(string)
	if !ok {
		t.Fatalf("expected string value at gpu-operator[%s], got %T (%v)",
			draParentChartVersionValueKey, componentValues["gpu-operator"][draParentChartVersionValueKey],
			componentValues["gpu-operator"])
	}
	// NormalizeVersionWithDefault strips the leading 'v'.
	if got != "26.4.0" {
		t.Errorf("gpu-operator[%s] = %q, want %q (normalized, no leading v)",
			draParentChartVersionValueKey, got, "26.4.0")
	}
}

// TestInjectDRAParentChartVersionValue_DRAComponentDisabled documents
// the looser gating from the second Codex review on PR #1030: the
// rollout-hook manifest is wired into gpu-operator's manifestFiles in
// base.yaml and ships whenever gpu-operator is enabled, regardless of
// whether nvidia-dra-driver-gpu is also enabled. If the injection
// gated on DRA-enabled, a recipe with --set
// nvidia-dra-driver-gpu:enabled=false would emit the hook with
// `<nil>` placeholders for the parent version and produce an invalid
// DNS-1123 Job name. Inject unconditionally when gpu-operator is
// present; the hook script's runtime "no DRA DaemonSet → exit 0"
// branch keeps the Job a no-op when DRA is in fact absent.
func TestInjectDRAParentChartVersionValue_DRAComponentDisabled(t *testing.T) {
	b, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	componentValues := map[string]map[string]any{
		"gpu-operator": {},
	}
	rr := &recipe.RecipeResult{
		ComponentRefs: []recipe.ComponentRef{
			{Name: "gpu-operator", Version: "v26.4.0"},
		},
	}

	b.injectDRAParentChartVersionValue(componentValues, rr)

	got, ok := componentValues["gpu-operator"][draParentChartVersionValueKey].(string)
	if !ok {
		t.Fatalf("expected the parent version to be injected even when DRA is disabled, got %T (%v)",
			componentValues["gpu-operator"][draParentChartVersionValueKey],
			componentValues["gpu-operator"])
	}
	if got != "26.4.0" {
		t.Errorf("gpu-operator[%s] = %q, want %q",
			draParentChartVersionValueKey, got, "26.4.0")
	}
}

// TestInjectDRAParentChartVersionValue_GPUOperatorDisabled is the
// mirror gating case: with no gpu-operator componentRef the helper
// has no version to mirror, so it leaves componentValues untouched.
func TestInjectDRAParentChartVersionValue_GPUOperatorDisabled(t *testing.T) {
	b, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	componentValues := map[string]map[string]any{
		"nvidia-dra-driver-gpu": {},
	}
	rr := &recipe.RecipeResult{
		ComponentRefs: []recipe.ComponentRef{
			{Name: "nvidia-dra-driver-gpu", Version: "25.12.0"},
		},
	}

	b.injectDRAParentChartVersionValue(componentValues, rr)

	if v, ok := componentValues["gpu-operator"]; ok {
		t.Errorf("expected no gpu-operator entry when gpu-operator is disabled, got %v", v)
	}
}

// TestInjectDRAParentChartVersionValue_OverridesUserSet documents the
// "internal key always reflects the resolved version" invariant. A
// user --set that wrote a stale value into the synthetic key must be
// overwritten by the bundler-derived value; otherwise the
// rollout-trigger semantics break exactly the same way the manual
// annotation in #973 did, and we lose the durability guarantee.
// Injection runs AFTER extractComponentValues specifically for this.
func TestInjectDRAParentChartVersionValue_OverridesUserSet(t *testing.T) {
	b, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	componentValues := map[string]map[string]any{
		"gpu-operator": {
			draParentChartVersionValueKey: "v25.10.1-stale-from-user-set",
		},
		"nvidia-dra-driver-gpu": {},
	}
	rr := &recipe.RecipeResult{
		ComponentRefs: []recipe.ComponentRef{
			{Name: "gpu-operator", Version: "v26.4.0"},
			{Name: "nvidia-dra-driver-gpu", Version: "25.12.0"},
		},
	}

	b.injectDRAParentChartVersionValue(componentValues, rr)

	got := componentValues["gpu-operator"][draParentChartVersionValueKey]
	if got != "26.4.0" {
		t.Errorf("user --set value should be overridden by the injected resolved version: got %v, want \"26.4.0\"", got)
	}
}

// TestInjectDRAParentChartVersionValue_NilInputs documents the
// nil-tolerant contract. Make calls this after extractComponentValues
// returns a non-nil map and almost never with a nil recipe, but
// defensive nil-handling makes the helper safe in unit tests and
// future callers.
func TestInjectDRAParentChartVersionValue_NilInputs(t *testing.T) {
	b, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	tests := []struct {
		name string
		cv   map[string]map[string]any
		rr   *recipe.RecipeResult
	}{
		{"nil componentValues", nil, &recipe.RecipeResult{}},
		{"nil recipe result", map[string]map[string]any{}, nil},
		{"both nil", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic.
			b.injectDRAParentChartVersionValue(tt.cv, tt.rr)
		})
	}
}
