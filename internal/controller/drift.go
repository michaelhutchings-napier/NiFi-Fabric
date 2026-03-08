package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/michaelhutchings-napier/nifi-made-simple/api/v1alpha1"
)

type WatchedResourceDrift struct {
	CurrentConfigHash      string
	CurrentCertificateHash string
	TargetConfigHash       string
	HasConfigInputs        bool
	HasCertificateInputs   bool
	ConfigDrift            bool
	CertificateDrift       bool
	ConfigRefs             []string
	CertificateRefs        []string
}

func (r *NiFiClusterReconciler) computeWatchedResourceDrift(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) (WatchedResourceDrift, error) {
	tlsSecretName, err := findTLSSecretName(target.Spec.Template.Spec.Volumes)
	if err != nil {
		tlsSecretName = ""
	}

	configMaps, err := r.loadWatchedConfigMaps(ctx, cluster)
	if err != nil {
		return WatchedResourceDrift{}, err
	}
	configSecrets, certificateSecrets, err := r.loadWatchedSecrets(ctx, cluster, tlsSecretName)
	if err != nil {
		return WatchedResourceDrift{}, err
	}

	configHash := aggregateConfigHash(configMaps, configSecrets)
	certificateHash := aggregateCertificateHash(certificateSecrets)

	drift := WatchedResourceDrift{
		CurrentConfigHash:      configHash,
		CurrentCertificateHash: certificateHash,
		HasConfigInputs:        len(configMaps) > 0 || len(configSecrets) > 0,
		HasCertificateInputs:   len(certificateSecrets) > 0,
		TargetConfigHash:       cluster.Status.Rollout.TargetConfigHash,
		ConfigRefs:             referencedConfigRefs(cluster.Spec.RestartTriggers.ConfigMaps, configSecrets),
		CertificateRefs:        referencedSecretRefs(certificateSecrets),
	}

	if drift.TargetConfigHash == "" {
		drift.TargetConfigHash = drift.CurrentConfigHash
	}

	if shouldCompareObservedHash(cluster.Status.ObservedConfigHash, drift.HasConfigInputs) && drift.CurrentConfigHash != cluster.Status.ObservedConfigHash {
		drift.ConfigDrift = true
	}
	if shouldCompareObservedHash(cluster.Status.ObservedCertificateHash, drift.HasCertificateInputs) && drift.CurrentCertificateHash != cluster.Status.ObservedCertificateHash {
		drift.CertificateDrift = true
	}

	if cluster.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerConfigDrift &&
		cluster.Status.Rollout.TargetConfigHash != "" &&
		cluster.Status.Rollout.TargetConfigHash != cluster.Status.ObservedConfigHash {
		drift.ConfigDrift = true
		drift.TargetConfigHash = cluster.Status.Rollout.TargetConfigHash
	}

	return drift, nil
}

func (r *NiFiClusterReconciler) loadWatchedConfigMaps(ctx context.Context, cluster *platformv1alpha1.NiFiCluster) ([]corev1.ConfigMap, error) {
	configMaps := make([]corev1.ConfigMap, 0, len(cluster.Spec.RestartTriggers.ConfigMaps))
	for _, ref := range cluster.Spec.RestartTriggers.ConfigMaps {
		if ref.Name == "" {
			continue
		}
		configMap := &corev1.ConfigMap{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: ref.Name}, configMap); err != nil {
			return nil, fmt.Errorf("get watched ConfigMap %q: %w", ref.Name, err)
		}
		configMaps = append(configMaps, *configMap)
	}
	return configMaps, nil
}

func (r *NiFiClusterReconciler) loadWatchedSecrets(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, tlsSecretName string) ([]corev1.Secret, []corev1.Secret, error) {
	configSecrets := make([]corev1.Secret, 0, len(cluster.Spec.RestartTriggers.Secrets))
	certificateSecrets := make([]corev1.Secret, 0, len(cluster.Spec.RestartTriggers.Secrets))

	for _, ref := range cluster.Spec.RestartTriggers.Secrets {
		if ref.Name == "" {
			continue
		}
		secret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: ref.Name}, secret); err != nil {
			return nil, nil, fmt.Errorf("get watched Secret %q: %w", ref.Name, err)
		}
		if ref.Name == tlsSecretName {
			certificateSecrets = append(certificateSecrets, *secret)
			continue
		}
		configSecrets = append(configSecrets, *secret)
	}

	return configSecrets, certificateSecrets, nil
}

func aggregateConfigHash(configMaps []corev1.ConfigMap, secrets []corev1.Secret) string {
	sort.Slice(configMaps, func(i, j int) bool {
		return configMaps[i].Name < configMaps[j].Name
	})
	sort.Slice(secrets, func(i, j int) bool {
		return secrets[i].Name < secrets[j].Name
	})

	lines := make([]string, 0)
	for _, configMap := range configMaps {
		lines = append(lines, "ConfigMap/"+configMap.Name)
		lines = append(lines, canonicalStringMap("data", configMap.Data)...)
		lines = append(lines, canonicalByteMap("binaryData", configMap.BinaryData)...)
	}
	for _, secret := range secrets {
		lines = append(lines, "Secret/"+secret.Name)
		lines = append(lines, "type="+string(secret.Type))
		lines = append(lines, canonicalByteMap("data", secret.Data)...)
	}
	return hashLines(lines)
}

func aggregateCertificateHash(secrets []corev1.Secret) string {
	sort.Slice(secrets, func(i, j int) bool {
		return secrets[i].Name < secrets[j].Name
	})

	lines := make([]string, 0)
	for _, secret := range secrets {
		lines = append(lines, "Secret/"+secret.Name)
		lines = append(lines, "type="+string(secret.Type))
		lines = append(lines, canonicalByteMap("data", secret.Data)...)
	}
	return hashLines(lines)
}

func canonicalStringMap(prefix string, values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s/%s=%s", prefix, key, values[key]))
	}
	return lines
}

func canonicalByteMap(prefix string, values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s/%s=%s", prefix, key, hex.EncodeToString(values[key])))
	}
	return lines
}

func hashLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

func shouldCompareObservedHash(observed string, hasInputs bool) bool {
	return observed != ""
}

func referencedConfigRefs(configMapRefs []corev1.LocalObjectReference, configSecrets []corev1.Secret) []string {
	names := make([]string, 0, len(configMapRefs)+len(configSecrets))
	for _, ref := range configMapRefs {
		if ref.Name != "" {
			names = append(names, "ConfigMap/"+ref.Name)
		}
	}
	for _, secret := range configSecrets {
		names = append(names, "Secret/"+secret.Name)
	}
	sort.Strings(names)
	return names
}

func referencedSecretRefs(secrets []corev1.Secret) []string {
	names := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		names = append(names, "Secret/"+secret.Name)
	}
	sort.Strings(names)
	return names
}
