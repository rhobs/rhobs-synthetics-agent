package templates

import (
	"os"
	"testing"

	"sigs.k8s.io/yaml"
)

type templateFile struct {
	Objects []map[string]interface{} `json:"objects"`
}

func loadTemplate(t *testing.T) templateFile {
	t.Helper()
	data, err := os.ReadFile("template.yaml")
	if err != nil {
		t.Fatalf("reading template.yaml: %v", err)
	}
	var tmpl templateFile
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		t.Fatalf("parsing template.yaml: %v", err)
	}
	return tmpl
}

func objectsByKind(tmpl templateFile, kind string) []map[string]interface{} {
	var out []map[string]interface{}
	for _, obj := range tmpl.Objects {
		if k, _ := obj["kind"].(string); k == kind {
			out = append(out, obj)
		}
	}
	return out
}

const oidcSecretName = "${APPLICATION_NAME}-oidc"

// Expected mapping: env var name -> Secret key -> template parameter
var oidcEnvToSecretKey = map[string]string{
	"OIDC_CLIENT_ID":     "client-id",
	"OIDC_CLIENT_SECRET": "client-secret",
	"OIDC_ISSUER_URL":    "issuer-url",
}

var oidcSecretKeyToParam = map[string]string{
	"client-id":     "${OIDC_CLIENT_ID}",
	"client-secret": "${OIDC_CLIENT_SECRET}",
	"issuer-url":    "${OIDC_ISSUER_URL}",
}

func findOIDCSecret(t *testing.T, tmpl templateFile) map[string]interface{} {
	t.Helper()
	for _, s := range objectsByKind(tmpl, "Secret") {
		meta, _ := s["metadata"].(map[string]interface{})
		name, _ := meta["name"].(string)
		if name == oidcSecretName {
			return s
		}
	}
	t.Fatalf("template must contain Secret named %q", oidcSecretName)
	return nil
}

func deploymentEnvVars(t *testing.T, tmpl templateFile) []map[string]interface{} {
	t.Helper()
	var envs []map[string]interface{}
	for _, deploy := range objectsByKind(tmpl, "Deployment") {
		spec, _ := deploy["spec"].(map[string]interface{})
		template, _ := spec["template"].(map[string]interface{})
		podSpec, _ := template["spec"].(map[string]interface{})
		containers, _ := podSpec["containers"].([]interface{})
		for _, c := range containers {
			container, _ := c.(map[string]interface{})
			envList, _ := container["env"].([]interface{})
			for _, e := range envList {
				env, _ := e.(map[string]interface{})
				envs = append(envs, env)
			}
		}
	}
	return envs
}

func TestOIDCCredentialsNotLiteralEnvValues(t *testing.T) {
	tmpl := loadTemplate(t)
	envs := deploymentEnvVars(t, tmpl)

	found := make(map[string]bool)
	for _, env := range envs {
		name, _ := env["name"].(string)
		expectedKey, isOIDC := oidcEnvToSecretKey[name]
		if !isOIDC {
			continue
		}
		found[name] = true

		if _, hasValue := env["value"]; hasValue {
			t.Errorf("env var %s uses literal value — must use valueFrom.secretKeyRef to avoid leaking credentials in pod spec", name)
		}
		valueFrom, _ := env["valueFrom"].(map[string]interface{})
		if valueFrom == nil {
			t.Errorf("env var %s missing valueFrom", name)
			continue
		}
		ref, _ := valueFrom["secretKeyRef"].(map[string]interface{})
		if ref == nil {
			t.Errorf("env var %s has valueFrom but not secretKeyRef", name)
			continue
		}
		refName, _ := ref["name"].(string)
		if refName != oidcSecretName {
			t.Errorf("env var %s references secret %q, want %q", name, refName, oidcSecretName)
		}
		refKey, _ := ref["key"].(string)
		if refKey != expectedKey {
			t.Errorf("env var %s secretKeyRef key = %q, want %q", name, refKey, expectedKey)
		}
	}

	for envName := range oidcEnvToSecretKey {
		if !found[envName] {
			t.Errorf("required OIDC env var %s not found in Deployment pod spec", envName)
		}
	}
}

func TestOIDCSecretObjectExists(t *testing.T) {
	tmpl := loadTemplate(t)
	oidcSecret := findOIDCSecret(t, tmpl)

	stringData, _ := oidcSecret["stringData"].(map[string]interface{})
	if stringData == nil {
		t.Fatal("OIDC Secret must have stringData")
	}

	for key, expectedParam := range oidcSecretKeyToParam {
		val, ok := stringData[key]
		if !ok {
			t.Errorf("OIDC Secret missing required key %q", key)
			continue
		}
		valStr, _ := val.(string)
		if valStr != expectedParam {
			t.Errorf("OIDC Secret key %q = %q, want template parameter %q", key, valStr, expectedParam)
		}
	}
}

