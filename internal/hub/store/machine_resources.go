package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// What the machine screens read and write about a machine's resources, hardware, operations and load
// (docs/hub/machines-resources.md).

// Machine parts the hub may control (machine_managed.capability).
const (
	CapBudget    = "budget"
	CapEnv       = "env"
	CapResources = "resources"
	CapAccess    = "access"
)

// Capabilities lists the parts in the order the screens show them.
var Capabilities = []string{CapBudget, CapEnv, CapResources, CapAccess}

// Operation kinds and statuses (machine_ops).
const (
	OpResize         = "resize"
	OpReboot         = "reboot"
	OpPowerOff       = "power_off"
	OpPowerOn        = "power_on"
	OpRestartRunner  = "restart_runner"
	OpRestartMachine = "restart_machine"
	OpDrain          = "drain"

	OpPending   = "pending"
	OpRunning   = "running"
	OpDone      = "done"
	OpFailed    = "failed"
	OpCancelled = "cancelled"
)

// MachineResources is what a machine has now and what its provider says it costs. Zero numbers are unknown (nil).
type MachineResources struct {
	MachineID          string
	Provider           string
	ExternalID         string
	CPU, RAMGB, DiskGB *int
	// Plan is the provider's plan as JSON ("{}" when none).
	Plan          string
	PriceAmount   *int64
	PriceCurrency string
	PricePeriod   string
	UpdatedAt     time.Time
}

const resourcesCols = `machine_id, provider, external_id, cpu, ram_gb, disk_gb, plan, price_amount, price_currency, price_period, updated_at`

func scanResources(row interface{ Scan(...any) error }) (MachineResources, error) {
	var r MachineResources
	var cpu, ram, disk, price sql.NullInt64
	var at int64
	if err := row.Scan(&r.MachineID, &r.Provider, &r.ExternalID, &cpu, &ram, &disk, &r.Plan, &price, &r.PriceCurrency, &r.PricePeriod, &at); err != nil {
		return MachineResources{}, err
	}
	r.CPU, r.RAMGB, r.DiskGB = nullInt(cpu), nullInt(ram), nullInt(disk)
	if price.Valid {
		v := price.Int64
		r.PriceAmount = &v
	}
	r.UpdatedAt = fromMs(at)
	return r, nil
}

