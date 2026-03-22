package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Sentinel errors for project model operations.
var (
	ErrProjectNotFound      = errors.New("project not found")
	ErrProjectAlreadyExists = errors.New("project with this slug already exists")
	ErrInvalidSlug          = errors.New("invalid project slug")
	ErrReservedSlug         = errors.New("slug is reserved")
	ErrInvalidTransition    = errors.New("invalid status transition")
	ErrInvalidDisplayName   = errors.New("invalid display name")
	ErrCannotNormalize      = errors.New("cannot normalize slug: result is too short")
)

// reservedSlugs is the list of slugs that cannot be used as project names.
var reservedSlugs = []string{
	"supabase", "control-plane", "default", "system", "admin",
	"api", "web", "app", "internal", "global",
}

// slugRe matches valid project slugs: lowercase alphanumeric and hyphens,
// 3–40 chars, no leading/trailing/consecutive hyphens.
var slugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// consecutiveHyphens matches two or more consecutive hyphens.
var consecutiveHyphens = regexp.MustCompile(`-{2,}`)

// ProjectModel represents the core identity and metadata of a Supabase project.
// It does not contain runtime-specific information.
type ProjectModel struct {
	// Slug is the unique project identifier used for directory names and compose project names.
	// Format: lowercase alphanumeric and hyphens, 3–40 characters, no leading/trailing hyphens.
	Slug string

	// DisplayName is the human-readable project name (non-empty after trim, max 100 runes).
	DisplayName string

	// Status is the current lifecycle state of the project.
	Status ProjectStatus

	// PreviousStatus records the last valid state before entering the error state.
	// Used to determine the error recovery path (RetryCreate vs RetryStart).
	// Meaningless when Status is not error.
	PreviousStatus ProjectStatus

	// LastError records the reason for the most recent transition to the error state.
	LastError string

	// CreatedAt is the project creation time, set by NewProject to time.Now().UTC().
	CreatedAt time.Time

	// UpdatedAt is the last update time, updated by TransitionTo or SetError.
	UpdatedAt time.Time

	// Health is the service health information, populated by the runtime adapter.
	// Only meaningful when Status is running; nil otherwise.
	Health *ProjectHealth
}

// TransitionError is returned when an invalid state transition is attempted.
// Supports errors.Is(err, ErrInvalidTransition) and errors.As(err, &te).
type TransitionError struct {
	From    ProjectStatus
	To      ProjectStatus
	Message string
}

// Error implements the error interface. Format: `invalid transition from "X" to "Y": reason`
func (e *TransitionError) Error() string {
	return fmt.Sprintf("invalid transition from %q to %q: %s", e.From, e.To, e.Message)
}

// Unwrap returns ErrInvalidTransition so that errors.Is(err, ErrInvalidTransition) is true.
func (e *TransitionError) Unwrap() error {
	return ErrInvalidTransition
}

// validTransitions defines all legal (from → to) transitions as a set.
// The error recovery paths (error → creating, error → starting) require additional
// checking of PreviousStatus and are handled separately in TransitionTo.
var validTransitions = map[ProjectStatus]map[ProjectStatus]bool{
	StatusCreating: {
		StatusStopped: true,
		StatusError:   true,
	},
	StatusStopped: {
		StatusStarting:  true,
		StatusDestroyed: true,
		StatusError:     true,
	},
	StatusStarting: {
		StatusRunning: true,
		StatusError:   true,
	},
	StatusRunning: {
		StatusStopping: true,
		StatusError:    true,
	},
	StatusStopping: {
		StatusStopped: true,
		StatusError:   true,
	},
	StatusError: {
		StatusCreating:  true, // only when PreviousStatus == creating
		StatusStarting:  true, // only when PreviousStatus ∈ {starting, running, stopping}
		StatusDestroyed: true,
		StatusError:     true, // re-entry (no PreviousStatus update)
	},
}

// ValidTransition checks whether a transition from → to is legal.
// previousStatus is only consulted when from == StatusError and to ∈ {creating, starting}.
func ValidTransition(from, to ProjectStatus, previousStatus ProjectStatus) bool {
	targets, ok := validTransitions[from]
	if !ok || !targets[to] {
		return false
	}
	if from == StatusError {
		switch to {
		case StatusCreating:
			return previousStatus == StatusCreating
		case StatusStarting:
			return previousStatus == StatusStarting ||
				previousStatus == StatusRunning ||
				previousStatus == StatusStopping
		}
	}
	return true
}

