package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

// ─── NewProject ──────────────────────────────────────────────────────────────

func TestNewProject(t *testing.T) {
	t.Run("valid creates project with creating status", func(t *testing.T) {
		before := time.Now().UTC()
		p, err := domain.NewProject("my-project", "My Project", domain.RuntimeDockerCompose)
		require.NoError(t, err)
		assert.Equal(t, "my-project", p.Slug)
		assert.Equal(t, "My Project", p.DisplayName)
		assert.Equal(t, domain.StatusCreating, p.Status)
		assert.False(t, p.CreatedAt.Before(before))
		assert.False(t, p.UpdatedAt.Before(before))
	})

	t.Run("trims whitespace from displayName", func(t *testing.T) {
		p, err := domain.NewProject("abc", "  hello  ", domain.RuntimeDockerCompose)
		require.NoError(t, err)
		assert.Equal(t, "hello", p.DisplayName)
	})

	t.Run("empty slug returns error", func(t *testing.T) {
		_, err := domain.NewProject("", "Name", domain.RuntimeDockerCompose)
		require.Error(t, err)
	})

	t.Run("empty displayName returns ErrInvalidDisplayName", func(t *testing.T) {
		_, err := domain.NewProject("abc", "", domain.RuntimeDockerCompose)
		require.ErrorIs(t, err, domain.ErrInvalidDisplayName)
	})

	t.Run("whitespace-only displayName returns ErrInvalidDisplayName", func(t *testing.T) {
		_, err := domain.NewProject("abc", "   ", domain.RuntimeDockerCompose)
		require.ErrorIs(t, err, domain.ErrInvalidDisplayName)
	})

	t.Run("displayName over 100 runes returns ErrInvalidDisplayName", func(t *testing.T) {
		long := strings.Repeat("あ", 101) // each 'あ' is 1 rune
		_, err := domain.NewProject("abc", long, domain.RuntimeDockerCompose)
		require.ErrorIs(t, err, domain.ErrInvalidDisplayName)
	})

	t.Run("valid kubernetes runtime", func(t *testing.T) {
		p, err := domain.NewProject("k8s-app", "K8s App", domain.RuntimeKubernetes)
		require.NoError(t, err)
		assert.Equal(t, domain.RuntimeKubernetes, p.RuntimeType)
	})

	t.Run("valid docker-compose runtime", func(t *testing.T) {
		p, err := domain.NewProject("dc-app", "DC App", domain.RuntimeDockerCompose)
		require.NoError(t, err)
		assert.Equal(t, domain.RuntimeDockerCompose, p.RuntimeType)
	})

	t.Run("invalid runtime type returns ErrInvalidRuntimeType", func(t *testing.T) {
		_, err := domain.NewProject("abc", "Name", domain.RuntimeType("invalid"))
		require.ErrorIs(t, err, domain.ErrInvalidRuntimeType)
	})

	t.Run("empty runtime type returns ErrInvalidRuntimeType", func(t *testing.T) {
		_, err := domain.NewProject("abc", "Name", domain.RuntimeType(""))
		require.ErrorIs(t, err, domain.ErrInvalidRuntimeType)
	})
}

// ─── ValidateSlug ─────────────────────────────────────────────────────────────

