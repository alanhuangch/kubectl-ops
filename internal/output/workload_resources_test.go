package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	workloadanalysis "github.com/alanhuangch/kubectl-ops/internal/workload"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestWorkloadResourceWritersExposeKindPlanActualAndAffinity(t *testing.T) {
	report := workloadanalysis.Report{
		CapturedAt: time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC), ResourceClass: workloadanalysis.ResourceClassGPU,
		Completeness: workloadanalysis.CompletenessComplete,
		Items: []workloadanalysis.ResourceUsage{{
			Namespace: "production", Kind: workloadanalysis.KindStatefulSet, Workload: "trainer", UID: "workload-uid",
			Pods:   workloadanalysis.PodCounts{Planned: 10, Actual: 8, ActualKnown: true},
			CPU:    workloadanalysis.ResourcePair{Planned: resource.MustParse("10"), Actual: resource.MustParse("8"), ActualKnown: true},
			Memory: workloadanalysis.ResourcePair{Planned: resource.MustParse("20Gi"), Actual: resource.MustParse("16Gi"), ActualKnown: true},
			GPUs:   []workloadanalysis.GPUResourcePair{{Resource: "nvidia.com/gpu", Planned: resource.MustParse("20"), Actual: resource.MustParse("16"), ActualKnown: true}},
			Placement: workloadanalysis.NodePlacement{
				NodeSelector: []workloadanalysis.KeyValue{{Key: "pool", Value: "gpu"}},
				Required:     []workloadanalysis.NodeSelectorTerm{{MatchExpressions: []workloadanalysis.NodeSelectorRequirement{{Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"a", "b"}}}}},
			},
		}},
		Warnings: []string{},
	}
	tableWriter, _ := NewWriter(FormatTable)
	var table bytes.Buffer
	if err := tableWriter.WriteWorkloadResources(&table, report); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"KIND", "WORKLOAD", "StatefulSet", "10/8", "nvidia.com/gpu=20/16", "required: (zone In [a,b])"} {
		if !strings.Contains(table.String(), expected) {
			t.Fatalf("table missing %q:\n%s", expected, table.String())
		}
	}
	jsonWriter, _ := NewWriter(FormatJSON)
	var jsonOutput bytes.Buffer
	if err := jsonWriter.WriteWorkloadResources(&jsonOutput, report); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"resourceClass": "gpu"`, `"kind": "StatefulSet"`, `"workload": "trainer"`, `"actual": "16"`, `"plannedDefinition":`} {
		if !strings.Contains(jsonOutput.String(), expected) {
			t.Fatalf("JSON missing %q:\n%s", expected, jsonOutput.String())
		}
	}
}
