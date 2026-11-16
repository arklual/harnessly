package design

import (
	"errors"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// A mockup of a chat's feed (an ```html answer) continued in Sinopia Design: the button of the chat's card
// makes a board of it, in the chat's project when the person may edit it, else in a new project, and the same mockup
// of the same chat is that board ever after.

// MockupSpec is what the card sends: the chat, the mockup and its title, and the projects that are the chat's, first
// the one fittest (the one open beside the chat, the one the chat was started from, the one its last mockup went to).
type MockupSpec struct {
	Conv, HTML, Title string
	// Person is the address of the person whose page asked ("" the runtime's own): a project is used only when they may
	// edit it, and a new one is theirs.
	Person     string
	Candidates []string
	// Fits says a project can take a board (not a deck or a video); nil takes any.
	Fits func(project string) bool
}

// MockupBoard is the board a mockup became.
type MockupBoard struct {
	Project      string `json:"project"`
	ProjectTitle string `json:"projectTitle"`
	Page         string `json:"page"`
	File         string `json:"file"`
	Title        string `json:"title"`
	// NewProject: the project was made for this mockup.
	NewProject bool `json:"newProject,omitempty"`
	// Existing: the mockup had been made a board before; nothing was made now.
	Existing bool `json:"existing,omitempty"`
}

// MockupStore keeps which board each mockup became, by MockupKey; the runtime keeps it in its data dir.
type MockupStore interface {
	Mockup(key string) (MockupBoard, bool)
	SetMockup(key string, b MockupBoard) error
}

// ErrMockupEmpty: the mockup has nothing to make a board of.
var ErrMockupEmpty = errors.New("empty mockup")

// mockupTitleMax bounds a title taken from the chat (a heading or the answer's first line).
const mockupTitleMax = 80

// BoardOfMockup is the board of the mockup: the one it became before, while that board is there and the person still
// sees its project, else a new page of the fittest candidate the person may edit, else a page of a new project of
// theirs. A board made is recorded before the answer, so a second press finds it.
func (l *Library) BoardOfMockup(s MockupSpec, store MockupStore) (MockupBoard, error) {
	if jscompat.Trim(s.HTML) == "" {
		return MockupBoard{}, ErrMockupEmpty
	}
	key := MockupKey(s.Conv, s.HTML)
	// Two presses at once (two devices, a double tap) make one board.
	mu := l.lockOf("mockup\x00" + key)
	mu.Lock()
	defer mu.Unlock()
	if was, ok := store.Mockup(key); ok && l.mockupStands(was, s.Person) {
		was.Existing, was.NewProject = true, false
		if a := l.ReadArtifact(was.Project); a != nil && a.Title != "" {
			was.ProjectTitle = a.Title
		}
		return was, nil
	}
	title := mockupTitle(s.Title)
	project, projectTitle, made := "", "", false
	for _, c := range s.Candidates {
		if !IsLibraryID(c) || l.ReadModel(c) == nil || !l.CanFor(c, RoleEdit, s.Person) || (s.Fits != nil && !s.Fits(c)) {
			continue
		}
		project = c
		if a := l.ReadArtifact(c); a != nil {
			projectTitle = a.Title
		}
		break
	}
	if project == "" {
		p, err := l.CreateProject(title)
		if err != nil {
			return MockupBoard{}, err
		}
		l.AnnounceNewBy(p.ID, s.Person)
		project, projectTitle, made = p.ID, p.Title, true
	}
	page, err := l.CreatePage(project, PageSpec{Title: title, Seed: BoardFromHTML(s.HTML)})
	if err != nil {
		return MockupBoard{}, err
	}
	out := MockupBoard{Project: project, ProjectTitle: projectTitle, Page: page.ID, File: page.File, Title: page.Title, NewProject: made}
	if err := store.SetMockup(key, out); err != nil {
		l.warn("design: which board a mockup became was not kept", err)
	}
	return out, nil
}

// mockupStands: the board a mockup became is still a page of its project, and the person still sees the project.
func (l *Library) mockupStands(b MockupBoard, person string) bool {
	if !IsLibraryID(b.Project) || !l.CanFor(b.Project, RoleView, person) {
		return false
	}
	list, err := l.ListPages(b.Project)
	if err != nil {
		return false
	}
	for _, p := range list.Pages {
		if p.ID == b.Page {
			return true
		}
	}
	return false
}

// mockupTitle is the title of a mockup's board and of its project: the chat's words, one line, bounded.
func mockupTitle(t string) string {
	t = jscompat.Trim(spaceRun.ReplaceAllString(boardTitleControls.ReplaceAllString(t, " "), " "))
	if t == "" {
		return locale.Pick("Mockup from the chat", "Макет из чата")
	}
	return jscompat.Slice(t, mockupTitleMax)
}

// MockupNote is what the agent of the chat reads under the person's words when the design comes with the board a
// mockup of the chat became (the field's chip of the press): the mockup is that board now, and the next
// changes go to it, not to a new ```html answer. A board that is not a page of the project says nothing.
func (l *Library) MockupNote(a *Artifact, board string) string {
	if a == nil || board == "" {
		return ""
	}
	list, err := l.ListPages(a.ID)
	if err != nil {
		return ""
	}
	for _, p := range list.Pages {
		if p.File != board {
			continue
		}
		cli, title, file := l.cfg.Texts.CLI, plain(p.Title, 120), filepath.Join(l.ArtifactDir(a.ID), ProjectDirname, p.File)
		if !locale.Russian() {
			return " The HTML mockup of this chat is now the board \"" + title + "\" (" + file + ") of the project \"" + plain(a.Title, 120) +
				"\" in Sinopia Design: make the changes the person asks for on that board, not as a new HTML block in the chat; " +
				"when done, show it with `" + cli + " design panel " + a.ID + " show --board " + p.File + "`."
		}
		return " HTML-макет из этого чата теперь доска «" + title + "» (" + file + ") проекта «" + plain(a.Title, 120) +
			"» в Sinopia Design: правки, о которых просит человек, вноси в эту доску, а не присылай новый HTML в чат; " +
			"закончил — покажи её командой `" + cli + " design panel " + a.ID + " show --board " + p.File + "`."
	}
	return ""
}
