package controller

import (
	"context"
	"fmt"
	"path"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
)

type inputReadiness struct {
	SecretsReady   bool
	SecretsReason  string
	SecretsMessage string

	TLSReady   bool
	TLSReason  string
	TLSMessage string
}

type secretRequirement struct {
	Name         string
	RequiredKeys map[string]struct{}
}

func defaultInputReadiness() inputReadiness {
	return inputReadiness{
		SecretsReady:   true,
		SecretsReason:  "SecretsReady",
		SecretsMessage: "All referenced Secret inputs are present and structurally usable",
		TLSReady:       true,
		TLSReason:      "TLSMaterialReady",
		TLSMessage:     "TLS Secret inputs are present and structurally usable",
	}
}

func (i inputReadiness) Ready() bool {
	return i.SecretsReady && i.TLSReady
}

func (i inputReadiness) BlockingMessage() string {
	if !i.TLSReady {
		return i.TLSMessage
	}
	if !i.SecretsReady {
		return i.SecretsMessage
	}
	return "All referenced Secret inputs are ready"
}

func (r *NiFiClusterReconciler) evaluateInputReadiness(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) (inputReadiness, error) {
	readiness := defaultInputReadiness()

	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}

	requirements := collectSecretRequirements(cluster, target)
	secrets := make(map[string]*corev1.Secret, len(requirements))
	loadSecret := func(name string) (*corev1.Secret, bool, error) {
		if secret, ok := secrets[name]; ok {
			return secret, true, nil
		}

		secret := &corev1.Secret{}
		if err := reader.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, secret); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("get secret %q: %w", name, err)
		}
		secrets[name] = secret
		return secret, true, nil
	}

	for _, requirement := range requirements {
		if requirement.Name == "" {
			continue
		}

		secret, found, err := loadSecret(requirement.Name)
		if err != nil {
			return readiness, err
		}
		if !found {
			if readiness.SecretsReady {
				readiness.SecretsReady = false
				readiness.SecretsReason = "SecretMissing"
				readiness.SecretsMessage = fmt.Sprintf("Secret %q is referenced by the managed NiFi inputs but does not exist", requirement.Name)
			}
			continue
		}

		keys := sortedKeys(requirement.RequiredKeys)
		for _, key := range keys {
			if len(secret.Data[key]) > 0 {
				continue
			}
			if readiness.SecretsReady {
				readiness.SecretsReady = false
				readiness.SecretsReason = "SecretKeyMissing"
				readiness.SecretsMessage = fmt.Sprintf("Secret %q is missing required key %q", requirement.Name, key)
			}
			break
		}
	}

	tlsSecretName, err := findTLSSecretName(target.Spec.Template.Spec.Volumes)
	if err != nil {
		readiness.TLSReady = false
		readiness.TLSReason = "TLSSecretReferenceMissing"
		readiness.TLSMessage = "Target StatefulSet does not define a TLS Secret volume"
		return readiness, nil
	}

	tlsSecret, found, err := loadSecret(tlsSecretName)
	if err != nil {
		return readiness, err
	}
	if !found {
		readiness.TLSReady = false
		readiness.TLSReason = "TLSSecretMissing"
		readiness.TLSMessage = fmt.Sprintf("TLS Secret %q was not found", tlsSecretName)
		return readiness, nil
	}

	requiredTLSKeys := requiredTLSSecretKeys(target)
	for _, key := range requiredTLSKeys {
		if len(tlsSecret.Data[key]) > 0 {
			continue
		}
		readiness.TLSReady = false
		readiness.TLSReason = "TLSSecretKeyMissing"
		readiness.TLSMessage = fmt.Sprintf("TLS Secret %q is missing required key %q", tlsSecretName, key)
		return readiness, nil
	}

	return readiness, nil
}

