package controller

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const controllerRevisionHashLabel = "controller-revision-hash"

type RolloutPlan struct {
	UpdateRevision   string
	CurrentRevision  string
	ExpectedReplicas int32
	CurrentReplicas  int32
	OutdatedPods     []corev1.Pod
	UpdatedPods      []corev1.Pod
	StatusOnlyDrift  bool
}

func BuildRolloutPlan(sts *appsv1.StatefulSet, pods []corev1.Pod) RolloutPlan {
	plan := RolloutPlan{
		UpdateRevision:   sts.Status.UpdateRevision,
		CurrentRevision:  sts.Status.CurrentRevision,
		ExpectedReplicas: derefInt32(sts.Spec.Replicas),
		CurrentReplicas:  sts.Status.CurrentReplicas,
	}

	if plan.CurrentRevision != "" && plan.UpdateRevision != "" && plan.CurrentRevision != plan.UpdateRevision && plan.CurrentReplicas > 0 {
		for _, pod := range pods {
			ordinal, ok := podOrdinal(&pod)
			if !ok {
				continue
			}
			if int32(ordinal) < plan.CurrentReplicas {
				plan.OutdatedPods = append(plan.OutdatedPods, pod)
			} else {
				plan.UpdatedPods = append(plan.UpdatedPods, pod)
			}
		}

		sortPodsByOrdinal(plan.OutdatedPods)
		sortPodsByOrdinal(plan.UpdatedPods)
		return plan
	}

	for _, pod := range pods {
		if podRevision(&pod) == plan.UpdateRevision {
			plan.UpdatedPods = append(plan.UpdatedPods, pod)
			continue
		}
		plan.OutdatedPods = append(plan.OutdatedPods, pod)
	}

	sortPodsByOrdinal(plan.OutdatedPods)
	sortPodsByOrdinal(plan.UpdatedPods)

	if len(plan.OutdatedPods) == 0 && rolloutStatusDrift(sts) {
		plan.StatusOnlyDrift = true
	}

	return plan
}

func (p RolloutPlan) HasDrift() bool {
	return len(p.OutdatedPods) > 0 || p.StatusOnlyDrift
}

func (p RolloutPlan) NextPodToDelete() *corev1.Pod {
	if len(p.OutdatedPods) == 0 {
		return nil
	}
	next := p.OutdatedPods[len(p.OutdatedPods)-1]
	return &next
}

func rolloutStatusDrift(sts *appsv1.StatefulSet) bool {
	expected := derefInt32(sts.Spec.Replicas)
	if sts.Status.CurrentRevision != sts.Status.UpdateRevision {
		return sts.Status.CurrentReplicas > 0
	}
	return sts.Status.UpdatedReplicas != expected
}

func isManagedStatefulSet(sts *appsv1.StatefulSet) bool {
	return sts.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType
}

func podRevision(pod *corev1.Pod) string {
	return pod.Labels[controllerRevisionHashLabel]
}

func podOrdinal(pod *corev1.Pod) (int, bool) {
	lastDash := strings.LastIndex(pod.Name, "-")
	if lastDash == -1 || lastDash == len(pod.Name)-1 {
		return 0, false
	}
	ordinal, err := strconv.Atoi(pod.Name[lastDash+1:])
	if err != nil {
		return 0, false
	}
	return ordinal, true
}

func sortPodsByOrdinal(pods []corev1.Pod) {
	sort.Slice(pods, func(i, j int) bool {
		left, leftOK := podOrdinal(&pods[i])
		right, rightOK := podOrdinal(&pods[j])
		if !leftOK || !rightOK {
			return pods[i].Name < pods[j].Name
		}
		return left < right
	})
}

func podNames(pods []corev1.Pod) string {
	names := make([]string, 0, len(pods))
	for _, pod := range pods {
		names = append(names, pod.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func rolloutMessage(plan RolloutPlan) string {
	if next := plan.NextPodToDelete(); next != nil {
		return fmt.Sprintf("rollout to revision %q pending; next pod is %s", plan.UpdateRevision, next.Name)
	}
	return fmt.Sprintf("rollout is waiting for StatefulSet status to converge to revision %q", plan.UpdateRevision)
}
