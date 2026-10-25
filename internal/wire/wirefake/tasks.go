package wirefake

import (
	"context"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Tasks is the agent's task list of a fake conversation: the first one keeps a plan, the others none.
func (s *Service) Tasks(_ context.Context, id string) ([]wire.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if c.rec.ID != "tab-k3Jx9QmZ" {
		return []wire.Task{}, nil
	}
	return []wire.Task{
		{ID: "1", Subject: "Прочитать, как устроен разбор транскрипта", Status: wire.TaskCompleted},
		{ID: "2", Subject: "Написать страницу задач", Status: wire.TaskInProgress},
		{ID: "3", Subject: "Прогнать проверки", Status: wire.TaskPending},
		{ID: "4", Subject: "Дождаться ответа руководителя", Status: wire.TaskBlocked},
	}, nil
}
