package compose

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

func TestParseComposePS_AllHealthy(t *testing.T) {
	ndjson := []byte(`{"Service":"db","State":"running","Health":"healthy"}
{"Service":"kong","State":"running","Health":"healthy"}`)

	health := parseComposePS(ndjson)
	require.Len(t, health.Services, 2)
	assert.Equal(t, domain.ServiceStatusHealthy, health.Services["db"].Status)
	assert.Equal(t, domain.ServiceStatusHealthy, health.Services["kong"].Status)
}

func TestParseComposePS_NoHealthcheck_TreatedAsHealthy(t *testing.T) {
	ndjson := []byte(`{"Service":"kong","State":"running","Health":""}`)
	health := parseComposePS(ndjson)
	assert.Equal(t, domain.ServiceStatusHealthy, health.Services["kong"].Status)
}

func TestParseComposePS_Unhealthy(t *testing.T) {
	ndjson := []byte(`{"Service":"auth","State":"running","Health":"unhealthy"}`)
	health := parseComposePS(ndjson)
	assert.Equal(t, domain.ServiceStatusUnhealthy, health.Services["auth"].Status)
}

func TestParseComposePS_Restarting_TreatedAsUnhealthy(t *testing.T) {
	ndjson := []byte(`{"Service":"rest","State":"running","Health":"restarting"}`)
	health := parseComposePS(ndjson)
	assert.Equal(t, domain.ServiceStatusUnhealthy, health.Services["rest"].Status)
}

func TestParseComposePS_Starting(t *testing.T) {
	ndjson := []byte(`{"Service":"db","State":"running","Health":"starting"}`)
	health := parseComposePS(ndjson)
	assert.Equal(t, domain.ServiceStatusStarting, health.Services["db"].Status)
}

func TestParseComposePS_Exited_TreatedAsStopped(t *testing.T) {
	ndjson := []byte(`{"Service":"studio","State":"exited","Health":""}`)
	health := parseComposePS(ndjson)
	assert.Equal(t, domain.ServiceStatusStopped, health.Services["studio"].Status)
}

func TestParseComposePS_UnknownState(t *testing.T) {
	ndjson := []byte(`{"Service":"meta","State":"dead","Health":""}`)
	health := parseComposePS(ndjson)
	assert.Equal(t, domain.ServiceStatusUnknown, health.Services["meta"].Status)
}

func TestParseComposePS_EmptyOutput_ReturnsEmptyMap(t *testing.T) {
	health := parseComposePS([]byte(""))
	require.NotNil(t, health)
	assert.Empty(t, health.Services)
}

func TestParseComposePS_MalformedLine_SkippedOthersProcessed(t *testing.T) {
	ndjson := []byte(`not-json
{"Service":"db","State":"running","Health":"healthy"}`)
	health := parseComposePS(ndjson)
	assert.Len(t, health.Services, 1)
	assert.Equal(t, domain.ServiceStatusHealthy, health.Services["db"].Status)
}
