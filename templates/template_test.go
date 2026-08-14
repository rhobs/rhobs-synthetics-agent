package templates

import (
	"os"
	"strings"
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

func TestOIDCCredentialsNotLiteralEnvValues(t *testing.T) {
	tmpl := loadTemplate(t)

	sensitiveVars := map[string]bool{
		"OIDC_CLIENT_ID":     true,
		"OIDC_CLIENT_SECRET": true,
		"OIDC_ISSUER_URL":    true,
	}

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
				name, _ := env["name"].(string)
				if !sensitiveVars[name] {
					continue
				}
				if _, hasValue := env["value"]; hasValue {
					t.Errorf("env var %s uses literal value — must use valueFrom.secretKeyRef to avoid leaking credentials in pod spec", name)
				}
				valueFrom, _ := env["valueFrom"].(map[string]interface{})
				if valueFrom == nil {
					t.Errorf("env var %s missing valueFrom", name)
					continue
				}
				if _, ok := valueFrom["secretKeyRef"]; !ok {
					t.Errorf("env var %s has valueFrom but not secretKeyRef", name)
				}
			}
		}
	}
}

func TestOIDCSecretObjectExists(t *testing.T) {
	tmpl := loadTemplate(t)

	secrets := objectsByKind(tmpl, "Secret")

	var oidcSecret map[string]interface{}
	for _, s := range secrets {
		meta, _ := s["metadata"].(map[string]interface{})
		name, _ := meta["name"].(string)
		if strings.Contains(name, "oidc") {
			oidcSecret = s
			break
		}
	}
	if oidcSecret == nil {
		t.Fatal("template must contain a Secret object for OIDC credentials")
	}

	stringData, _ := oidcSecret["stringData"].(map[string]interface{})
	if stringData == nil {
		t.Fatal("OIDC Secret must have stringData")
	}

	for _, key := range []string{"client-id", "client-secret", "issuer-url"} {
		if _, ok := stringData[key]; !ok {
			t.Errorf("OIDC Secret missing required key %q", key)
		}
	}
}

func TestSecretKeyRefMatchesSecretName(t *testing.T) {
	tmpl := loadTemplate(t)

	var secretName string
	for _, s := range objectsByKind(tmpl, "Secret") {
		meta, _ := s["metadata"].(map[string]interface{})
		name, _ := meta["name"].(string)
		if strings.Contains(name, "oidc") {
			secretName = name
			break
		}
	}
	if secretName == "" {
		t.Fatal("no OIDC Secret found")
	}

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
				name, _ := env["name"].(string)
				if !strings.HasPrefix(name, "OIDC_") {
					continue
				}
				valueFrom, _ := env["valueFrom"].(map[string]interface{})
				if valueFrom == nil {
					continue
				}
				ref, _ := valueFrom["secretKeyRef"].(map[string]interface{})
				if ref == nil {
					continue
				}
				refName, _ := ref["name"].(string)
				if refName != secretName {
					t.Errorf("env var %s references secret %q but OIDC Secret is named %q", name, refName, secretName)
				}
			}
		}
	}
}
