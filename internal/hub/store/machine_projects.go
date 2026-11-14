package store

import (
	"context"
	"encoding/json"
)

// MachineProject is a project (workspace) a machine told the hub of (migration 0072).
type MachineProject struct {
	MachineID   string
	ID          string
	Name        string
	Directories []string
	UpdatedAt   int64
}

func (s Store) putMachineProject(ctx context.Context, p MachineProject) error {
	dirs, _ := json.Marshal(p.Directories)
	if p.Directories == nil {
		dirs = []byte("[]")
	}
	_, err := s.q.Exec(ctx, `INSERT INTO machine_projects (machine_id, project_id, name, directories, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (machine_id, project_id) DO UPDATE SET name = excluded.name, directories = excluded.directories, updated_at = excluded.updated_at`,
		p.MachineID, p.ID, p.Name, string(dirs), p.UpdatedAt)
	return err
}

// PutMachineProject keeps one project of a machine, replacing the one of the same id.
func (s Store) PutMachineProject(ctx context.Context, p MachineProject) error {
	return s.putMachineProject(ctx, p)
}

// DeleteMachineProject forgets one project of a machine.
func (s Store) DeleteMachineProject(ctx context.Context, machineID, projectID string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM machine_projects WHERE machine_id = ? AND project_id = ?`, machineID, projectID)
	return err
}

// ReplaceMachineProjects makes the machine's rows exactly the list (a full list on connect). Run it in a transaction.
func (s Store) ReplaceMachineProjects(ctx context.Context, machineID string, list []MachineProject) error {
	if _, err := s.q.Exec(ctx, `DELETE FROM machine_projects WHERE machine_id = ?`, machineID); err != nil {
		return err
	}
	for _, p := range list {
		p.MachineID = machineID
		if err := s.putMachineProject(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// MachineProjectsOf reads the projects of these machines, by machine, in name order.
func (s Store) MachineProjectsOf(ctx context.Context, machineIDs []string) (map[string][]MachineProject, error) {
	out := map[string][]MachineProject{}
	ids := machineIDs
	for len(ids) > 0 {
		n := min(len(ids), chunk)
		part := ids[:n]
		ids = ids[n:]
		args := make([]any, len(part))
		for i, id := range part {
			args[i] = id
		}
		rows, err := s.q.Query(ctx, `SELECT machine_id, project_id, name, directories, updated_at FROM machine_projects
			WHERE machine_id IN (`+placeholders(len(part))+`) ORDER BY name, project_id`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var p MachineProject
			var dirs string
			if err := rows.Scan(&p.MachineID, &p.ID, &p.Name, &dirs, &p.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			_ = json.Unmarshal([]byte(dirs), &p.Directories)
			out[p.MachineID] = append(out[p.MachineID], p)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
