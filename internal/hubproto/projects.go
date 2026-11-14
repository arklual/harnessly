package hubproto

// The registry of a machine's projects and workspaces: the runtime tells its agent about each change, the agent
// passes it to the hubs that subscribed, and the hub keeps one row per project in its database. The «Проекты» sidebar
// then reads the hub, so the projects of a machine that sleeps are still there. Names and folders, no files.

const (
	// EventProjects carries {"event": "projects", "projects": ProjectsEvent}.
	EventProjects = "projects"
	// FeatureProjects is in `hello` when the agent sends the registry's events.
	FeatureProjects = "projects-registry"

	// ProjectsFull is the machine's whole list, sent when a link comes up: the hub replaces what it has.
	ProjectsFull = "projects.full"
	// ProjectsCreated, ProjectsRenamed and ProjectsPath carry one Project; ProjectsDeleted names it by id.
	ProjectsCreated = "project.created"
	ProjectsRenamed = "project.renamed"
	ProjectsPath    = "project.path"
	ProjectsDeleted = "project.deleted"
)

// Project is a workspace of the machine: its id, name and folders.
type Project struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Directories []string `json:"directories,omitempty"`
}

// ProjectsEvent is a change of the registry: Kind is one of the Projects* constants; Projects is set for ProjectsFull,
// Project for the others.
type ProjectsEvent struct {
	Kind     string    `json:"kind"`
	Project  *Project  `json:"project,omitempty"`
	Projects []Project `json:"projects,omitempty"`
}

// Limits of what the hub keeps of one project.
const (
	MaxProjects       = 500
	MaxProjectDirs    = 16
	MaxProjectNameLen = 200
	MaxProjectDirLen  = 1024
)
