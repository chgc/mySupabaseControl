package domain

// ProjectStatus represents the lifecycle state of a Supabase project.
type ProjectStatus string

const (
	StatusCreating  ProjectStatus = "creating"
	StatusStopped   ProjectStatus = "stopped"
	StatusStarting  ProjectStatus = "starting"
	StatusRunning   ProjectStatus = "running"
	StatusStopping  ProjectStatus = "stopping"
	StatusDestroying ProjectStatus = "destroying"
	StatusDestroyed ProjectStatus = "destroyed"
	StatusError     ProjectStatus = "error"
)
