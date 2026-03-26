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

type secretLoader func(name string) (*corev1.Secret, bool, error)

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

	tlsSecretName, err := findTLSSecretName(target.Spec.Template.Spec.Volumes)
	if err != nil {
		readiness.TLSReady = false
		readiness.TLSReason = "TLSSecretReferenceMissing"
		readiness.TLSMessage = "Target StatefulSet does not define a TLS Secret volume"
		return readiness, nil
	}

	requirements := collectSecretRequirements(cluster, target, tlsSecretName)
	secrets := make(map[string]*corev1.Secret, len(requirements)+2)
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

	if reason, message, err := validateSecretRequirements(loadSecret, requirements); err != nil {
		return readiness, err
	} else if reason != "" {
		readiness.SecretsReady = false
		readiness.SecretsReason = reason
		readiness.SecretsMessage = message
	}

	if reason, message, handled, err := validateSingleUserSecretRequirements(loadSecret, target); err != nil {
		return readiness, err
	} else if handled {
		readiness.SecretsReady = false
		readiness.SecretsReason = reason
		readiness.SecretsMessage = message
	}

	if reason, message, handled, err := validateTLSContractRequirements(loadSecret, target, tlsSecretName); err != nil {
		return readiness, err
	} else if handled {
		readiness.TLSReady = false
		readiness.TLSReason = reason
		readiness.TLSMessage = message
	}

	return readiness, nil
}

