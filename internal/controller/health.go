package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/michaelhutchings-napier/nifi-made-simple/api/v1alpha1"
	"github.com/michaelhutchings-napier/nifi-made-simple/internal/nifi"
)

const (
	defaultHealthPollInterval    = 5 * time.Second
	defaultStableHealthPollCount = 3
	defaultPodReadyTimeout       = 10 * time.Minute
	defaultClusterHealthTimeout  = 15 * time.Minute
	defaultCACertKey             = "ca.crt"
	nifiContainerName            = "nifi"
)

type ClusterHealthChecker interface {
	WaitForPodsReady(ctx context.Context, sts *appsv1.StatefulSet, timeout time.Duration) error
	WaitForClusterHealthy(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, sts *appsv1.StatefulSet, timeout time.Duration) (ClusterHealthResult, error)
	CheckClusterHealth(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, sts *appsv1.StatefulSet) (ClusterHealthResult, error)
}

type PodHealth struct {
	PodName            string
	Ready              bool
	APIReachable       bool
	Clustered          bool
	ConnectedToCluster bool
	ConnectedNodeCount int32
	TotalNodeCount     int32
	FailureReason      string
}

type ClusterHealthResult struct {
	ExpectedReplicas int32
	ReadyPods        int32
	ReachablePods    int32
	ConvergedPods    int32
	StablePolls      int
	Pods             []PodHealth
}

func (r ClusterHealthResult) Healthy() bool {
	return r.ExpectedReplicas > 0 &&
		r.ReadyPods == r.ExpectedReplicas &&
		r.ReachablePods == r.ExpectedReplicas &&
		r.ConvergedPods == r.ExpectedReplicas
}

func (r ClusterHealthResult) Summary() string {
	parts := []string{
		fmt.Sprintf("ready=%d/%d", r.ReadyPods, r.ExpectedReplicas),
		fmt.Sprintf("api=%d/%d", r.ReachablePods, r.ExpectedReplicas),
		fmt.Sprintf("converged=%d/%d", r.ConvergedPods, r.ExpectedReplicas),
	}
	if len(r.Pods) == 0 {
		return strings.Join(parts, " ")
	}

	podParts := make([]string, 0, len(r.Pods))
	for _, pod := range r.Pods {
		podParts = append(podParts, fmt.Sprintf("%s[ready=%t api=%t connected=%t %d/%d reason=%s]",
			pod.PodName,
			pod.Ready,
			pod.APIReachable,
			pod.ConnectedToCluster,
			pod.ConnectedNodeCount,
			pod.TotalNodeCount,
			emptyIfUnset(pod.FailureReason, "none"),
		))
	}

	return strings.Join(append(parts, strings.Join(podParts, " ")), " ")
}

type LiveClusterHealthChecker struct {
	KubeClient   client.Client
	NiFiClient   nifi.Client
	PollInterval time.Duration
	StablePolls  int
}

func (c *LiveClusterHealthChecker) WaitForPodsReady(ctx context.Context, sts *appsv1.StatefulSet, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultPodReadyTimeout
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		pods, err := c.listTargetPods(deadlineCtx, sts)
		if err != nil {
			return err
		}
		if allExpectedPodsReady(derefInt32(sts.Spec.Replicas), pods) {
			return nil
		}
		if deadlineCtx.Err() != nil {
			return fmt.Errorf("timed out waiting for target pods to become Ready")
		}
		if err := sleepWithContext(deadlineCtx, c.pollInterval()); err != nil {
			return fmt.Errorf("timed out waiting for target pods to become Ready")
		}
	}
}

