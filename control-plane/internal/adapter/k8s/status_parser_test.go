package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

func TestParseK8sPods_Empty(t *testing.T) {
	health := parseK8sPods([]byte(""))
	require.NotNil(t, health)
	assert.Empty(t, health.Services)
}

func TestParseK8sPods_InvalidJSON(t *testing.T) {
	health := parseK8sPods([]byte("not json"))
	require.NotNil(t, health)
	assert.Empty(t, health.Services)
}

func TestParseK8sPods_AllHealthy(t *testing.T) {
	input := []byte(`{
		"items": [
			{
				"metadata": {"name": "supabase-db-0", "labels": {"app.kubernetes.io/name": "supabase-db"}},
				"status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "True"}]}
			},
			{
				"metadata": {"name": "supabase-auth-abc", "labels": {"app.kubernetes.io/name": "supabase-auth"}},
				"status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "True"}]}
			},
			{
				"metadata": {"name": "supabase-rest-xyz", "labels": {"app.kubernetes.io/name": "supabase-rest"}},
				"status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "True"}]}
			}
		]
	}`)

	health := parseK8sPods(input)
	require.Len(t, health.Services, 3)
	assert.Equal(t, domain.ServiceStatusHealthy, health.Services[domain.ServiceDB].Status)
	assert.Equal(t, domain.ServiceStatusHealthy, health.Services[domain.ServiceAuth].Status)
	assert.Equal(t, domain.ServiceStatusHealthy, health.Services[domain.ServiceRest].Status)
}

func TestParseK8sPods_MixedStatus(t *testing.T) {
	input := []byte(`{
		"items": [
			{
				"metadata": {"name": "supabase-db-0", "labels": {"app.kubernetes.io/name": "supabase-db"}},
				"status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "True"}]}
			},
			{
				"metadata": {"name": "supabase-auth-abc", "labels": {"app.kubernetes.io/name": "supabase-auth"}},
				"status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "False", "reason": "ContainersNotReady"}]}
			},
			{
				"metadata": {"name": "supabase-rest-xyz", "labels": {"app.kubernetes.io/name": "supabase-rest"}},
				"status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "False", "reason": "SomeOtherReason"}]}
			}
		]
	}`)

	health := parseK8sPods(input)
	require.Len(t, health.Services, 3)
	assert.Equal(t, domain.ServiceStatusHealthy, health.Services[domain.ServiceDB].Status)
	assert.Equal(t, domain.ServiceStatusStarting, health.Services[domain.ServiceAuth].Status)
	assert.Equal(t, domain.ServiceStatusUnhealthy, health.Services[domain.ServiceRest].Status)
}

func TestParseK8sPods_PendingPod(t *testing.T) {
	input := []byte(`{
		"items": [{
			"metadata": {"name": "supabase-db-0", "labels": {"app.kubernetes.io/name": "supabase-db"}},
			"status": {"phase": "Pending"}
		}]
	}`)

	health := parseK8sPods(input)
	require.Len(t, health.Services, 1)
	assert.Equal(t, domain.ServiceStatusStarting, health.Services[domain.ServiceDB].Status)
}

func TestParseK8sPods_FailedPod(t *testing.T) {
	input := []byte(`{
		"items": [{
			"metadata": {"name": "supabase-auth-abc", "labels": {"app.kubernetes.io/name": "supabase-auth"}},
			"status": {"phase": "Failed"}
		}]
	}`)

	health := parseK8sPods(input)
	require.Len(t, health.Services, 1)
	assert.Equal(t, domain.ServiceStatusUnhealthy, health.Services[domain.ServiceAuth].Status)
}

func TestParseK8sPods_UnknownLabel(t *testing.T) {
	input := []byte(`{
		"items": [{
			"metadata": {"name": "nginx-pod", "labels": {"app.kubernetes.io/name": "nginx"}},
			"status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "True"}]}
		}]
	}`)

	health := parseK8sPods(input)
	require.NotNil(t, health)
	assert.Empty(t, health.Services)
}

func TestParseK8sPods_NoReadyCondition(t *testing.T) {
	input := []byte(`{
		"items": [{
			"metadata": {"name": "supabase-db-0", "labels": {"app.kubernetes.io/name": "supabase-db"}},
			"status": {"phase": "Running", "conditions": [{"type": "Initialized", "status": "True"}]}
		}]
	}`)

	health := parseK8sPods(input)
	require.Len(t, health.Services, 1)
	assert.Equal(t, domain.ServiceStatusUnknown, health.Services[domain.ServiceDB].Status)
}

func TestParseK8sPods_ReadyStatusUnknown(t *testing.T) {
	input := []byte(`{
		"items": [{
			"metadata": {"name": "supabase-db-0", "labels": {"app.kubernetes.io/name": "supabase-db"}},
			"status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "Unknown"}]}
		}]
	}`)

	health := parseK8sPods(input)
	require.Len(t, health.Services, 1)
	assert.Equal(t, domain.ServiceStatusUnknown, health.Services[domain.ServiceDB].Status)
}

func TestParseK8sPods_SucceededPhase(t *testing.T) {
	input := []byte(`{
		"items": [{
			"metadata": {"name": "supabase-functions-job", "labels": {"app.kubernetes.io/name": "supabase-functions"}},
			"status": {"phase": "Succeeded"}
		}]
	}`)

	health := parseK8sPods(input)
	require.Len(t, health.Services, 1)
	assert.Equal(t, domain.ServiceStatusStopped, health.Services[domain.ServiceFunctions].Status)
}

func TestParseK8sPods_UnknownPhase(t *testing.T) {
	input := []byte(`{
		"items": [{
			"metadata": {"name": "supabase-db-0", "labels": {"app.kubernetes.io/name": "supabase-db"}},
			"status": {"phase": "Unknown"}
		}]
	}`)

	health := parseK8sPods(input)
	require.Len(t, health.Services, 1)
	assert.Equal(t, domain.ServiceStatusUnknown, health.Services[domain.ServiceDB].Status)
}

func TestParseK8sPods_EmptyPhase(t *testing.T) {
	input := []byte(`{
		"items": [{
			"metadata": {"name": "supabase-db-0", "labels": {"app.kubernetes.io/name": "supabase-db"}},
			"status": {"phase": ""}
		}]
	}`)

	health := parseK8sPods(input)
	require.Len(t, health.Services, 1)
	assert.Equal(t, domain.ServiceStatusUnknown, health.Services[domain.ServiceDB].Status)
}

func TestParseK8sPods_DuplicateService_LastWins(t *testing.T) {
	input := []byte(`{
		"items": [
			{
				"metadata": {"name": "supabase-db-old", "labels": {"app.kubernetes.io/name": "supabase-db"}},
				"status": {"phase": "Failed"}
			},
			{
				"metadata": {"name": "supabase-db-new", "labels": {"app.kubernetes.io/name": "supabase-db"}},
				"status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "True"}]}
			}
		]
	}`)

	health := parseK8sPods(input)
	require.Len(t, health.Services, 1)
	assert.Equal(t, domain.ServiceStatusHealthy, health.Services[domain.ServiceDB].Status)
	assert.Equal(t, "supabase-db-new", health.Services[domain.ServiceDB].Message)
}
