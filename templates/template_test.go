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