func TestValidateSlug(t *testing.T) {
	valid := []string{
		"abc",
		"my-project",
		"project123",
		"a1b",
		strings.Repeat("a", 40), // exactly 40 chars
	}
	for _, slug := range valid {
		t.Run("valid:"+slug, func(t *testing.T) {
			assert.NoError(t, domain.ValidateSlug(slug))
		})
	}

	invalid := []struct {
		slug string
		want error
	}{
		{"ab", domain.ErrInvalidSlug},                         // too short
		{strings.Repeat("a", 41), domain.ErrInvalidSlug},     // too long
		{"my.project", domain.ErrInvalidSlug},                 // dot
		{"my/project", domain.ErrInvalidSlug},                 // slash
		{"-abc", domain.ErrInvalidSlug},                       // leading hyphen
		{"abc-", domain.ErrInvalidSlug},                       // trailing hyphen
		{"my--project", domain.ErrInvalidSlug},                // consecutive hyphens
		{"UPPER", domain.ErrInvalidSlug},                      // uppercase
		{"api", domain.ErrReservedSlug},                       // reserved
		{"admin", domain.ErrReservedSlug},                     // reserved
	}
	for _, tc := range invalid {
		tc := tc
		t.Run("invalid:"+tc.slug, func(t *testing.T) {
			err := domain.ValidateSlug(tc.slug)
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

// ─── NormalizeSlug ────────────────────────────────────────────────────────────

func TestNormalizeSlug(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello-world"},
		{"MY_PROJECT", "my-project"},
		{"my--project", "my-project"},       // collapse consecutive hyphens
		{"-my-project-", "my-project"},      // trim leading/trailing hyphens
		{"My.Cool/Project", "mycoolproject"}, // strip illegal chars
		{strings.Repeat("a", 45), strings.Repeat("a", 40)}, // truncate to 40
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			got, err := domain.NormalizeSlug(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	tooShort := []string{"!!", "  ", ".."}
	for _, s := range tooShort {
		s := s
		t.Run("too-short:"+s, func(t *testing.T) {
			_, err := domain.NormalizeSlug(s)
			assert.ErrorIs(t, err, domain.ErrCannotNormalize)
		})
	}
}

// ─── TransitionTo and ValidTransition ────────────────────────────────────────

func TestTransitionTo(t *testing.T) {
	type tc struct {
		from, to       domain.ProjectStatus
		previous       domain.ProjectStatus
		expectErr      bool
	}
	cases := []tc{
		// Legal transitions
		{domain.StatusCreating, domain.StatusStopped, "", false},
		{domain.StatusCreating, domain.StatusError, "", false},
		{domain.StatusStopped, domain.StatusStarting, "", false},
		{domain.StatusStopped, domain.StatusDestroying, "", false},
		{domain.StatusStopped, domain.StatusError, "", false},
		{domain.StatusStarting, domain.StatusRunning, "", false},
		{domain.StatusStarting, domain.StatusError, "", false},
		{domain.StatusRunning, domain.StatusStopping, "", false},
		{domain.StatusRunning, domain.StatusError, "", false},
		{domain.StatusStopping, domain.StatusStopped, "", false},
		{domain.StatusStopping, domain.StatusError, "", false},
		{domain.StatusDestroying, domain.StatusDestroyed, "", false},
		{domain.StatusDestroying, domain.StatusError, "", false},
		{domain.StatusError, domain.StatusCreating, domain.StatusCreating, false},
		{domain.StatusError, domain.StatusStarting, domain.StatusStarting, false},
		{domain.StatusError, domain.StatusDestroying, "", false},
		{domain.StatusError, domain.StatusError, "", false}, // re-entry

		// Illegal transitions
		{domain.StatusCreating, domain.StatusRunning, "", true},
		{domain.StatusStopped, domain.StatusDestroyed, "", true},
		{domain.StatusRunning, domain.StatusStopped, "", true},
		{domain.StatusDestroyed, domain.StatusCreating, "", true},
		{domain.StatusError, domain.StatusCreating, domain.StatusStopped, true}, // wrong previous
		{domain.StatusError, domain.StatusStarting, domain.StatusCreating, true}, // wrong previous
	}

	for _, c := range cases {
		c := c
		name := string(c.from) + "→" + string(c.to)
		t.Run(name, func(t *testing.T) {
			p, err := domain.NewProject("test-slug", "Test", domain.RuntimeDockerCompose)
			require.NoError(t, err)
			p.Status = c.from
			p.PreviousStatus = c.previous

			err = p.TransitionTo(c.to)
			if c.expectErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, domain.ErrInvalidTransition)
				var te *domain.TransitionError
				require.ErrorAs(t, err, &te)
				assert.Equal(t, c.from, te.From)
				assert.Equal(t, c.to, te.To)
			} else {
				require.NoError(t, err)
				assert.Equal(t, c.to, p.Status)
			}
		})
	}
}

func TestTransitionTo_ErrorReEntryPreservePreviousStatus(t *testing.T) {
	p, err := domain.NewProject("test-slug", "Test", domain.RuntimeDockerCompose)
	require.NoError(t, err)
	p.Status = domain.StatusRunning

	// First error — PreviousStatus should be set to Running.
	require.NoError(t, p.TransitionTo(domain.StatusError))
	assert.Equal(t, domain.StatusRunning, p.PreviousStatus)

	// Re-entry into error — PreviousStatus must NOT be overwritten.
	require.NoError(t, p.TransitionTo(domain.StatusError))
	assert.Equal(t, domain.StatusRunning, p.PreviousStatus, "PreviousStatus must not be overwritten on error re-entry")
}

// ─── SetError ────────────────────────────────────────────────────────────────

func TestSetError(t *testing.T) {
	t.Run("transitions to error and records reason", func(t *testing.T) {
		p, _ := domain.NewProject("abc", "Test", domain.RuntimeDockerCompose)
		p.Status = domain.StatusRunning
		require.NoError(t, p.SetError("boom"))
		assert.Equal(t, domain.StatusError, p.Status)
		assert.Equal(t, domain.StatusRunning, p.PreviousStatus)
		assert.Equal(t, "boom", p.LastError)
	})

	t.Run("already in error updates only LastError", func(t *testing.T) {
		p, _ := domain.NewProject("abc", "Test", domain.RuntimeDockerCompose)
		p.Status = domain.StatusError
		p.PreviousStatus = domain.StatusStarting
		p.LastError = "first"
		require.NoError(t, p.SetError("second"))
		assert.Equal(t, domain.StatusError, p.Status)
		assert.Equal(t, domain.StatusStarting, p.PreviousStatus)
		assert.Equal(t, "second", p.LastError)
	})

	t.Run("cannot enter error from destroyed", func(t *testing.T) {
		p, _ := domain.NewProject("abc", "Test", domain.RuntimeDockerCompose)
		p.Status = domain.StatusDestroyed
		err := p.SetError("oops")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidTransition)
	})
}

// ─── Status predicate helpers ────────────────────────────────────────────────

func TestStatusPredicates(t *testing.T) {
	type row struct {
		status         domain.ProjectStatus
		previousStatus domain.ProjectStatus
		canStart       bool
		canStop        bool
		canDestroy     bool
		canRetry       bool
		isTerminal     bool
	}
	cases := []row{
		{domain.StatusCreating, "", false, false, false, false, false},
		{domain.StatusStopped, "", true, false, true, false, false},   // stopped: canStart=true per spec
		{domain.StatusStarting, "", false, false, false, false, false},
		{domain.StatusRunning, "", false, true, false, false, false},
		{domain.StatusStopping, "", false, false, false, false, false},
		{domain.StatusDestroying, "", false, false, true, false, false}, // retryable destroy
		{domain.StatusDestroyed, "", false, false, false, false, true},
		// Error — previous=creating → canRetry
		{domain.StatusError, domain.StatusCreating, false, false, true, true, false},
		// Error — previous=starting → canStart retry
		{domain.StatusError, domain.StatusStarting, true, false, true, false, false},
		// Error — previous=running → canStart retry
		{domain.StatusError, domain.StatusRunning, true, false, true, false, false},
	}
	for _, c := range cases {
		c := c
		t.Run(string(c.status)+"/"+string(c.previousStatus), func(t *testing.T) {
			p, _ := domain.NewProject("abc", "Test", domain.RuntimeDockerCompose)
			p.Status = c.status
			p.PreviousStatus = c.previousStatus
			assert.Equal(t, c.canStart, p.CanStart(), "CanStart")
			assert.Equal(t, c.canStop, p.CanStop(), "CanStop")
			assert.Equal(t, c.canDestroy, p.CanDestroy(), "CanDestroy")
			assert.Equal(t, c.canRetry, p.CanRetryCreate(), "CanRetryCreate")
			assert.Equal(t, c.isTerminal, p.IsTerminal(), "IsTerminal")
		})
	}
}

// ─── TransitionError ─────────────────────────────────────────────────────────

func TestTransitionError_Semantics(t *testing.T) {
	p, _ := domain.NewProject("abc", "Test", domain.RuntimeDockerCompose)
	p.Status = domain.StatusDestroyed
	err := p.TransitionTo(domain.StatusRunning)
	require.Error(t, err)

	assert.ErrorIs(t, err, domain.ErrInvalidTransition)

	var te *domain.TransitionError
	require.ErrorAs(t, err, &te)
	assert.Equal(t, domain.StatusDestroyed, te.From)
	assert.Equal(t, domain.StatusRunning, te.To)
	assert.Contains(t, err.Error(), "destroyed")
	assert.Contains(t, err.Error(), "running")
}

// ─── ProjectHealth ────────────────────────────────────────────────────────────

func TestProjectHealth_IsHealthy(t *testing.T) {
	t.Run("nil receiver returns false", func(t *testing.T) {
		var h *domain.ProjectHealth
		assert.False(t, h.IsHealthy())
	})

	t.Run("empty services map returns false", func(t *testing.T) {
		h := &domain.ProjectHealth{Services: map[domain.ServiceName]domain.ServiceHealth{}}
		assert.False(t, h.IsHealthy())
	})

	t.Run("all healthy returns true", func(t *testing.T) {
		h := &domain.ProjectHealth{
			Services: map[domain.ServiceName]domain.ServiceHealth{
				domain.ServiceDB:   {Status: domain.ServiceStatusHealthy},
				domain.ServiceAuth: {Status: domain.ServiceStatusHealthy},
			},
		}
		assert.True(t, h.IsHealthy())
	})

	t.Run("any unhealthy returns false", func(t *testing.T) {
		h := &domain.ProjectHealth{
			Services: map[domain.ServiceName]domain.ServiceHealth{
				domain.ServiceDB:   {Status: domain.ServiceStatusHealthy},
				domain.ServiceAuth: {Status: domain.ServiceStatusUnhealthy},
			},
		}
		assert.False(t, h.IsHealthy())
	})
}

func TestAllServices(t *testing.T) {
	services := domain.AllServices()
	assert.Len(t, services, 13)

	// Verify order: db is first, supavisor is last.
	assert.Equal(t, domain.ServiceDB, services[0])
	assert.Equal(t, domain.ServiceSupavisor, services[12])

	// Verify no duplicates.
	seen := make(map[domain.ServiceName]struct{}, len(services))
	for _, s := range services {
		_, dup := seen[s]
		assert.False(t, dup, "duplicate service: %s", s)
		seen[s] = struct{}{}
	}
}

// ─── errors package check ────────────────────────────────────────────────────

func TestTransitionError_IsUnwrap(t *testing.T) {
	te := &domain.TransitionError{From: domain.StatusStopped, To: domain.StatusRunning}
	assert.True(t, errors.Is(te, domain.ErrInvalidTransition))
}

// ─── ValidateRuntimeType ─────────────────────────────────────────────────────

func TestValidateRuntimeType(t *testing.T) {
	t.Run("docker-compose is valid", func(t *testing.T) {
		assert.NoError(t, domain.ValidateRuntimeType(domain.RuntimeDockerCompose))
	})
	t.Run("kubernetes is valid", func(t *testing.T) {
		assert.NoError(t, domain.ValidateRuntimeType(domain.RuntimeKubernetes))
	})
	t.Run("empty string is invalid", func(t *testing.T) {
		err := domain.ValidateRuntimeType(domain.RuntimeType(""))
		assert.ErrorIs(t, err, domain.ErrInvalidRuntimeType)
	})
	t.Run("unknown value is invalid", func(t *testing.T) {
		err := domain.ValidateRuntimeType(domain.RuntimeType("podman"))
		assert.ErrorIs(t, err, domain.ErrInvalidRuntimeType)
	})
}

// ─── K8sNamespace ────────────────────────────────────────────────────────────

func TestK8sNamespace(t *testing.T) {
	p, err := domain.NewProject("my-app", "My App", domain.RuntimeKubernetes)
	require.NoError(t, err)
	assert.Equal(t, "supabase-my-app", p.K8sNamespace())
}

func TestK8sNamespace_MaxLength(t *testing.T) {
	slug := strings.Repeat("a", 40) // max slug length
	p, err := domain.NewProject(slug, "Test", domain.RuntimeKubernetes)
	require.NoError(t, err)
	ns := p.K8sNamespace()
	assert.Equal(t, "supabase-"+slug, ns)
	assert.LessOrEqual(t, len(ns), 63, "namespace must fit K8s 63-char limit")
}
