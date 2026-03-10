package controller

import (
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
)

const tlsAutoreloadObservationWindow = 30 * time.Second

func tlsDiffPolicy(cluster *platformv1alpha1.NiFiCluster) platformv1alpha1.TLSDiffPolicy {
	if cluster.Spec.RestartPolicy.TLSDrift == "" {
		return platformv1alpha1.TLSDiffPolicyAutoreloadThenRestartOnFailure
	}
	return cluster.Spec.RestartPolicy.TLSDrift
}

func computeTLSConfigurationHash(sts *appsv1.StatefulSet) (string, error) {
	lines := make([]string, 0)

	secretName, err := findTLSSecretName(sts.Spec.Template.Spec.Volumes)
	if err != nil {
		return "", err
	}
	lines = append(lines, "secretName="+secretName)

	for _, containerSet := range [][]corev1.Container{sts.Spec.Template.Spec.InitContainers, sts.Spec.Template.Spec.Containers} {
		for _, container := range containerSet {
			for _, mount := range container.VolumeMounts {
				if mount.Name != "tls" {
					continue
				}
				lines = append(lines, fmt.Sprintf("mount/%s/path=%s/readOnly=%t/subPath=%s", container.Name, mount.MountPath, mount.ReadOnly, mount.SubPath))
			}
		}
	}

	initContainer, err := findContainer(sts.Spec.Template.Spec.InitContainers, "init-conf")
	if err == nil {
		for _, envName := range []string{"KEYSTORE_PASSWORD", "TRUSTSTORE_PASSWORD"} {
			secretRef, refErr := findSecretKeyRef(initContainer.Env, envName)
			if refErr == nil {
				lines = append(lines, fmt.Sprintf("env/%s=%s/%s", envName, secretRef.Name, secretRef.Key))
			}
		}
		for _, propertyName := range []string{"nifi.security.keystore", "nifi.security.truststore"} {
			if value, ok := extractReplacePropertyValue(initContainer, propertyName); ok {
				lines = append(lines, fmt.Sprintf("property/%s=%s", propertyName, value))
			}
		}
	}

	return hashLines(lines), nil
}

func extractReplacePropertyValue(container corev1.Container, propertyName string) (string, bool) {
	pattern := fmt.Sprintf(`replace_property "%s" "`, propertyName)
	for _, value := range append(append([]string{}, container.Command...), container.Args...) {
		index := strings.Index(value, pattern)
		if index == -1 {
			continue
		}
		remaining := value[index+len(pattern):]
		end := strings.Index(remaining, `"`)
		if end == -1 {
			continue
		}
		return remaining[:end], true
	}
	return "", false
}

func tlsObservationMatches(cluster *platformv1alpha1.NiFiCluster, drift WatchedResourceDrift) bool {
	return cluster.Status.TLS.TargetCertificateHash == drift.CurrentCertificateHash &&
		cluster.Status.TLS.TargetTLSConfigurationHash == drift.CurrentTLSConfigurationHash &&
		cluster.Status.TLS.ObservationStartedAt != nil
}

func tlsObservationElapsed(cluster *platformv1alpha1.NiFiCluster) bool {
	if cluster.Status.TLS.ObservationStartedAt == nil {
		return false
	}
	return time.Since(cluster.Status.TLS.ObservationStartedAt.Time) >= tlsAutoreloadObservationWindow
}

func startTLSObservation(cluster *platformv1alpha1.NiFiCluster, drift WatchedResourceDrift) {
	if tlsObservationMatches(cluster, drift) {
		return
	}

	now := metav1.NewTime(time.Now().UTC())
	cluster.Status.TLS = platformv1alpha1.TLSStatus{
		ObservationStartedAt:       &now,
		TargetCertificateHash:      drift.CurrentCertificateHash,
		TargetTLSConfigurationHash: drift.CurrentTLSConfigurationHash,
	}
}

func clearTLSObservation(cluster *platformv1alpha1.NiFiCluster) {
	cluster.Status.TLS = platformv1alpha1.TLSStatus{}
}
