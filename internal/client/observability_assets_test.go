package client

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestAlertingRules_ValidYAML(t *testing.T) {
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
	assert.GreaterOrEqual(t, len(ruleList), 7, "should have at least 7 alerting rules")

	for i, r := range ruleList {
		rule := r.(map[string]any)
		assert.NotEmpty(t, rule["alert"], "rule %d should have 'alert' name", i)
		assert.NotEmpty(t, rule["expr"], "rule %d should have 'expr'", i)
	}
}

func TestGrafanaDashboard_ValidJSON(t *testing.T) {
	data, err := os.ReadFile("../../deploy/observability/dashboards/truenas-provider.json")
	require.NoError(t, err, "should be able to read dashboard file")

	var dashboard map[string]any
	err = json.Unmarshal(data, &dashboard)
	require.NoError(t, err, "dashboard should be valid JSON")

	assert.Equal(t, "TrueNAS Omni Provider", dashboard["title"])
	assert.Equal(t, "truenas-omni-provider", dashboard["uid"])

	panels, ok := dashboard["panels"].([]any)
	require.True(t, ok, "should have 'panels' key")
	assert.GreaterOrEqual(t, len(panels), 10, "should have at least 10 panels")

	for i, p := range panels {
		panel := p.(map[string]any)
		assert.NotEmpty(t, panel["title"], "panel %d should have 'title'", i)
		assert.NotEmpty(t, panel["type"], "panel %d should have 'type'", i)
	}
}
