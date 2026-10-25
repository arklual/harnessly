package wirefake

import (
	"context"
	"strings"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The fake machine's design library: a few projects, two design systems, and conversations opened in the folders of the
// projects made here. Nothing is written to disk. The person asking is Пётр: one project is theirs, one is
// Олег's, and the third is older than owners, with none.
const (
	fakeLibraryRoot = "/srv/fake-design"
	fakeMe          = "Пётр"
	fakeOther       = "Олег"
)

type fakeDesign struct {
	a wire.DesignArtifact
}

// Designly is the fake machine's design library, on the same service as its conversations.
func (s *Service) Designly() wire.DesignlySource { return designly{s} }

type designly struct{ *Service }

var _ wire.DesignlySource = designly{}

func fakeDesigns(now int64) []fakeDesign {
	mk := func(id, title string, kind wire.DesignKind, pinned bool, age int64, owner *wire.DesignOwner, boards ...string) fakeDesign {
		a := wire.DesignArtifact{Owner: owner, ID: id, Title: title, UpdatedAt: wire.Ms(now - age), Kind: kind, Pinned: pinned,
			ProjectPath: fakeLibraryRoot + "/" + id + "/project", Boards: []wire.DesignBoard{}, Convs: []string{}}
		for _, b := range boards {
			a.Boards = append(a.Boards, wire.DesignBoard{Title: b, Path: b + ".dc.html"})
		}
		return fakeDesign{a}
	}
	return []fakeDesign{
		mk("lending-dlya-kofeini", "Лендинг для кофейни", wire.DesignSite, true, 3600_000, &wire.DesignOwner{Name: fakeMe, Me: true}, "Главная", "Меню"),
		mk("pitch-q4", "Питч Q4", wire.DesignSlides, false, 86400_000, &wire.DesignOwner{Name: fakeOther}, "Титул", "Рынок", "Продукт"),
		mk("acme-ui", "Acme UI", wire.DesignDS, false, 3*86400_000, nil, "Кнопки", "Формы"),
	}
}

// Create makes a project and opens a conversation in its folder.
func (s designly) Create(ctx context.Context, c wire.DesignlyCreate) (wire.DesignlyCreated, error) {
	title := strings.TrimSpace(c.Title)
	if title == "" {
		return wire.DesignlyCreated{}, &wire.DesignlyError{Code: wire.RefuseBadRequest, Message: "Назовите проект"}
	}
	s.mu.Lock()
	if s.designs == nil {
		s.designs = fakeDesigns(s.now())
	}
	id := slug(title)
	for _, d := range s.designs {
		if d.a.ID == id {
			s.mu.Unlock()
			return wire.DesignlyCreated{}, &wire.DesignlyError{Code: wire.RefuseConflict, Message: "Проект «" + id + "» уже есть"}
		}
	}
	dir := fakeLibraryRoot + "/" + id
	a := wire.DesignArtifact{ID: id, Title: title, UpdatedAt: wire.Ms(s.now()), Kind: c.Kind, ProjectPath: dir + "/project",
		Owner: &wire.DesignOwner{Name: fakeMe, Me: true}, Boards: []wire.DesignBoard{}, Convs: []string{}}
	s.designs = append([]fakeDesign{{a}}, s.designs...)
	s.mu.Unlock()
	var model *wire.Model
	if c.Model != nil {
		model = &wire.Model{ID: *c.Model, Effort: c.Effort}
	}
	res := s.Do(ctx, "designly", wire.ConversationCreateCmd{OpID: "dz-" + s.newID(), Cwd: dir, Provider: c.Provider, Model: model,
		First: &wire.FirstMessage{ClientID: "dz-first-" + s.newID(), Text: c.Brief, AttachmentIDs: []string{}}})
	if res.R != wire.ResultAccepted || res.ID == nil {
		return wire.DesignlyCreated{}, &wire.DesignlyError{Code: wire.RefuseInternal, ProjectID: id,
			Message: "Проект «" + title + "» создан, но диалог не запустился: " + string(res.Code)}
	}
	return wire.DesignlyCreated{ID: id, Title: title, Conv: *res.ID}, nil
}

func slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r >= 'а' && r <= 'я':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	if out := strings.Trim(b.String(), "-"); out != "" {
		return out
	}
	return "project"
}

// Library lists the projects with the conversations opened in their folders.
func (s designly) Library(context.Context) (wire.DesignLibrary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.designs == nil {
		s.designs = fakeDesigns(s.now())
	}
	out := wire.DesignLibrary{Port: 4466, Root: fakeLibraryRoot, Artifacts: make([]wire.DesignArtifact, 0, len(s.designs))}
	for _, d := range s.designs {
		a := d.a
		a.Convs = []string{}
		for _, c := range s.convs {
			if !c.deleted && strings.HasPrefix(c.rec.Cwd, fakeLibraryRoot+"/"+a.ID) {
				a.Convs = append(a.Convs, c.rec.ID)
			}
		}
		out.Artifacts = append(out.Artifacts, a)
	}
	return out, nil
}

func (s designly) Systems(context.Context) ([]wire.DesignSystemInfo, error) {
	return []wire.DesignSystemInfo{
		{ID: "acme-design-system", Label: "Acme Design System", Desc: "Компоненты, токены и правила витрины"},
		{ID: "acme-ui", Label: "Acme UI", Desc: "Проект-дизайн-система из библиотеки"},
		{ID: "kit:acme-ui", Label: "acme-ui", Desc: "UI-кит, версия 1.4.0"},
	}, nil
}

func (s designly) Pin(_ context.Context, id string, pinned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.designs == nil {
		s.designs = fakeDesigns(s.now())
	}
	for i := range s.designs {
		if s.designs[i].a.ID == id {
			s.designs[i].a.Pinned = pinned
			return nil
		}
	}
	return &wire.DesignlyError{Code: wire.RefuseNotFound, Message: "Макета больше нет"}
}