func collectSecretRequirements(cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, tlsSecretName string) []secretRequirement {
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
		if ref.Name == tlsSecretName {
			continue
		}
		addRequirement(ref.Name)
	}

	for _, volume := range target.Spec.Template.Spec.Volumes {
		if volume.Name == "tls" || (volume.Secret != nil && volume.Secret.SecretName == tlsSecretName) {
			continue
		}
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
				if coreTLSSecretEnvVarNames[envVar.Name] || singleUserSecretEnvVarNames[envVar.Name] {
					continue
				}
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

var coreTLSSecretEnvVarNames = map[string]bool{
	"KEYSTORE_PASSWORD":        true,
	"TRUSTSTORE_PASSWORD":      true,
	"NIFI_SENSITIVE_PROPS_KEY": true,
}

var singleUserSecretEnvVarNames = map[string]bool{
	"SINGLE_USER_CREDENTIALS_USERNAME": true,
	"SINGLE_USER_CREDENTIALS_PASSWORD": true,
}

func validateSecretRequirements(loadSecret secretLoader, requirements []secretRequirement) (string, string, error) {
	for _, requirement := range requirements {
		if requirement.Name == "" {
			continue
		}

		secret, found, err := loadSecret(requirement.Name)
		if err != nil {
			return "", "", err
		}
		if !found {
			return "SecretMissing", fmt.Sprintf("Secret %q is referenced by the managed NiFi inputs but does not exist", requirement.Name), nil
		}

		for _, key := range sortedKeys(requirement.RequiredKeys) {
			if len(secret.Data[key]) > 0 {
				continue
			}
			return "SecretKeyMissing", fmt.Sprintf("Secret %q is missing required key %q", requirement.Name, key), nil
		}
	}

	return "", "", nil
}

func validateSingleUserSecretRequirements(loadSecret secretLoader, target *appsv1.StatefulSet) (string, string, bool, error) {
	container, err := findContainer(target.Spec.Template.Spec.Containers, nifiContainerName)
	if err != nil {
		return "", "", false, nil
	}

	usernameEnv, usernameFound := findEnvVar(container.Env, "SINGLE_USER_CREDENTIALS_USERNAME")
	passwordEnv, passwordFound := findEnvVar(container.Env, "SINGLE_USER_CREDENTIALS_PASSWORD")
	if !usernameFound && !passwordFound {
		return "", "", false, nil
	}
	if !usernameFound || !passwordFound {
		return "SingleUserSecretReferenceMissing", "The target StatefulSet is missing one or more single-user credential Secret references", true, nil
	}

	if reason, message, handled, err := validateSecretBackedEnv(loadSecret, usernameEnv, "single-user username", "SingleUserSecretReferenceMissing", "SingleUserSecretMissing", "SingleUserSecretKeyMissing"); err != nil {
		return "", "", false, err
	} else if handled {
		return reason, message, true, nil
	}
	if reason, message, handled, err := validateSecretBackedEnv(loadSecret, passwordEnv, "single-user password", "SingleUserSecretReferenceMissing", "SingleUserSecretMissing", "SingleUserSecretKeyMissing"); err != nil {
		return "", "", false, err
	} else if handled {
		return reason, message, true, nil
	}

	return "", "", false, nil
}

func validateTLSContractRequirements(loadSecret secretLoader, target *appsv1.StatefulSet, tlsSecretName string) (string, string, bool, error) {
	tlsSecret, found, err := loadSecret(tlsSecretName)
	if err != nil {
		return "", "", false, err
	}
	if !found {
		return "TLSSecretMissing", fmt.Sprintf("TLS Secret %q was not found", tlsSecretName), true, nil
	}

	for _, key := range requiredTLSSecretKeys(target) {
		if len(tlsSecret.Data[key]) > 0 {
			continue
		}
		return "TLSSecretKeyMissing", fmt.Sprintf("TLS Secret %q is missing required key %q", tlsSecretName, key), true, nil
	}

	initContainer, err := findContainer(target.Spec.Template.Spec.InitContainers, "init-conf")
	if err != nil {
		return "TLSConfigurationReferenceMissing", "The target StatefulSet is missing the init-conf container required for TLS bootstrap", true, nil
	}

	for _, envName := range []string{"KEYSTORE_PASSWORD", "TRUSTSTORE_PASSWORD"} {
		envVar, found := findEnvVar(initContainer.Env, envName)
		if !found {
			return "TLSConfigurationReferenceMissing", fmt.Sprintf("The target StatefulSet is missing TLS bootstrap env %q", envName), true, nil
		}

		if reason, message, handled, err := validateTLSEnv(loadSecret, envVar, envName, tlsSecretName); err != nil {
			return "", "", false, err
		} else if handled {
			return reason, message, true, nil
		}
	}

	if envVar, found := findEnvVar(initContainer.Env, "NIFI_SENSITIVE_PROPS_KEY"); found {
		if reason, message, handled, err := validateTLSEnv(loadSecret, envVar, "NIFI_SENSITIVE_PROPS_KEY", tlsSecretName); err != nil {
			return "", "", false, err
		} else if handled {
			return reason, message, true, nil
		}
	}

	return "", "", false, nil
}

func requiredTLSSecretKeys(target *appsv1.StatefulSet) []string {
	required := map[string]struct{}{
		defaultCACertKey: {},
	}

	initContainer, err := findContainer(target.Spec.Template.Spec.InitContainers, "init-conf")
	if err == nil {
		for _, propertyName := range []string{"nifi.security.keystore", "nifi.security.truststore"} {
			if value, ok := extractReplacePropertyValue(initContainer, propertyName); ok {
				required[path.Base(value)] = struct{}{}
			}
		}
	}

	return sortedKeys(required)
}

func validateTLSEnv(loadSecret secretLoader, envVar corev1.EnvVar, envName, tlsSecretName string) (string, string, bool, error) {
	if envVar.Value != "" {
		return "", "", false, nil
	}
	if envVar.ValueFrom == nil || envVar.ValueFrom.SecretKeyRef == nil {
		return "TLSConfigurationReferenceMissing", fmt.Sprintf("TLS bootstrap env %q must come from an inline value or Secret reference", envName), true, nil
	}

	secretRef := envVar.ValueFrom.SecretKeyRef
	secret, found, err := loadSecret(secretRef.Name)
	if err != nil {
		return "", "", false, err
	}
	if !found {
		if secretRef.Name == tlsSecretName {
			return "TLSSecretMissing", fmt.Sprintf("TLS Secret %q was not found", secretRef.Name), true, nil
		}
		return "TLSSupportSecretMissing", fmt.Sprintf("TLS support Secret %q was not found for %s", secretRef.Name, envName), true, nil
	}
	if len(secret.Data[secretRef.Key]) > 0 {
		return "", "", false, nil
	}
	if secretRef.Name == tlsSecretName {
		return "TLSSecretKeyMissing", fmt.Sprintf("TLS Secret %q is missing required key %q", secretRef.Name, secretRef.Key), true, nil
	}
	return "TLSSupportSecretKeyMissing", fmt.Sprintf("TLS support Secret %q is missing required key %q for %s", secretRef.Name, secretRef.Key, envName), true, nil
}

func validateSecretBackedEnv(loadSecret secretLoader, envVar corev1.EnvVar, description, referenceReason, secretReason, keyReason string) (string, string, bool, error) {
	if envVar.Value != "" {
		return "", "", false, nil
	}
	if envVar.ValueFrom == nil || envVar.ValueFrom.SecretKeyRef == nil {
		return referenceReason, fmt.Sprintf("The target StatefulSet must provide %s through a Secret reference or inline value", description), true, nil
	}

	secretRef := envVar.ValueFrom.SecretKeyRef
	secret, found, err := loadSecret(secretRef.Name)
	if err != nil {
		return "", "", false, err
	}
	if !found {
		return secretReason, fmt.Sprintf("Secret %q was not found for %s", secretRef.Name, description), true, nil
	}
	if len(secret.Data[secretRef.Key]) > 0 {
		return "", "", false, nil
	}
	return keyReason, fmt.Sprintf("Secret %q is missing required key %q for %s", secretRef.Name, secretRef.Key, description), true, nil
}

func findEnvVar(envVars []corev1.EnvVar, name string) (corev1.EnvVar, bool) {
	for _, envVar := range envVars {
		if envVar.Name == name {
			return envVar, true
		}
	}
	return corev1.EnvVar{}, false
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
