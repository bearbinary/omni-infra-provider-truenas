package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestAlertingRules_ValidYAML(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../deploy/observability/alerts/truenas-provider.rules.yml")
	require.NoError(t, err, "should be able to read alerting rules file")

	var rules map[string]any
	err = yaml.Unmarshal(data, &rules)
	require.NoError(t, err, "alerting rules should be valid YAML")

	groups, ok := rules["groups"].([]any)
	require.True(t, ok, "should have 'groups' key")
	require.NotEmpty(t, groups, "should have at least one group")

	group := groups[0].(map[string]any)
	assert.Equal(t, "truenas-provider", group["name"])

	ruleList, ok := group["rules"].([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(ruleList), 11, "should have at least 11 alerting rules")

	for i, r := range ruleList {
		rule := r.(map[string]any)
		assert.NotEmpty(t, rule["alert"], "rule %d should have 'alert' name", i)
		assert.NotEmpty(t, rule["expr"], "rule %d should have 'expr'", i)
	}
}

func TestGrafanaDashboards_ValidJSON(t *testing.T) {
	t.Parallel()
	dashboardDir := "../../deploy/observability/dashboards"

	expected := map[string]struct {
		Title     string
		UID       string
		MinPanels int
	}{
		"overview.json":        {Title: "TrueNAS Provider / Overview", UID: "truenas-overview", MinPanels: 10},
		"provisioning.json":    {Title: "TrueNAS Provider / Provisioning", UID: "truenas-provisioning", MinPanels: 10},
		"api-performance.json": {Title: "TrueNAS Provider / API Performance", UID: "truenas-api", MinPanels: 10},
		"cleanup.json":         {Title: "TrueNAS Provider / Cleanup & Maintenance", UID: "truenas-cleanup", MinPanels: 8},
	}

	entries, err := os.ReadDir(dashboardDir)
	require.NoError(t, err, "should be able to read dashboards directory")

	found := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		found[e.Name()] = true
	}

	for name := range expected {
		assert.True(t, found[name], "expected dashboard file %s to exist", name)
	}

	for name, want := range expected {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dashboardDir, name))
			require.NoError(t, err)

			var dashboard map[string]any
			err = json.Unmarshal(data, &dashboard)
			require.NoError(t, err, "dashboard should be valid JSON")

			assert.Equal(t, want.Title, dashboard["title"])
			assert.Equal(t, want.UID, dashboard["uid"])

			panels, ok := dashboard["panels"].([]any)
			require.True(t, ok, "should have 'panels' key")
			assert.GreaterOrEqual(t, len(panels), want.MinPanels, "should have at least %d panels", want.MinPanels)

			for i, p := range panels {
				panel := p.(map[string]any)
				assert.NotEmpty(t, panel["type"], "panel %d should have 'type'", i)
			}
		})
	}
}