func collectSecretRequirements(cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) []secretRequirement {
	requirements := map[string]map[string]struct{}{}
	addRequirement := func(name string, keys ...string) {
		if name == "" {
			return
		}
		if requirements[name] == nil {
			requirements[name] = map[string]struct{}{}
		}
		for _, key := range keys {
			if key == "" {
				continue
			}
			requirements[name][key] = struct{}{}
		}
	}

	for _, ref := range cluster.Spec.RestartTriggers.Secrets {
		addRequirement(ref.Name)
	}

	for _, volume := range target.Spec.Template.Spec.Volumes {
		if volume.Secret != nil {
			keys := make([]string, 0, len(volume.Secret.Items))
			for _, item := range volume.Secret.Items {
				keys = append(keys, item.Key)
			}
			addRequirement(volume.Secret.SecretName, keys...)
		}
		if volume.Projected == nil {
			continue
		}
		for _, source := range volume.Projected.Sources {
			if source.Secret == nil {
				continue
			}
			keys := make([]string, 0, len(source.Secret.Items))
			for _, item := range source.Secret.Items {
				keys = append(keys, item.Key)
			}
			addRequirement(source.Secret.Name, keys...)
		}
	}

	collectFromContainerSet := func(containers []corev1.Container) {
		for _, container := range containers {
			for _, envVar := range container.Env {
				if envVar.ValueFrom != nil && envVar.ValueFrom.SecretKeyRef != nil {
					addRequirement(envVar.ValueFrom.SecretKeyRef.Name, envVar.ValueFrom.SecretKeyRef.Key)
				}
			}
			for _, envFrom := range container.EnvFrom {
				if envFrom.SecretRef != nil {
					addRequirement(envFrom.SecretRef.Name)
				}
			}
		}
	}

	collectFromContainerSet(target.Spec.Template.Spec.InitContainers)
	collectFromContainerSet(target.Spec.Template.Spec.Containers)

	names := make([]string, 0, len(requirements))
	for name := range requirements {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]secretRequirement, 0, len(names))
	for _, name := range names {
		result = append(result, secretRequirement{
			Name:         name,
			RequiredKeys: requirements[name],
		})
	}

	return result
}

func requiredTLSSecretKeys(target *appsv1.StatefulSet) []string {
	required := map[string]struct{}{
		defaultCACertKey: {},
	}

	initContainer, err := findContainer(target.Spec.Template.Spec.InitContainers, "init-conf")
	if err == nil {
		for _, envName := range []string{"KEYSTORE_PASSWORD", "TRUSTSTORE_PASSWORD"} {
			secretRef, refErr := findSecretKeyRef(initContainer.Env, envName)
			if refErr == nil {
				required[secretRef.Key] = struct{}{}
			}
		}
		for _, propertyName := range []string{"nifi.security.keystore", "nifi.security.truststore"} {
			if value, ok := extractReplacePropertyValue(initContainer, propertyName); ok {
				required[path.Base(value)] = struct{}{}
			}
		}
	}

	return sortedKeys(required)
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (r *NiFiClusterReconciler) applyInputReadiness(cluster *platformv1alpha1.NiFiCluster, readiness inputReadiness) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionSecretsReady,
		Status:             conditionStatus(readiness.SecretsReady),
		Reason:             readiness.SecretsReason,
		Message:            readiness.SecretsMessage,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTLSReady,
		Status:             conditionStatus(readiness.TLSReady),
		Reason:             readiness.TLSReason,
		Message:            readiness.TLSMessage,
		LastTransitionTime: metav1.Now(),
	})
}

func (r *NiFiClusterReconciler) markInputReadinessUnknown(cluster *platformv1alpha1.NiFiCluster, reason, message string) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionSecretsReady,
		Status:             metav1.ConditionUnknown,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTLSReady,
		Status:             metav1.ConditionUnknown,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

func (r *NiFiClusterReconciler) markInputReadinessBlocked(cluster *platformv1alpha1.NiFiCluster, readiness inputReadiness) {
	r.applyInputReadiness(cluster, readiness)
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "InputReadinessPending",
		Message:            "Managed running-state orchestration is waiting for referenced Secret inputs",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "WaitingForInputReadiness",
		Message:            readiness.BlockingMessage(),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "InputReadinessPending",
		Message:            "No rollout failure is active while waiting for referenced Secret inputs",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "Running",
		Message:            "Hibernation is not active",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation("InputReadiness", readiness.BlockingMessage())
}

func conditionStatus(ready bool) metav1.ConditionStatus {
	if ready {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}