func nullInt(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

func intArg(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// ResourcesByMachine reads one machine's resources; ErrNotFound when none were stored.
func (s Store) ResourcesByMachine(ctx context.Context, machineID string) (MachineResources, error) {
	r, err := scanResources(s.q.QueryRow(ctx, `SELECT `+resourcesCols+` FROM machine_resources WHERE machine_id = ?`, machineID))
	if errors.Is(err, sql.ErrNoRows) {
		return MachineResources{}, ErrNotFound
	}
	return r, err
}

// ResourcesByMachines reads the resources of the machines that have any.
func (s Store) ResourcesByMachines(ctx context.Context, ids []string) (map[string]MachineResources, error) {
	out := map[string]MachineResources{}
	for i := 0; i < len(ids); i += chunk {
		part := ids[i:min(i+chunk, len(ids))]
		args := make([]any, len(part))
		for j, id := range part {
			args[j] = id
		}
		rows, err := s.q.Query(ctx, `SELECT `+resourcesCols+` FROM machine_resources WHERE machine_id IN (`+placeholders(len(part))+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			r, err := scanResources(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			out[r.MachineID] = r
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// PutResources stores a machine's resources, replacing what was there.
func (s Store) PutResources(ctx context.Context, r MachineResources) error {
	if r.Plan == "" {
		r.Plan = "{}"
	}
	var price any
	if r.PriceAmount != nil {
		price = *r.PriceAmount
	}
	_, err := s.q.Exec(ctx, `INSERT INTO machine_resources (`+resourcesCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (machine_id) DO UPDATE SET provider = excluded.provider, external_id = excluded.external_id, cpu = excluded.cpu,
		ram_gb = excluded.ram_gb, disk_gb = excluded.disk_gb, plan = excluded.plan, price_amount = excluded.price_amount,
		price_currency = excluded.price_currency, price_period = excluded.price_period, updated_at = excluded.updated_at`,
		r.MachineID, r.Provider, r.ExternalID, intArg(r.CPU), intArg(r.RAMGB), intArg(r.DiskGB), r.Plan, price, r.PriceCurrency, r.PricePeriod, Ms(r.UpdatedAt))
	return err
}

// MachineHW is what a machine said about its hardware. Zero numbers are unknown.
type MachineHW struct {
	MachineID string
	Cores     int
	// RAMTotal and DiskTotal are bytes.
	RAMTotal  int64
	DiskTotal int64
	OS, Arch  string
	Model     string
	Laptop    bool
	UpdatedAt time.Time
}

// PutHW stores the hardware a machine reported.
func (s Store) PutHW(ctx context.Context, h MachineHW) error {
	_, err := s.q.Exec(ctx, `INSERT INTO machine_hw (machine_id, cores, ram_total, disk_total, os, arch, model, laptop, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (machine_id) DO UPDATE SET cores = excluded.cores, ram_total = excluded.ram_total, disk_total = excluded.disk_total,
		os = excluded.os, arch = excluded.arch, model = excluded.model, laptop = excluded.laptop, updated_at = excluded.updated_at`,
		h.MachineID, h.Cores, h.RAMTotal, h.DiskTotal, h.OS, h.Arch, h.Model, boolInt(h.Laptop), Ms(h.UpdatedAt))
	return err
}

// HWByMachines reads the hardware of the machines that reported any.
func (s Store) HWByMachines(ctx context.Context, ids []string) (map[string]MachineHW, error) {
	out := map[string]MachineHW{}
	for i := 0; i < len(ids); i += chunk {
		part := ids[i:min(i+chunk, len(ids))]
		args := make([]any, len(part))
		for j, id := range part {
			args[j] = id
		}
		rows, err := s.q.Query(ctx, `SELECT machine_id, cores, ram_total, disk_total, os, arch, model, laptop, updated_at
			FROM machine_hw WHERE machine_id IN (`+placeholders(len(part))+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var h MachineHW
			var cores, ram, disk sql.NullInt64
			var laptop int
			var at int64
			if err := rows.Scan(&h.MachineID, &cores, &ram, &disk, &h.OS, &h.Arch, &h.Model, &laptop, &at); err != nil {
				rows.Close()
				return nil, err
			}
			h.Cores, h.RAMTotal, h.DiskTotal = int(cores.Int64), ram.Int64, disk.Int64
			h.Laptop, h.UpdatedAt = laptop != 0, fromMs(at)
			out[h.MachineID] = h
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// MachineOp is one operation on a machine.
type MachineOp struct {
	ID           string
	MachineID    string
	Kind         string
	Status       string
	Params       string // JSON
	ProviderOpID string
	Error        string
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	FinishedAt   *time.Time
}

// Active reports whether the operation is not finished.
func (o MachineOp) Active() bool { return o.Status == OpPending || o.Status == OpRunning }

const opCols = `id, machine_id, kind, status, params, provider_op_id, error, created_by, created_at, updated_at, finished_at`

func scanOp(row interface{ Scan(...any) error }) (MachineOp, error) {
	var o MachineOp
	var created, updated int64
	var fin sql.NullInt64
	if err := row.Scan(&o.ID, &o.MachineID, &o.Kind, &o.Status, &o.Params, &o.ProviderOpID, &o.Error, &o.CreatedBy, &created, &updated, &fin); err != nil {
		return MachineOp{}, err
	}
	o.CreatedAt, o.UpdatedAt, o.FinishedAt = fromMs(created), fromMs(updated), fromNullMs(fin)
	return o, nil
}

// CreateOp stores a new operation; ID and the times are filled when empty.
func (s Store) CreateOp(ctx context.Context, o *MachineOp) error {
	if o.ID == "" {
		o.ID = NewID()
	}
	if o.CreatedAt.IsZero() {
		o.CreatedAt = time.Now().UTC()
	}
	if o.UpdatedAt.IsZero() {
		o.UpdatedAt = o.CreatedAt
	}
	if o.Status == "" {
		o.Status = OpPending
	}
	if o.Params == "" {
		o.Params = "{}"
	}
	_, err := s.q.Exec(ctx, `INSERT INTO machine_ops (`+opCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.ID, o.MachineID, o.Kind, o.Status, o.Params, o.ProviderOpID, o.Error, o.CreatedBy, Ms(o.CreatedAt), Ms(o.UpdatedAt), nullMs(o.FinishedAt))
	return err
}

// OpByID reads one operation; ErrNotFound when there is none.
func (s Store) OpByID(ctx context.Context, id string) (MachineOp, error) {
	o, err := scanOp(s.q.QueryRow(ctx, `SELECT `+opCols+` FROM machine_ops WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return MachineOp{}, ErrNotFound
	}
	return o, err
}

// SetOp moves an operation to a status. A terminal status stamps finished_at. It reports false when the operation was
// already finished (a finished operation never changes again), so a cancel and a completion cannot both win.
func (s Store) SetOp(ctx context.Context, id, status, providerOpID, errText string, at time.Time) (bool, error) {
	var fin any
	if status == OpDone || status == OpFailed || status == OpCancelled {
		fin = Ms(at)
	}
	res, err := s.q.Exec(ctx, `UPDATE machine_ops SET status = ?, provider_op_id = CASE WHEN ? <> '' THEN ? ELSE provider_op_id END,
		error = ?, updated_at = ?, finished_at = ? WHERE id = ? AND status IN ('pending', 'running')`,
		status, providerOpID, providerOpID, errText, Ms(at), fin, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// OpsByMachine lists a machine's operations, newest first; active limits it to the unfinished ones.
func (s Store) OpsByMachine(ctx context.Context, machineID string, active bool, limit int) ([]MachineOp, error) {
	q := `SELECT ` + opCols + ` FROM machine_ops WHERE machine_id = ?`
	if active {
		q += ` AND status IN ('pending', 'running')`
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	return s.queryOps(ctx, q, machineID, limit)
}

// OpenOps lists every unfinished operation of every machine, oldest first: what the runner picks up after a restart.
func (s Store) OpenOps(ctx context.Context) ([]MachineOp, error) {
	return s.queryOps(ctx, `SELECT `+opCols+` FROM machine_ops WHERE status IN ('pending', 'running') ORDER BY created_at, id LIMIT ?`, 1000)
}

// ActiveOpsByMachines lists the unfinished operations of the machines, oldest first.
func (s Store) ActiveOpsByMachines(ctx context.Context, ids []string) ([]MachineOp, error) {
	var out []MachineOp
	for i := 0; i < len(ids); i += chunk {
		part := ids[i:min(i+chunk, len(ids))]
		args := make([]any, len(part))
		for j, id := range part {
			args[j] = id
		}
		ops, err := s.queryOps(ctx, `SELECT `+opCols+` FROM machine_ops WHERE status IN ('pending', 'running') AND machine_id IN (`+
			placeholders(len(part))+`) ORDER BY created_at, id LIMIT 100000`, args...)
		if err != nil {
			return nil, err
		}
		out = append(out, ops...)
	}
	return out, nil
}

func (s Store) queryOps(ctx context.Context, q string, args ...any) ([]MachineOp, error) {
	rows, err := s.q.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MachineOp
	for rows.Next() {
		o, err := scanOp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// MetricBucket is the load of a machine over one five-minute bucket.
type MetricBucket struct {
	MachineID string
	// Bucket is the start of the five minutes, epoch ms.
	Bucket           int64
	Samples          int
	CPUAvg, CPUMax   float64
	RAMAvg, RAMMax   float64
	DiskAvg, DiskMax float64
	ChatsMax         int
}

// MetricStep is the width of a bucket.
const MetricStep = 5 * time.Minute

// MetricsRetention is how long buckets are kept.
const MetricsRetention = 7 * 24 * time.Hour

// BucketMs is the start of the five-minute bucket ms falls in.
func BucketMs(ms int64) int64 {
	step := MetricStep.Milliseconds()
	return ms - ms%step
}

// AddMetricSample folds one sample (percentages 0..100) into its bucket.
func (s Store) AddMetricSample(ctx context.Context, machineID string, atMs int64, cpu, ram, disk float64, chats int) error {
	_, err := s.q.Exec(ctx, `INSERT INTO machine_metrics_5m (machine_id, bucket, samples, cpu_sum, cpu_max, ram_sum, ram_max, disk_sum, disk_max, chats_max)
		VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (machine_id, bucket) DO UPDATE SET samples = machine_metrics_5m.samples + 1,
		cpu_sum = machine_metrics_5m.cpu_sum + excluded.cpu_sum,
		cpu_max = CASE WHEN excluded.cpu_max > machine_metrics_5m.cpu_max THEN excluded.cpu_max ELSE machine_metrics_5m.cpu_max END,
		ram_sum = machine_metrics_5m.ram_sum + excluded.ram_sum,
		ram_max = CASE WHEN excluded.ram_max > machine_metrics_5m.ram_max THEN excluded.ram_max ELSE machine_metrics_5m.ram_max END,
		disk_sum = machine_metrics_5m.disk_sum + excluded.disk_sum,
		disk_max = CASE WHEN excluded.disk_max > machine_metrics_5m.disk_max THEN excluded.disk_max ELSE machine_metrics_5m.disk_max END,
		chats_max = CASE WHEN excluded.chats_max > machine_metrics_5m.chats_max THEN excluded.chats_max ELSE machine_metrics_5m.chats_max END`,
		machineID, BucketMs(atMs), cpu, cpu, ram, ram, disk, disk, chats)
	return err
}

// MetricsRange reads a machine's buckets that start in [fromMs, toMs), oldest first.
func (s Store) MetricsRange(ctx context.Context, machineID string, fromMs, toMs int64) ([]MetricBucket, error) {
	rows, err := s.q.Query(ctx, `SELECT bucket, samples, cpu_sum, cpu_max, ram_sum, ram_max, disk_sum, disk_max, chats_max
		FROM machine_metrics_5m WHERE machine_id = ? AND bucket >= ? AND bucket < ? ORDER BY bucket`, machineID, fromMs, toMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetricBucket
	for rows.Next() {
		b := MetricBucket{MachineID: machineID}
		var cs, rs, ds float64
		if err := rows.Scan(&b.Bucket, &b.Samples, &cs, &b.CPUMax, &rs, &b.RAMMax, &ds, &b.DiskMax, &b.ChatsMax); err != nil {
			return nil, err
		}
		n := float64(max(b.Samples, 1))
		b.CPUAvg, b.RAMAvg, b.DiskAvg = cs/n, rs/n, ds/n
		out = append(out, b)
	}
	return out, rows.Err()
}

// LastMetric reads a machine's newest bucket; ErrNotFound when it has none.
func (s Store) LastMetric(ctx context.Context, machineID string) (MetricBucket, error) {
	out, err := s.MetricsRange(ctx, machineID, 0, 1<<62)
	if err != nil {
		return MetricBucket{}, err
	}
	if len(out) == 0 {
		return MetricBucket{}, ErrNotFound
	}
	return out[len(out)-1], nil
}

// PruneMetrics deletes at most limit buckets older than beforeMs and reports how many went. The caller repeats with
// pauses until it gets less than limit, so a long backlog never holds the database for long.
func (s Store) PruneMetrics(ctx context.Context, beforeMs int64, limit int) (int, error) {
	res, err := s.q.Exec(ctx, `DELETE FROM machine_metrics_5m WHERE (machine_id, bucket) IN
		(SELECT machine_id, bucket FROM machine_metrics_5m WHERE bucket < ? LIMIT ?)`, beforeMs, limit)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// MachineManaged is one row of machine_managed.
type MachineManaged struct {
	Capability string
	Managed    bool
	UpdatedAt  time.Time
	UpdatedBy  string
}

// ManagedByMachine reads the explicit settings of a machine, by capability.
func (s Store) ManagedByMachine(ctx context.Context, machineID string) (map[string]MachineManaged, error) {
	rows, err := s.q.Query(ctx, `SELECT capability, managed, updated_at, updated_by FROM machine_managed WHERE machine_id = ?`, machineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]MachineManaged{}
	for rows.Next() {
		var m MachineManaged
		var managed int
		var at int64
		if err := rows.Scan(&m.Capability, &managed, &at, &m.UpdatedBy); err != nil {
			return nil, err
		}
		m.Managed, m.UpdatedAt = managed != 0, fromMs(at)
		out[m.Capability] = m
	}
	return out, rows.Err()
}

// SetManaged stores whether the hub controls a part of a machine.
func (s Store) SetManaged(ctx context.Context, machineID, capability string, managed bool, by string, at time.Time) error {
	_, err := s.q.Exec(ctx, `INSERT INTO machine_managed (machine_id, capability, managed, updated_at, updated_by) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (machine_id, capability) DO UPDATE SET managed = excluded.managed, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		machineID, capability, boolInt(managed), Ms(at), by)
	return err
}

// MachineWeek is what one machine's calls amount to over a span.
type MachineWeek struct {
	MachineID string
	// Chats counts the distinct sessions that made a call.
	Chats int
	// Calls and Unpriced count the calls and those among them with no list price.
	Calls, Unpriced int
	// ListCostMicros is the list cost of the priced calls in micro-dollars.
	ListCostMicros int64
}

// MachineWeeks sums the calls of the machines in [fromMs, toMs); a machine with none is absent from the map.
func (s Store) MachineWeeks(ctx context.Context, ids []string, fromMs, toMs int64) (map[string]MachineWeek, error) {
	out := map[string]MachineWeek{}
	for i := 0; i < len(ids); i += chunk {
		part := ids[i:min(i+chunk, len(ids))]
		args := []any{fromMs, toMs}
		for _, id := range part {
			args = append(args, id)
		}
		rows, err := s.q.Query(ctx, `SELECT machine_id, COUNT(DISTINCT session_key), COUNT(*),
			COALESCE(SUM(CASE WHEN list_cost_micros IS NULL THEN 1 ELSE 0 END), 0), COALESCE(SUM(list_cost_micros), 0)
			FROM usage_calls WHERE ts >= ? AND ts < ? AND machine_id IN (`+placeholders(len(part))+`) GROUP BY machine_id`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var w MachineWeek
			if err := rows.Scan(&w.MachineID, &w.Chats, &w.Calls, &w.Unpriced, &w.ListCostMicros); err != nil {
				rows.Close()
				return nil, err
			}
			out[w.MachineID] = w
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ManagedByMachines reads the explicit settings of the machines, by machine and capability; a machine with none is
// absent from the map.
func (s Store) ManagedByMachines(ctx context.Context, ids []string) (map[string]map[string]MachineManaged, error) {
	out := map[string]map[string]MachineManaged{}
	for i := 0; i < len(ids); i += chunk {
		part := ids[i:min(i+chunk, len(ids))]
		args := make([]any, len(part))
		for j, id := range part {
			args[j] = id
		}
		rows, err := s.q.Query(ctx, `SELECT machine_id, capability, managed, updated_at, updated_by FROM machine_managed WHERE machine_id IN (`+
			placeholders(len(part))+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var m MachineManaged
			var managed int
			var at int64
			if err := rows.Scan(&id, &m.Capability, &managed, &at, &m.UpdatedBy); err != nil {
				rows.Close()
				return nil, err
			}
			m.Managed, m.UpdatedAt = managed != 0, fromMs(at)
			if out[id] == nil {
				out[id] = map[string]MachineManaged{}
			}
			out[id][m.Capability] = m
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
