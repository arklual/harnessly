package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// HeavyJob is a heavy command the hub places on a builder (docs/hub/builders.md). Its files are on the hub's
// disk (internal/hub/jobs); the row is what the lists and the statuses read.
type HeavyJob struct {
	ID          string
	SpaceID     string
	UserID      string
	FromMachine string
	Title       string
	Kind        string
	Caps        []string
	// WantMachine is the person's choice of a builder; "" is auto.
	WantMachine string
	Argv        []string
	Env         map[string]string
	Outputs     []string
	TimeoutSec  int
	HasInput    bool
	Status      string
	// MachineID is where the job was placed: a builder, or the submitter's own machine for a local run.
	MachineID string
	Local     bool
	// Reason is why it was placed there, in words.
	Reason       string
	ExitCode     *int
	Error        string
	LogTail      string
	Result       []JobResultFile
	CancelWanted bool
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
}

// JobResultFile is a result file of a job, kept on the hub.
type JobResultFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

const jobCols = `id, space_id, user_id, from_machine, title, kind, caps, want_machine, argv, env, outputs, timeout_sec, has_input, status, machine_id, local, reason, exit_code, error, log_tail, result, cancel_wanted, created_at, started_at, finished_at`

func scanJob(row interface{ Scan(...any) error }) (HeavyJob, error) {
	var j HeavyJob
	var user sql.NullString
	var caps, argv, env, outputs, result string
	var input, local, cancel int
	var exit sql.NullInt64
	var created int64
	var started, finished sql.NullInt64
	if err := row.Scan(&j.ID, &j.SpaceID, &user, &j.FromMachine, &j.Title, &j.Kind, &caps, &j.WantMachine, &argv, &env, &outputs, &j.TimeoutSec,
		&input, &j.Status, &j.MachineID, &local, &j.Reason, &exit, &j.Error, &j.LogTail, &result, &cancel, &created, &started, &finished); err != nil {
		return HeavyJob{}, err
	}
	j.UserID, j.Caps, j.HasInput, j.Local, j.CancelWanted = user.String, SplitCaps(caps), input != 0, local != 0, cancel != 0
	_ = json.Unmarshal([]byte(argv), &j.Argv)
	_ = json.Unmarshal([]byte(env), &j.Env)
	_ = json.Unmarshal([]byte(outputs), &j.Outputs)
	_ = json.Unmarshal([]byte(result), &j.Result)
	if exit.Valid {
		v := int(exit.Int64)
		j.ExitCode = &v
	}
	j.CreatedAt, j.StartedAt, j.FinishedAt = fromMs(created), fromNullMs(started), fromNullMs(finished)
	return j, nil
}

func jsonText(v any, empty string) string {
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return empty
	}
	return string(b)
}

// CreateHeavyJob records a new job.
func (s Store) CreateHeavyJob(ctx context.Context, j *HeavyJob) error {
	if j.ID == "" {
		j.ID = NewID()
	}
	if j.CreatedAt.IsZero() {
		j.CreatedAt = time.Now().UTC()
	}
	var exit any
	if j.ExitCode != nil {
		exit = *j.ExitCode
	}
	_, err := s.q.Exec(ctx, `INSERT INTO heavy_jobs (`+jobCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.ID, j.SpaceID, nullStr(j.UserID), j.FromMachine, j.Title, j.Kind, JoinCaps(j.Caps), j.WantMachine, jsonText(j.Argv, "[]"), jsonText(j.Env, "{}"),
		jsonText(j.Outputs, "[]"), j.TimeoutSec, boolInt(j.HasInput), j.Status, j.MachineID, boolInt(j.Local), j.Reason, exit, j.Error, j.LogTail,
		jsonText(j.Result, "[]"), boolInt(j.CancelWanted), Ms(j.CreatedAt), nullMs(j.StartedAt), nullMs(j.FinishedAt))
	return err
}

// HeavyJobByID is a job.
func (s Store) HeavyJobByID(ctx context.Context, id string) (HeavyJob, error) {
	return scanJob(s.q.QueryRow(ctx, `SELECT `+jobCols+` FROM heavy_jobs WHERE id = ?`, id))
}

func (s Store) queryJobs(ctx context.Context, where string, args ...any) ([]HeavyJob, error) {
	rows, err := s.q.Query(ctx, `SELECT `+jobCols+` FROM heavy_jobs WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HeavyJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ActiveHeavyJobs lists the jobs that are not over, oldest first: the queue and what runs.
func (s Store) ActiveHeavyJobs(ctx context.Context) ([]HeavyJob, error) {
	return s.queryJobs(ctx, `status IN ('queued', 'running') ORDER BY created_at, id`)
}

// HeavyJobsOfSpace lists the space's jobs, newest first, at most limit.
func (s Store) HeavyJobsOfSpace(ctx context.Context, spaceID string, limit int) ([]HeavyJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.queryJobs(ctx, `space_id = ? ORDER BY created_at DESC, id LIMIT `+itoa(limit), spaceID)
}

// UpdateHeavyJob writes the fields that change over a job's life: where it was placed, how it stands, what came of it.
func (s Store) UpdateHeavyJob(ctx context.Context, j HeavyJob) error {
	var exit any
	if j.ExitCode != nil {
		exit = *j.ExitCode
	}
	_, err := s.q.Exec(ctx, `UPDATE heavy_jobs SET status = ?, machine_id = ?, local = ?, reason = ?, exit_code = ?, error = ?, log_tail = ?, result = ?,
		cancel_wanted = ?, started_at = ?, finished_at = ? WHERE id = ?`,
		j.Status, j.MachineID, boolInt(j.Local), j.Reason, exit, j.Error, j.LogTail, jsonText(j.Result, "[]"), boolInt(j.CancelWanted),
		nullMs(j.StartedAt), nullMs(j.FinishedAt), j.ID)
	return err
}

// DeleteHeavyJobsBefore drops the final jobs that ended before the time and returns their ids (their files go too).
func (s Store) DeleteHeavyJobsBefore(ctx context.Context, before time.Time) ([]string, error) {
	rows, err := s.q.Query(ctx, `SELECT id FROM heavy_jobs WHERE status NOT IN ('queued', 'running') AND finished_at IS NOT NULL AND finished_at < ?`, Ms(before))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if _, err := s.q.Exec(ctx, `DELETE FROM heavy_jobs WHERE id = ?`, id); err != nil {
			return ids, err
		}
	}
	return ids, nil
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