func (c *LiveClusterHealthChecker) WaitForClusterHealthy(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, sts *appsv1.StatefulSet, timeout time.Duration) (ClusterHealthResult, error) {
	if timeout <= 0 {
		timeout = defaultClusterHealthTimeout
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stablePolls := 0
	lastResult := ClusterHealthResult{ExpectedReplicas: derefInt32(sts.Spec.Replicas)}

	for {
		result, err := c.checkOnce(deadlineCtx, sts)
		lastResult = result
		if err == nil && result.Healthy() {
			stablePolls++
			lastResult.StablePolls = stablePolls
			if stablePolls >= c.requiredStablePolls() {
				return lastResult, nil
			}
		} else {
			stablePolls = 0
			lastResult.StablePolls = 0
		}

		if deadlineCtx.Err() != nil {
			return lastResult, fmt.Errorf("timed out waiting for stable cluster health: %s", lastResult.Summary())
		}
		if err := sleepWithContext(deadlineCtx, c.pollInterval()); err != nil {
			return lastResult, fmt.Errorf("timed out waiting for stable cluster health: %s", lastResult.Summary())
		}
	}
}

func (c *LiveClusterHealthChecker) CheckClusterHealth(ctx context.Context, _ *platformv1alpha1.NiFiCluster, sts *appsv1.StatefulSet) (ClusterHealthResult, error) {
	return c.checkOnce(ctx, sts)
}

func (c *LiveClusterHealthChecker) checkOnce(ctx context.Context, sts *appsv1.StatefulSet) (ClusterHealthResult, error) {
	result := ClusterHealthResult{
		ExpectedReplicas: derefInt32(sts.Spec.Replicas),
	}

	pods, err := c.listTargetPods(ctx, sts)
	if err != nil {
		return result, err
	}
	authConfig, err := c.resolveAuthConfig(ctx, sts)
	if err != nil {
		return result, err
	}
	caCertPEM, err := c.resolveCACert(ctx, sts)
	if err != nil {
		return result, err
	}

	sort.Slice(pods, func(i, j int) bool {
		leftOrdinal, _ := podOrdinal(&pods[i])
		rightOrdinal, _ := podOrdinal(&pods[j])
		return leftOrdinal < rightOrdinal
	})

	podByName := make(map[string]corev1.Pod, len(pods))
	for _, pod := range pods {
		podByName[pod.Name] = pod
	}

	for ordinal := int32(0); ordinal < result.ExpectedReplicas; ordinal++ {
		podName := fmt.Sprintf("%s-%d", sts.Name, ordinal)
		podHealth := PodHealth{PodName: podName}

		pod, found := podByName[podName]
		if !found {
			podHealth.FailureReason = "pod-missing"
			result.Pods = append(result.Pods, podHealth)
			continue
		}

		podHealth.Ready = isPodReady(&pod)
		if podHealth.Ready {
			result.ReadyPods++
		}

		summary, summaryErr := c.NiFiClient.GetClusterSummary(ctx, nifi.ClusterSummaryRequest{
			BaseURL:   podBaseURL(sts, &pod),
			Username:  authConfig.Username,
			Password:  authConfig.Password,
			CACertPEM: caCertPEM,
		})
		if summaryErr != nil {
			podHealth.FailureReason = truncateError(summaryErr)
			result.Pods = append(result.Pods, podHealth)
			continue
		}

		podHealth.APIReachable = true
		podHealth.Clustered = summary.Clustered
		podHealth.ConnectedToCluster = summary.ConnectedToCluster
		podHealth.ConnectedNodeCount = summary.ConnectedNodeCount
		podHealth.TotalNodeCount = summary.TotalNodeCount
		result.ReachablePods++

		if summary.Healthy(result.ExpectedReplicas) {
			result.ConvergedPods++
		}

		result.Pods = append(result.Pods, podHealth)
	}

	if result.Healthy() {
		return result, nil
	}

	return result, fmt.Errorf("cluster health gate not yet satisfied: %s", result.Summary())
}

func (c *LiveClusterHealthChecker) resolveAuthConfig(ctx context.Context, sts *appsv1.StatefulSet) (authConfig, error) {
	container, err := findContainer(sts.Spec.Template.Spec.Containers, nifiContainerName)
	if err != nil {
		return authConfig{}, err
	}

	usernameRef, err := findSecretKeyRef(container.Env, "SINGLE_USER_CREDENTIALS_USERNAME")
	if err != nil {
		return authConfig{}, err
	}
	passwordRef, err := findSecretKeyRef(container.Env, "SINGLE_USER_CREDENTIALS_PASSWORD")
	if err != nil {
		return authConfig{}, err
	}

	usernameSecret := &corev1.Secret{}
	if err := c.KubeClient.Get(ctx, client.ObjectKey{Namespace: sts.Namespace, Name: usernameRef.Name}, usernameSecret); err != nil {
		return authConfig{}, fmt.Errorf("get username secret %q: %w", usernameRef.Name, err)
	}
	passwordSecret := usernameSecret
	if usernameRef.Name != passwordRef.Name {
		passwordSecret = &corev1.Secret{}
		if err := c.KubeClient.Get(ctx, client.ObjectKey{Namespace: sts.Namespace, Name: passwordRef.Name}, passwordSecret); err != nil {
			return authConfig{}, fmt.Errorf("get password secret %q: %w", passwordRef.Name, err)
		}
	}

	username := string(usernameSecret.Data[usernameRef.Key])
	password := string(passwordSecret.Data[passwordRef.Key])
	if username == "" || password == "" {
		return authConfig{}, fmt.Errorf("auth secret data is incomplete")
	}

	return authConfig{Username: username, Password: password}, nil
}

func (c *LiveClusterHealthChecker) resolveCACert(ctx context.Context, sts *appsv1.StatefulSet) ([]byte, error) {
	secretName, err := findTLSSecretName(sts.Spec.Template.Spec.Volumes)
	if err != nil {
		return nil, err
	}

	secret := &corev1.Secret{}
	if err := c.KubeClient.Get(ctx, client.ObjectKey{Namespace: sts.Namespace, Name: secretName}, secret); err != nil {
		return nil, fmt.Errorf("get TLS secret %q: %w", secretName, err)
	}

	caCert := secret.Data[defaultCACertKey]
	if len(caCert) == 0 {
		return nil, fmt.Errorf("TLS secret %q does not contain key %q", secretName, defaultCACertKey)
	}

	return caCert, nil
}

func (c *LiveClusterHealthChecker) listTargetPods(ctx context.Context, sts *appsv1.StatefulSet) ([]corev1.Pod, error) {
	if sts.Spec.Selector == nil {
		return nil, fmt.Errorf("target StatefulSet %q does not define a selector", sts.Name)
	}

	podList := &corev1.PodList{}
	if err := c.KubeClient.List(ctx, podList,
		client.InNamespace(sts.Namespace),
		client.MatchingLabels(sts.Spec.Selector.MatchLabels),
	); err != nil {
		return nil, fmt.Errorf("list target pods: %w", err)
	}

	return podList.Items, nil
}

func (c *LiveClusterHealthChecker) requiredStablePolls() int {
	if c.StablePolls <= 0 {
		return defaultStableHealthPollCount
	}
	return c.StablePolls
}

func (c *LiveClusterHealthChecker) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultHealthPollInterval
	}
	return c.PollInterval
}

