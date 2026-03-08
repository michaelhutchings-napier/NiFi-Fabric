package controller

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	platformv1alpha1 "github.com/michaelhutchings-napier/nifi-made-simple/api/v1alpha1"
)

const controllerRevisionHashLabel = "controller-revision-hash"

type RolloutPlan struct {
	UpdateRevision   string
	CurrentRevision  string
	ExpectedReplicas int32
	CurrentReplicas  int32
	Trigger          platformv1alpha1.RolloutTrigger
	OutdatedPods     []corev1.Pod
	UpdatedPods      []corev1.Pod
	StatusOnlyDrift  bool
}

func BuildRolloutPlan(sts *appsv1.StatefulSet, pods []corev1.Pod, rolloutStatus platformv1alpha1.RolloutStatus) RolloutPlan {
	plan := RolloutPlan{
		UpdateRevision:   sts.Status.UpdateRevision,
		CurrentRevision:  sts.Status.CurrentRevision,
		ExpectedReplicas: derefInt32(sts.Spec.Replicas),
		CurrentReplicas:  sts.Status.CurrentReplicas,
	}

	if planUsesRestartTimestamp(rolloutStatus.Trigger) && rolloutStatus.StartedAt != nil {
		plan.Trigger = rolloutStatus.Trigger
		startedAt := rolloutStatus.StartedAt.Time
		for _, pod := range pods {
			if podWasRecreatedAfter(&pod, startedAt) {
				plan.UpdatedPods = append(plan.UpdatedPods, pod)
				continue
			}
			plan.OutdatedPods = append(plan.OutdatedPods, pod)
		}

		sortPodsByOrdinal(plan.OutdatedPods)
		sortPodsByOrdinal(plan.UpdatedPods)
		return plan
	}

	plan.Trigger = platformv1alpha1.RolloutTriggerStatefulSetRevision
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

func highestOrdinalPod(pods []corev1.Pod) (corev1.Pod, bool) {
	if len(pods) == 0 {
		return corev1.Pod{}, false
	}

	ordered := append([]corev1.Pod(nil), pods...)
	sortPodsByOrdinal(ordered)
	return ordered[len(ordered)-1], true
}

func podsPendingTermination(pods []corev1.Pod) bool {
	for i := range pods {
		if pods[i].DeletionTimestamp != nil {
			return true
		}
	}
	return false
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
		if plan.Trigger == platformv1alpha1.RolloutTriggerConfigDrift {
			return fmt.Sprintf("config drift rollout pending; next pod is %s", next.Name)
		}
		if plan.Trigger == platformv1alpha1.RolloutTriggerTLSDrift {
			return fmt.Sprintf("TLS drift rollout pending; next pod is %s", next.Name)
		}
		return fmt.Sprintf("rollout to revision %q pending; next pod is %s", plan.UpdateRevision, next.Name)
	}
	if plan.Trigger == platformv1alpha1.RolloutTriggerConfigDrift {
		return "config drift rollout is waiting for pod and cluster status to converge"
	}
	if plan.Trigger == platformv1alpha1.RolloutTriggerTLSDrift {
		return "TLS drift rollout is waiting for pod and cluster status to converge"
	}
	return fmt.Sprintf("rollout is waiting for StatefulSet status to converge to revision %q", plan.UpdateRevision)
}

func planUsesRestartTimestamp(trigger platformv1alpha1.RolloutTrigger) bool {
	return trigger == platformv1alpha1.RolloutTriggerConfigDrift || trigger == platformv1alpha1.RolloutTriggerTLSDrift
}

func podWasRecreatedAfter(pod *corev1.Pod, startedAt time.Time) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}
	return !pod.CreationTimestamp.Time.Before(startedAt)
}