func TestLeaseRBACPresent(t *testing.T) {
	tmpl := loadTemplate(t)
	roles := objectsByKind(tmpl, "Role")
	found := false
	for _, role := range roles {
		rules, _ := role["rules"].([]interface{})
		for _, r := range rules {
			rule, _ := r.(map[string]interface{})
			apiGroups, _ := rule["apiGroups"].([]interface{})
			resources, _ := rule["resources"].([]interface{})
			for _, ag := range apiGroups {
				if ag == "coordination.k8s.io" {
					for _, res := range resources {
						if res == "leases" {
							found = true
						}
					}
				}
			}
		}
	}
	if !found {
		t.Error("template Role must grant access to coordination.k8s.io/leases for leader election")
	}
}

func TestPodAntiAffinityPresent(t *testing.T) {
	tmpl := loadTemplate(t)
	for _, deploy := range objectsByKind(tmpl, "Deployment") {
		spec, _ := deploy["spec"].(map[string]interface{})
		template, _ := spec["template"].(map[string]interface{})
		podSpec, _ := template["spec"].(map[string]interface{})
		affinity, _ := podSpec["affinity"].(map[string]interface{})
		if affinity == nil {
			t.Fatal("Deployment must have affinity configured")
		}
		paa, _ := affinity["podAntiAffinity"].(map[string]interface{})
		if paa == nil {
			t.Fatal("Deployment must have podAntiAffinity configured")
		}
		required, _ := paa["requiredDuringSchedulingIgnoredDuringExecution"].([]interface{})
		if len(required) == 0 {
			t.Fatal("podAntiAffinity must have at least one requiredDuringSchedulingIgnoredDuringExecution term")
		}
		term, _ := required[0].(map[string]interface{})
		topologyKey, _ := term["topologyKey"].(string)
		if topologyKey != "kubernetes.io/hostname" {
			t.Errorf("anti-affinity topologyKey = %q, want %q", topologyKey, "kubernetes.io/hostname")
		}
	}
}

func TestPodNameEnvVar(t *testing.T) {
	tmpl := loadTemplate(t)
	envs := deploymentEnvVars(t, tmpl)
	found := false
	for _, env := range envs {
		name, _ := env["name"].(string)
		if name != "POD_NAME" {
			continue
		}
		found = true
		valueFrom, _ := env["valueFrom"].(map[string]interface{})
		if valueFrom == nil {
			t.Error("POD_NAME must use valueFrom")
			continue
		}
		fieldRef, _ := valueFrom["fieldRef"].(map[string]interface{})
		if fieldRef == nil {
			t.Error("POD_NAME must use fieldRef")
			continue
		}
		fieldPath, _ := fieldRef["fieldPath"].(string)
		if fieldPath != "metadata.name" {
			t.Errorf("POD_NAME fieldPath = %q, want %q", fieldPath, "metadata.name")
		}
	}
	if !found {
		t.Error("Deployment must have POD_NAME env var for leader election identity")
	}
}

func TestReplicaCountDefault(t *testing.T) {
	tmpl := loadTemplate(t)
	for _, deploy := range objectsByKind(tmpl, "Deployment") {
		spec, _ := deploy["spec"].(map[string]interface{})
		replicas := spec["replicas"]
		if replicas != "${REPLICA_COUNT}" && replicas != "${{REPLICA_COUNT}}" {
			t.Errorf("Deployment replicas = %v, want template parameter REPLICA_COUNT", replicas)
		}
	}
}

func TestSecretKeyRefMatchesSecretName(t *testing.T) {
	tmpl := loadTemplate(t)
	findOIDCSecret(t, tmpl)
	envs := deploymentEnvVars(t, tmpl)

	found := make(map[string]bool)
	for _, env := range envs {
		name, _ := env["name"].(string)
		expectedKey, isOIDC := oidcEnvToSecretKey[name]
		if !isOIDC {
			continue
		}
		found[name] = true

		valueFrom, _ := env["valueFrom"].(map[string]interface{})
		if valueFrom == nil {
			t.Errorf("env var %s missing valueFrom", name)
			continue
		}
		ref, _ := valueFrom["secretKeyRef"].(map[string]interface{})
		if ref == nil {
			t.Errorf("env var %s missing secretKeyRef", name)
			continue
		}
		refName, _ := ref["name"].(string)
		if refName != oidcSecretName {
			t.Errorf("env var %s secretKeyRef references %q, want %q", name, refName, oidcSecretName)
		}
		refKey, _ := ref["key"].(string)
		if refKey != expectedKey {
			t.Errorf("env var %s secretKeyRef key = %q, want %q", name, refKey, expectedKey)
		}
	}

	for envName := range oidcEnvToSecretKey {
		if !found[envName] {
			t.Errorf("required OIDC env var %s not found in Deployment pod spec", envName)
		}
	}
}