type authConfig struct {
	Username string
	Password string
}

func findContainer(containers []corev1.Container, name string) (corev1.Container, error) {
	for _, container := range containers {
		if container.Name == name {
			return container, nil
		}
	}
	return corev1.Container{}, fmt.Errorf("container %q was not found", name)
}

func findSecretKeyRef(envVars []corev1.EnvVar, name string) (corev1.SecretKeySelector, error) {
	for _, envVar := range envVars {
		if envVar.Name == name && envVar.ValueFrom != nil && envVar.ValueFrom.SecretKeyRef != nil {
			return *envVar.ValueFrom.SecretKeyRef, nil
		}
	}
	return corev1.SecretKeySelector{}, fmt.Errorf("secretKeyRef env %q was not found", name)
}

func findTLSSecretName(volumes []corev1.Volume) (string, error) {
	for _, volume := range volumes {
		if volume.Name == "tls" && volume.Secret != nil && volume.Secret.SecretName != "" {
			return volume.Secret.SecretName, nil
		}
	}
	for _, volume := range volumes {
		if volume.Secret != nil && volume.Secret.SecretName != "" {
			return volume.Secret.SecretName, nil
		}
	}
	return "", fmt.Errorf("TLS secret volume was not found")
}

func allExpectedPodsReady(expectedReplicas int32, pods []corev1.Pod) bool {
	if int32(len(pods)) != expectedReplicas {
		return false
	}
	for i := range pods {
		if !isPodReady(&pods[i]) || pods[i].DeletionTimestamp != nil {
			return false
		}
	}
	return true
}

func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func podBaseURL(sts *appsv1.StatefulSet, pod *corev1.Pod) string {
	return fmt.Sprintf("https://%s.%s.%s.svc.cluster.local:8443", pod.Name, sts.Spec.ServiceName, sts.Namespace)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func truncateError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 120 {
		return message[:120]
	}
	return message
}

func emptyIfUnset(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
