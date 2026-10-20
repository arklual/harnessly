package usagereport

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestBucketsOfWorkWithoutAProject(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	if noProject() != "no project" || outsideRepos() != "outside repositories" {
		t.Errorf("en: %q %q", noProject(), outsideRepos())
	}
	t.Setenv("RUNTIME_LOCALE", "ru")
	if noProject() != "без проекта" || outsideRepos() != "вне репозиториев" {
		t.Errorf("ru: %q %q", noProject(), outsideRepos())
	}
}
