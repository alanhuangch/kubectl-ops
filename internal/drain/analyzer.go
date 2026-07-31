package drain

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type analyzedPod struct {
	item      *corev1.Pod
	impact    PodImpact
	pdbTarget bool
	pdbExempt bool
}

func Analyze(node string, pods []corev1.Pod, pdbs []policyv1.PodDisruptionBudget, options Options) Report {
	report := Report{
		CapturedAt:   options.Now,
		Node:         node,
		Drainability: DrainabilityReady,
		Completeness: CompletenessComplete,
		Options: AppliedOptions{
			IgnoreDaemonSets:   options.IgnoreDaemonSets,
			Force:              options.Force,
			DeleteEmptyDirData: options.DeleteEmptyDirData,
		},
	}

	analyzed := make([]analyzedPod, 0, len(pods))
	for index := range pods {
		pod := &pods[index]
		if pod.Spec.NodeName != node {
			continue
		}
		entry := classifyPod(pod, options)
		analyzed = append(analyzed, entry)
	}
	sort.Slice(analyzed, func(i, j int) bool {
		if analyzed[i].impact.Namespace != analyzed[j].impact.Namespace {
			return analyzed[i].impact.Namespace < analyzed[j].impact.Namespace
		}
		if analyzed[i].impact.Pod != analyzed[j].impact.Pod {
			return analyzed[i].impact.Pod < analyzed[j].impact.Pod
		}
		return analyzed[i].impact.UID < analyzed[j].impact.UID
	})

	if !options.PDBsKnown {
		report.Completeness = CompletenessPartial
		report.Warnings = append(report.Warnings, "PodDisruptionBudget analysis is unavailable because PodDisruptionBudgets could not be listed.")
	} else {
		evaluatePDBs(&report, analyzed, pdbs)
	}

	for _, entry := range analyzed {
		report.Pods = append(report.Pods, entry.impact)
		switch entry.impact.Action {
		case PodActionEvict:
			report.Summary.Evict++
		case PodActionDelete:
			report.Summary.Delete++
		case PodActionSkip:
			report.Summary.Skip++
		case PodActionBlocked:
			report.Summary.Blocked++
		}
	}
	report.Summary.Pods = len(report.Pods)

	if report.Summary.Blocked > 0 {
		report.Drainability = DrainabilityBlocked
	} else if report.Completeness == CompletenessPartial {
		report.Drainability = DrainabilityUnknown
	}
	return report
}

func classifyPod(pod *corev1.Pod, options Options) analyzedPod {
	owner, daemonSet := podOwner(pod)
	result := analyzedPod{
		item: pod,
		impact: PodImpact{
			Namespace: pod.Namespace,
			Pod:       pod.Name,
			UID:       string(pod.UID),
			Owner:     owner,
			Phase:     string(pod.Status.Phase),
			Action:    PodActionEvict,
		},
	}

	if isMirrorPod(pod) {
		result.impact.Action = PodActionSkip
		result.impact.Impacts = append(result.impact.Impacts, ImpactMirrorPod)
		return result
	}
	if daemonSet {
		result.impact.Impacts = append(result.impact.Impacts, ImpactDaemonSetManaged)
		if options.IgnoreDaemonSets {
			result.impact.Action = PodActionSkip
		} else {
			result.impact.Action = PodActionBlocked
			result.impact.Blockers = append(result.impact.Blockers, ImpactDaemonSetManaged)
		}
		return result
	}
	if isTerminalPod(pod) {
		result.impact.Action = PodActionDelete
		result.impact.Impacts = append(result.impact.Impacts, ImpactTerminalPod)
		return result
	}

	result.pdbTarget = true
	result.pdbExempt = isUnhealthyRunningPod(pod)
	if owner == "<none>" {
		result.impact.Impacts = append(result.impact.Impacts, ImpactUnmanagedPod)
		if !options.Force {
			result.impact.Blockers = append(result.impact.Blockers, ImpactUnmanagedPod)
		}
	}
	if usesEmptyDir(pod) {
		result.impact.Impacts = append(result.impact.Impacts, ImpactLocalStorage)
		if !options.DeleteEmptyDirData {
			result.impact.Blockers = append(result.impact.Blockers, ImpactLocalStorage)
		}
	}
	if len(result.impact.Blockers) > 0 {
		result.impact.Action = PodActionBlocked
	}
	return result
}

func evaluatePDBs(report *Report, pods []analyzedPod, pdbs []policyv1.PodDisruptionBudget) {
	ordered := append([]policyv1.PodDisruptionBudget(nil), pdbs...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Namespace != ordered[j].Namespace {
			return ordered[i].Namespace < ordered[j].Namespace
		}
		return ordered[i].Name < ordered[j].Name
	})

	for index := range ordered {
		pdb := &ordered[index]
		impact := PDBImpact{
			Namespace:          pdb.Namespace,
			Name:               pdb.Name,
			DisruptionsAllowed: pdb.Status.DisruptionsAllowed,
			SelectorValid:      true,
		}
		selector, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
		if err != nil {
			impact.SelectorValid = false
			report.Completeness = CompletenessPartial
			report.Warnings = append(report.Warnings, fmt.Sprintf("PodDisruptionBudget %s/%s has an invalid selector: %v", pdb.Namespace, pdb.Name, err))
			report.PDBs = append(report.PDBs, impact)
			continue
		}

		matchedIndexes := make([]int, 0)
		for podIndex := range pods {
			pod := &pods[podIndex]
			if !pod.pdbTarget || pod.item.Namespace != pdb.Namespace || !selector.Matches(labels.Set(pod.item.Labels)) {
				continue
			}
			impact.MatchedTargets++
			if pdb.Spec.UnhealthyPodEvictionPolicy != nil && *pdb.Spec.UnhealthyPodEvictionPolicy == policyv1.AlwaysAllow && pod.pdbExempt {
				impact.UnhealthyExempt++
				continue
			}
			matchedIndexes = append(matchedIndexes, podIndex)
		}

		impact.Blocked = len(matchedIndexes) > 0 && pdb.Status.DisruptionsAllowed == 0
		if impact.Blocked {
			for _, podIndex := range matchedIndexes {
				addBlocker(&pods[podIndex].impact, ImpactPDBViolation)
			}
		}
		report.PDBs = append(report.PDBs, impact)
	}
}

func addBlocker(impact *PodImpact, code ImpactCode) {
	for _, existing := range impact.Blockers {
		if existing == code {
			return
		}
	}
	impact.Impacts = append(impact.Impacts, code)
	impact.Blockers = append(impact.Blockers, code)
	impact.Action = PodActionBlocked
}

func isMirrorPod(pod *corev1.Pod) bool {
	_, found := pod.Annotations[corev1.MirrorPodAnnotationKey]
	return found
}

func isTerminalPod(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}

func isUnhealthyRunningPod(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status != corev1.ConditionTrue
		}
	}
	return true
}

func usesEmptyDir(pod *corev1.Pod) bool {
	for _, volume := range pod.Spec.Volumes {
		if volume.EmptyDir != nil {
			return true
		}
	}
	return false
}

func podOwner(pod *corev1.Pod) (string, bool) {
	owner := metav1.GetControllerOf(pod)
	if owner == nil {
		return "<none>", false
	}
	return owner.Kind + "/" + owner.Name, owner.Kind == "DaemonSet"
}
