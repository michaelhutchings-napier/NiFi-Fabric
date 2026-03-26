package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestValidateTLSContractRequirementsResolvesShellVariableTruststorePath(t *testing.T) {
	statefulSet := managedStatefulSet("nifi", 3, "nifi-rev", "nifi-rev")
	initContainer := &statefulSet.Spec.Template.Spec.InitContainers[0]
	initContainer.Env[0].ValueFrom.SecretKeyRef.Name = "nifi-tls-params"
	initContainer.Env[0].ValueFrom.SecretKeyRef.Key = "pkcs12Password"
	initContainer.Env[1].ValueFrom.SecretKeyRef.Name = "nifi-tls-params"
	initContainer.Env[1].ValueFrom.SecretKeyRef.Key = "pkcs12Password"
	initContainer.Env = append(initContainer.Env, corev1.EnvVar{
		Name: "NIFI_SENSITIVE_PROPS_KEY",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "nifi-tls-params"},
				Key:                  "sensitivePropsKey",
			},
		},
	})
	initContainer.Args = []string{
		`replace_property "nifi.security.keystore" "/opt/nifi/tls/keystore.p12"
TRUSTSTORE_PATH="/opt/nifi/tls/truststore.p12"
replace_property "nifi.security.truststore" "${TRUSTSTORE_PATH}"`,
	}

	secrets := map[string]*corev1.Secret{
		"nifi-tls": {
			Data: map[string][]byte{
				"ca.crt":         []byte("ca"),
				"keystore.p12":   []byte("keystore"),
				"truststore.p12": []byte("truststore"),
			},
		},
		"nifi-tls-params": {
			Data: map[string][]byte{
				"pkcs12Password":    []byte("changeit"),
				"sensitivePropsKey": []byte("sensitive"),
			},
		},
	}

	loadSecret := func(name string) (*corev1.Secret, bool, error) {
		secret, ok := secrets[name]
		return secret, ok, nil
	}

	reason, message, handled, err := validateTLSContractRequirements(loadSecret, statefulSet, "nifi-tls")
	if err != nil {
		t.Fatalf("validateTLSContractRequirements returned error: %v", err)
	}
	if handled {
		t.Fatalf("expected cert-manager TLS contract to validate successfully, got %s: %s", reason, message)
	}
}
