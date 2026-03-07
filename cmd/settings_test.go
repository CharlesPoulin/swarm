package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSettingsFields_DefaultsWhenConfigMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude-swarm.yaml")
	fields, err := loadSettingsFields(path)
	if err != nil {
		t.Fatalf("loadSettingsFields: %v", err)
	}
	if len(fields) == 0 {
		t.Fatalf("expected editable settings fields")
	}

	if got := findFieldValue(t, fields, "num"); got != "6" {
		t.Fatalf("num default = %q, want %q", got, "6")
	}
	if got := findFieldValue(t, fields, "session"); got != "claude-swarm" {
		t.Fatalf("session default = %q, want %q", got, "claude-swarm")
	}
}

func TestPersistSettings_WritesAndPreservesUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude-swarm.yaml")
	initial := "num: 2\nsession: old\ncustom_note: keep-me\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	fields, err := loadSettingsFields(path)
	if err != nil {
		t.Fatalf("loadSettingsFields: %v", err)
	}
	setFieldValue(t, fields, "num", "8")
	setFieldValue(t, fields, "session", "team-swarm")
	setFieldValue(t, fields, "hub_mode", "git")
	setFieldValue(t, fields, "assignment_mode", "manual")

	if err := persistSettings(path, fields); err != nil {
		t.Fatalf("persistSettings: %v", err)
	}

	doc, err := loadConfigDocument(path)
	if err != nil {
		t.Fatalf("loadConfigDocument: %v", err)
	}

	if got, _ := doc["num"].(int); got != 8 {
		t.Fatalf("num=%v, want 8", doc["num"])
	}
	if got, _ := doc["session"].(string); got != "team-swarm" {
		t.Fatalf("session=%v, want team-swarm", doc["session"])
	}
	if got, _ := doc["hub_mode"].(string); got != "git" {
		t.Fatalf("hub_mode=%v, want git", doc["hub_mode"])
	}
	if got, _ := doc["assignment_mode"].(string); got != "manual" {
		t.Fatalf("assignment_mode=%v, want manual", doc["assignment_mode"])
	}
	if got, _ := doc["custom_note"].(string); got != "keep-me" {
		t.Fatalf("custom_note=%v, want keep-me", doc["custom_note"])
	}
}

func TestPersistSettings_DispatchPlanModeBoolean(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude-swarm.yaml")

	fields, err := loadSettingsFields(path)
	if err != nil {
		t.Fatalf("loadSettingsFields: %v", err)
	}
	setFieldValue(t, fields, "dispatch_plan_mode", "false")

	if err := persistSettings(path, fields); err != nil {
		t.Fatalf("persistSettings: %v", err)
	}

	doc, err := loadConfigDocument(path)
	if err != nil {
		t.Fatalf("loadConfigDocument: %v", err)
	}

	got, ok := doc["dispatch_plan_mode"].(bool)
	if !ok {
		t.Fatalf("dispatch_plan_mode type=%T, want bool", doc["dispatch_plan_mode"])
	}
	if got {
		t.Fatalf("dispatch_plan_mode=%v, want false", got)
	}
}

func TestPersistSettings_ValidationErrorDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude-swarm.yaml")
	initial := "num: 4\nsession: keep\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read initial config: %v", err)
	}

	fields, err := loadSettingsFields(path)
	if err != nil {
		t.Fatalf("loadSettingsFields: %v", err)
	}
	setFieldValue(t, fields, "num", "zero")

	err = persistSettings(path, fields)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "Workers") {
		t.Fatalf("unexpected error: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read resulting config: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("config file changed on failed validation\nbefore:\n%s\nafter:\n%s", string(before), string(after))
	}
}

func TestLoadSettingsFields_InvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude-swarm.yaml")
	if err := os.WriteFile(path, []byte("num: [\n"), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
	_, err := loadSettingsFields(path)
	if err == nil {
		t.Fatalf("expected YAML parse error")
	}
	if !strings.Contains(err.Error(), "invalid YAML") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func findFieldValue(t *testing.T, fields []settingField, key string) string {
	t.Helper()
	for _, f := range fields {
		if f.Spec.Key == key {
			return f.Value
		}
	}
	t.Fatalf("field %q not found", key)
	return ""
}

func setFieldValue(t *testing.T, fields []settingField, key, value string) {
	t.Helper()
	for i := range fields {
		if fields[i].Spec.Key == key {
			fields[i].Value = value
			return
		}
	}
	t.Fatalf("field %q not found", key)
}