// NewProject creates a new ProjectModel with status creating.
// Validates slug (including reserved names) and displayName, and sets timestamps.
// displayName is trimmed before validation (must be non-empty, max 100 runes).
func NewProject(slug, displayName string) (*ProjectModel, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(displayName)
	if name == "" {
		return nil, fmt.Errorf("%w: must not be empty", ErrInvalidDisplayName)
	}
	if utf8.RuneCountInString(name) > 100 {
		return nil, fmt.Errorf("%w: must not exceed 100 characters", ErrInvalidDisplayName)
	}
	now := time.Now().UTC()
	return &ProjectModel{
		Slug:        slug,
		DisplayName: name,
		Status:      StatusCreating,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// TransitionTo attempts to transition the project to the target state.
// On success it updates Status and UpdatedAt.
// PreviousStatus is updated only when target == StatusError and current Status != StatusError.
// For transitioning to error, prefer SetError so that LastError is also updated.
func (p *ProjectModel) TransitionTo(target ProjectStatus) error {
	if !ValidTransition(p.Status, target, p.PreviousStatus) {
		return &TransitionError{
			From:    p.Status,
			To:      target,
			Message: "not a valid lifecycle transition",
		}
	}
	if target == StatusError && p.Status != StatusError {
		p.PreviousStatus = p.Status
	}
	p.Status = target
	p.UpdatedAt = time.Now().UTC()
	return nil
}

// SetError is the canonical path for entering the error state.
// It records the reason in LastError.
// If already in error (e.g. a second crash), only LastError and UpdatedAt are updated;
// PreviousStatus is left unchanged.
// Returns *TransitionError if the current state does not permit entering error.
func (p *ProjectModel) SetError(reason string) error {
	if p.Status == StatusError {
		// Already in error — update message only.
		p.LastError = reason
		p.UpdatedAt = time.Now().UTC()
		return nil
	}
	if err := p.TransitionTo(StatusError); err != nil {
		return err
	}
	p.LastError = reason
	return nil
}

// IsTerminal returns true if the project is in the destroyed terminal state.
func (p *ProjectModel) IsTerminal() bool {
	return p.Status == StatusDestroyed
}

// CanStart returns true if the project can be started.
// A project can start from stopped, or from error when a retry-start is possible.
func (p *ProjectModel) CanStart() bool {
	if p.Status == StatusStopped {
		return true
	}
	if p.Status == StatusError {
		prev := p.PreviousStatus
		return prev == StatusStarting || prev == StatusRunning || prev == StatusStopping
	}
	return false
}

// CanStop returns true if the project is currently running.
func (p *ProjectModel) CanStop() bool {
	return p.Status == StatusRunning
}

// CanDestroy returns true if the project can be destroyed (stopped or error).
func (p *ProjectModel) CanDestroy() bool {
	return p.Status == StatusStopped || p.Status == StatusError
}

// CanRetryCreate returns true if the project is in error and the previous status was creating.
func (p *ProjectModel) CanRetryCreate() bool {
	return p.Status == StatusError && p.PreviousStatus == StatusCreating
}

// ValidateSlug checks whether a slug meets all naming rules.
// Rules: 3–40 chars, lowercase alphanumeric + hyphens, no leading/trailing/consecutive hyphens,
// not a reserved name.
func ValidateSlug(slug string) error {
	length := len(slug)
	if length < 3 || length > 40 {
		return fmt.Errorf("%w: must be 3–40 characters", ErrInvalidSlug)
	}
	if !slugRe.MatchString(slug) {
		return fmt.Errorf("%w: must match ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$", ErrInvalidSlug)
	}
	if consecutiveHyphens.MatchString(slug) {
		return fmt.Errorf("%w: must not contain consecutive hyphens", ErrInvalidSlug)
	}
	if IsReservedSlug(slug) {
		return fmt.Errorf("%w: %q", ErrReservedSlug, slug)
	}
	return nil
}

// NormalizeSlug converts arbitrary input into a valid slug candidate.
// Steps: lowercase → spaces/underscores to hyphens → strip non-[a-z0-9-] →
// collapse consecutive hyphens → trim leading/trailing hyphens → truncate to 40.
// Returns ErrCannotNormalize if the result is shorter than 3 characters.
// Reserved names are not checked; callers must call ValidateSlug if needed.
func NormalizeSlug(input string) (string, error) {
	s := strings.ToLower(input)
	// Replace spaces and underscores with hyphens.
	s = strings.NewReplacer(" ", "-", "_", "-").Replace(s)
	// Remove non-[a-z0-9-] characters.
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	s = b.String()
	// Collapse consecutive hyphens.
	s = consecutiveHyphens.ReplaceAllString(s, "-")
	// Trim leading and trailing hyphens.
	s = strings.Trim(s, "-")
	// Truncate to 40 characters, removing any trailing hyphen introduced by truncation.
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	if len(s) < 3 {
		return "", ErrCannotNormalize
	}
	return s, nil
}

// IsReservedSlug returns true if the given slug is a system-reserved name.
func IsReservedSlug(slug string) bool {
	for _, r := range reservedSlugs {
		if slug == r {
			return true
		}
	}
	return false
}
