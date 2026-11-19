package automations

// Routines (docs/hub/routines.md): a recurring task of a person. A routine is an automation rule with
// `routine` set, almost always with a schedule. Its run's agent reports an outcome (ok, quiet, failed) with a short
// summary, the full text and artifacts; the hub checks the report with the routine's own deterministic checks, keeps
// the memory the agent left for the next run, tells the person per the delivery policy, and pauses the routine after
// three failed runs in a row with one notice. A routine's run needs no approval: it ends `done` (or `failed`).

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Outcomes of a routine's run.
const (
	OutcomeOK     = "ok"
	OutcomeQuiet  = "quiet"
	OutcomeFailed = "failed"
)

// Delivery policies: when the person hears of a run.
const (
	DeliverAlways  = "always"
	DeliverChanged = "changed" // ok and failed, not quiet
	DeliverFailure = "failure"
	DeliverNever   = "never"
)

// Channels a routine's result goes to. The run's chat is always there; `notice` is the bell of harnessly (the hub's
// notices), the others the run's agent sends itself with the person's connectors (telegram, messenger) or writes
// (file, doc) — it says which it reached.
var routineChannels = map[string]bool{"notice": true, "telegram": true, "messenger": true, "mail": true, "file": true, "doc": true}

// Autonomy levels: what the run's agent may do outside the machine.
var routineAutonomy = map[string]string{
	"read":  "Только читай и собирай: ничего не меняй во внешних системах, ничего не отправляй людям, не создавай MR.",
	"draft": "Готовь черновики (MR, документы, ответы), но ничего не отправляй людям, не мерджи и не выкатывай: решает человек.",
	"act":   "Можешь делать изменения сам в пределах задачи, но не мерджи в main, не выкатывай в прод и не пиши людям от имени человека.",
}

const (
	failStreakPause = 3
	routineTimeout  = 2 * time.Hour
	maxState        = 16 << 10
	maxBody         = 64 << 10
	maxArtifacts    = 12
)

// Delivery says when and where the person hears of a run.
type Delivery struct {
	When     string   `json:"when,omitempty"`
	Channels []string `json:"channels,omitempty"`
	// File is the path the agent writes the result to as a new version every run (channel file), e.g. digest/{date}.md.
	File string `json:"file,omitempty"`
}

// Check is one deterministic check of a run's report, done by the hub: the report passes only when every check does.
type Check struct {
	// Kind: contains (text in the summary or body), regex (a pattern matches the body), artifact (an artifact of this
	// kind, or any when empty), file (an artifact of kind file whose title is the delivery file, with a new version),
	// fresh (the body names today's date in the person's zone, YYYY-MM-DD or D month).
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
	Label string `json:"label,omitempty"`
}

// Routine is what makes a rule a routine.
type Routine struct {
	Delivery Delivery `json:"delivery"`
	Checks   []Check  `json:"checks,omitempty"`
	Autonomy string   `json:"autonomy,omitempty"`
	// Words is the schedule as the person said it ("каждый понедельник в 10:00"), shown instead of the cron.
	Words string `json:"words,omitempty"`
	// Once runs the routine one time: after its run it is turned off.
	Once bool `json:"once,omitempty"`
	// Until turns the routine off after this day (YYYY-MM-DD, the schedule's zone); empty: never.
	Until string `json:"until,omitempty"`
	// BudgetUSD is the run's spending cap the agent is told to keep; 0: none named.
	BudgetUSD float64 `json:"budgetUsd,omitempty"`
	// ChatID is the chat the routine was made in, for «Открыть чат задачи».
	ChatID string `json:"chatId,omitempty"`
	// Owner and runAs are reserved for team routines: who it belongs to, whose accounts its runs use. Today both are
	// the rule's owner.
	Team string `json:"team,omitempty"`
}

// Artifact is something a run made: a document, a file version, a chart, a merge request, a link.
type Artifact struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
	Meta  string `json:"meta,omitempty"`
}

// CheckResult is one check as the hub found it.
type CheckResult struct {
	Label string `json:"label"`
	OK    bool   `json:"ok"`
	Out   string `json:"out,omitempty"`
}

// RoutineResult is what a routine's run came to, kept in the run's `result`.
type RoutineResult struct {
	Summary   string        `json:"summary,omitempty"`
	Body      string        `json:"body,omitempty"`
	Artifacts []Artifact    `json:"artifacts,omitempty"`
	Checks    []CheckResult `json:"checks,omitempty"`
	Delivered []string      `json:"delivered,omitempty"`
	Error     string        `json:"error,omitempty"`
	Model     string        `json:"model,omitempty"`
	Manual    bool          `json:"manual,omitempty"`
	// Said is what the agent itself reported as the outcome, before the checks.
	Said string `json:"said,omitempty"`
}

// Report is a routine run's report as the machine relays it.
type Report struct {
	RunID       string     `json:"runId"`
	ResultToken string     `json:"resultToken"`
	Outcome     string     `json:"outcome"`
	Summary     string     `json:"summary"`
	Body        string     `json:"body"`
	Artifacts   []Artifact `json:"artifacts"`
	State       *string    `json:"state"`
	Delivered   []string   `json:"delivered"`
	Error       string     `json:"error"`
	BugIssueID  string     `json:"bugIssueId"`
	MRURL       string     `json:"mrUrl"`
}

func checkRoutine(rt *Routine) error {
	if rt == nil {
		return nil
	}
	switch rt.Delivery.When {
	case "":
		rt.Delivery.When = DeliverChanged
	case DeliverAlways, DeliverChanged, DeliverFailure, DeliverNever:
	default:
		return invalid("delivery.when: always, changed, failure или never")
	}
	seen := map[string]bool{}
	chans := rt.Delivery.Channels[:0:0]
	for _, c := range rt.Delivery.Channels {
		c = strings.TrimSpace(c)
		if c == "chat" || seen[c] {
			continue // the run's chat is always there
		}
		if !routineChannels[c] {
			return invalid("Неизвестный канал итога: " + c)
		}
		seen[c] = true
		chans = append(chans, c)
	}
	rt.Delivery.Channels = chans
	if seen["file"] && strings.TrimSpace(rt.Delivery.File) == "" {
		return invalid("Для канала file нужен путь файла (delivery.file)")
	}
	if len(rt.Checks) > 20 {
		return invalid("Проверок не больше 20")
	}
	for i, c := range rt.Checks {
		switch c.Kind {
		case "contains", "artifact", "fresh":
		case "regex":
			if _, err := regexp.Compile(c.Value); err != nil {
				return invalid("Проверка " + strconv.Itoa(i+1) + ": неверное регулярное выражение")
			}
		case "file":
			if rt.Delivery.File == "" {
				return invalid("Проверка file нужна вместе с delivery.file")
			}
		default:
			return invalid("Проверка " + strconv.Itoa(i+1) + ": вид contains, regex, artifact, file или fresh")
		}
		if c.Kind == "contains" && strings.TrimSpace(c.Value) == "" {
			return invalid("Проверка " + strconv.Itoa(i+1) + ": что искать")
		}
	}
	if rt.Autonomy == "" {
		rt.Autonomy = "read"
	}
	if _, ok := routineAutonomy[rt.Autonomy]; !ok {
		return invalid("autonomy: read, draft или act")
	}
	if rt.Until != "" {
		if _, err := time.Parse("2006-01-02", rt.Until); err != nil {
			return invalid("until: дата YYYY-MM-DD")
		}
	}
	if rt.BudgetUSD < 0 || rt.BudgetUSD > 100 {
		return invalid("budgetUsd: от 0 до 100")
	}
	if len(rt.Words) > 200 {
		rt.Words = rt.Words[:200]
	}
	return nil
}

// routineDefaults fills what a routine needs that an event rule does not: no quiet hours (a digest at 7:00 is the
// point) and room for an hourly schedule.
func routineDefaults(r *Rule) {
	if r.Routine == nil {
		return
	}
	if r.Limits.MaxRunsPerDay == nil {
		n := 48
		r.Limits.MaxRunsPerDay = &n
	}
	if r.Limits.QuietHoursStartMsk == nil && r.Limits.QuietHoursEndMsk == nil {
		z := 0
		r.Limits.QuietHoursStartMsk, r.Limits.QuietHoursEndMsk = &z, &z
	}
}

// heard says whether the person hears of a run of this outcome.
func (d Delivery) heard(outcome string) bool {
	switch d.When {
	case DeliverAlways:
		return true
	case DeliverNever:
		return false
	case DeliverFailure:
		return outcome == OutcomeFailed
	default:
		return outcome != OutcomeQuiet
	}
}

func (d Delivery) has(ch string) bool {
	for _, c := range d.Channels {
		if c == ch {
			return true
		}
	}
	return false
}

var monthsRu = []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}

// runChecks does the routine's checks on a report.
func runChecks(rt *Routine, rep Report, day time.Time) []CheckResult {
	out := make([]CheckResult, 0, len(rt.Checks))
	text := rep.Summary + "\n" + rep.Body
	lower := strings.ToLower(text)
	for _, c := range rt.Checks {
		res := CheckResult{Label: c.Label}
		switch c.Kind {
		case "contains":
			if res.Label == "" {
				res.Label = "В итоге есть «" + c.Value + "»"
			}
			res.OK = strings.Contains(lower, strings.ToLower(c.Value))
			if !res.OK {
				res.Out = "не нашёл «" + c.Value + "» в итоге"
			}
		case "regex":
			if res.Label == "" {
				res.Label = "Итог подходит под /" + c.Value + "/"
			}
			re, err := regexp.Compile(c.Value)
			res.OK = err == nil && re.MatchString(text)
			if !res.OK {
				res.Out = "нет совпадения"
			}
		case "artifact":
			if res.Label == "" {
				res.Label = "Есть артефакт"
				if c.Value != "" {
					res.Label += " вида " + c.Value
				}
			}
			for _, a := range rep.Artifacts {
				if c.Value == "" || a.Kind == c.Value {
					res.OK = true
				}
			}
			if !res.OK {
				res.Out = "агент не приложил артефакт"
			}
		case "file":
			want := expandFile(rt.Delivery.File, day)
			if res.Label == "" {
				res.Label = "Записан " + want
			}
			for _, a := range rep.Artifacts {
				if a.Kind == "file" && (a.Title == want || strings.HasSuffix(a.URL, want)) {
					res.OK = true
				}
			}
			if !res.OK {
				res.Out = "в отчёте нет файла " + want
			}
		case "fresh":
			if res.Label == "" {
				res.Label = "Итог за сегодня"
			}
			iso := day.Format("2006-01-02")
			ru := strconv.Itoa(day.Day()) + " " + monthsRu[day.Month()-1]
			ruShort := strconv.Itoa(day.Day()) + " " + monthsRu[day.Month()-1][:min(len(monthsRu[day.Month()-1]), 6)]
			res.OK = strings.Contains(text, iso) || strings.Contains(lower, ru) || strings.Contains(lower, ruShort)
			if !res.OK {
				res.Out = "в итоге нет сегодняшней даты (" + iso + ")"
			}
		}
		out = append(out, res)
	}
	return out
}

// expandFile puts the day into a delivery path: {date} is YYYY-MM-DD.
func expandFile(path string, day time.Time) string {
	return strings.ReplaceAll(path, "{date}", day.Format("2006-01-02"))
}

func routineZone(r Rule) *time.Location {
	if r.Trigger.Schedule != nil {
		if loc, err := r.Trigger.Schedule.location(); err == nil {
			return loc
		}
	}
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.UTC
	}
	return loc
}

// routineModel is the model a routine's run uses: its own, or the hub's default for routines (Sonnet).
func (s *Service) routineModel(r Rule) string {
	if m := strings.TrimSpace(r.Model); m != "" && m != "default" {
		return m
	}
	if s.cfg.RoutinesModel != "" {
		return s.cfg.RoutinesModel
	}
	return "sonnet"
}

// buildRoutinePrompt is the run's prompt: the task, the memory of the past runs, what the agent may do and how it
// reports.
func (s *Service) buildRoutinePrompt(rule Rule, run Run) string {
	rt := rule.Routine
	port, tokenEnv := s.cfg.AutomationsPortEnv, s.cfg.AutomationsTokenEnv
	loc := routineZone(rule)
	now := s.now().In(loc)
	why := "по расписанию"
	if strings.HasPrefix(run.Fingerprint, "manual:") {
		why = "вручную — человек нажал «Запустить сейчас»"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Это регулярная задача «%s», запуск %s, %s (%s).\n", rule.Name, why, now.Format("2006-01-02 15:04"), loc.String())
	b.WriteString("Человека рядом нет: не задавай ему вопросов, доведи работу до итога сам.\n")
	b.WriteString(routineAutonomy[rt.Autonomy] + "\n")
	if rt.BudgetUSD > 0 {
		fmt.Fprintf(&b, "Уложись примерно в %.2f $ на запуск: не начинай долгих и дорогих обходов.\n", rt.BudgetUSD)
	}
	b.WriteString("\n## Задача\n\n")
	b.WriteString(strings.Replace(rule.PromptTemplate, "{{event}}", promptEvent(run.EventPayload), 1))
	b.WriteString("\n\n## Память с прошлых запусков\n\n")
	if strings.TrimSpace(rule.State) == "" {
		b.WriteString("Пусто: это первый запуск или память очистили.\n")
	} else {
		b.WriteString(rule.State + "\n")
	}
	b.WriteString("\nПиши в итог только новое по сравнению с памятью. Нового нет — исход quiet, короткая фраза почему.\n")
	if len(rt.Checks) > 0 {
		b.WriteString("\n## Проверки итога (их сделает хаб, итог без них считается сбоем)\n\n")
		for _, c := range rt.Checks {
			lbl := c.Label
			if lbl == "" {
				lbl = c.Kind + " " + c.Value
			}
			b.WriteString("- " + lbl + "\n")
		}
	}
	b.WriteString("\n## Куда итог\n\n")
	b.WriteString("Этот чат и есть чат запуска: полный ответ пиши здесь.\n")
	if rt.Delivery.has("file") {
		fmt.Fprintf(&b, "Запиши итог в файл %s новой версией (старые версии не трогай; если репозиторий — отдельным коммитом) и приложи его артефактом kind=file с title=%s.\n",
			expandFile(rt.Delivery.File, now), expandFile(rt.Delivery.File, now))
	}
	if rt.Delivery.has("doc") {
		b.WriteString("Опубликуй итог документом (новой версией того же документа, если он уже есть в памяти) и приложи артефактом kind=doc со ссылкой.\n")
	}
	if rt.Delivery.has("telegram") || rt.Delivery.has("messenger") || rt.Delivery.has("mail") {
		fmt.Fprintf(&b, "Политика доставки: %s. Если по ней человек должен услышать об этом исходе, отправь короткий итог (2–4 строки и ссылку на чат) через его подключённые каналы (`harnessly connectors`): %s. Отправленные каналы перечисли в delivered.\n",
			deliveryWords(rt.Delivery.When), strings.Join(externalChannels(rt.Delivery.Channels), ", "))
	}
	fmt.Fprintf(&b, `
## Как отчитаться (обязательно, одним запросом в самом конце)

Запиши отчёт в файл JSON и отправь его. Токен лежит в переменной окружения $%[1]s — не печатай его значение.

    {"runId": "%[2]s",
     "outcome": "ok | quiet | failed",
     "summary": "1–2 предложения: главное для человека",
     "body": "полный итог в markdown (необязательно)",
     "artifacts": [{"kind": "doc|file|chart|mr|board|link", "title": "…", "url": "…", "meta": "версия 12 / черновик / …"}],
     "state": "память для следующего запуска целиком: что уже сообщил, договорённости, ссылки на документы (до 4000 символов)",
     "delivered": ["telegram"],
     "error": "если failed — почему"}

    curl -sS -X POST "http://127.0.0.1:$%[3]s/automations/report" -H 'content-type: application/json' \
      -H "x-result-token: $%[1]s" --data @report.json

Ответ 200 — готово. Не удалось сделать задачу — всё равно отчитайся с outcome failed и причиной.
`, tokenEnv, run.ID, port)
	return b.String()
}

func deliveryWords(when string) string {
	switch when {
	case DeliverAlways:
		return "всегда"
	case DeliverFailure:
		return "только при сбое"
	case DeliverNever:
		return "никогда"
	}
	return "если есть новое или сбой (тихий исход — молчать)"
}

func externalChannels(ch []string) []string {
	var out []string
	for _, c := range ch {
		if c == "telegram" || c == "messenger" || c == "mail" {
			out = append(out, c)
		}
	}
	return out
}

func cleanArtifacts(in []Artifact) []Artifact {
	out := []Artifact{}
	for _, a := range in {
		a.Kind, a.Title = strings.TrimSpace(a.Kind), strings.TrimSpace(a.Title)
		if a.Title == "" {
			continue
		}
		if a.Kind == "" {
			a.Kind = "link"
		}
		if a.URL != "" && !strings.HasPrefix(a.URL, "https://") && !strings.HasPrefix(a.URL, "http://") && !strings.HasPrefix(a.URL, "/") {
			a.URL = ""
		}
		a.Title, a.Meta, a.URL = clip(a.Title, 200), clip(a.Meta, 200), clip(a.URL, 2000)
		out = append(out, a)
		if len(out) == maxArtifacts {
			break
		}
	}
	return out
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut]
}

// recordRoutine takes a routine run's report: the checks decide the outcome, the memory is kept, the streak counted.
func (s *Service) recordRoutine(ctx context.Context, rule Rule, rep Report) (Run, error) {
	var hash *string
	var status, fingerprint string
	if err := s.db.QueryRow(ctx, `SELECT result_token_hash, status, fingerprint FROM automation_runs WHERE id = ? AND rule_id = ?`, rep.RunID, rule.ID).Scan(&hash, &status, &fingerprint); err != nil {
		return Run{}, err
	}
	if hash == nil || subtle.ConstantTimeCompare([]byte(tokenHash(rep.ResultToken)), []byte(*hash)) != 1 {
		return Run{}, errBadToken
	}
	if status != StatusRunning {
		return Run{}, errors.New("run is " + status + ", not running")
	}
	said := rep.Outcome
	switch said {
	case OutcomeOK, OutcomeQuiet, OutcomeFailed:
	default:
		said = OutcomeOK
	}
	day := s.now().In(routineZone(rule))
	res := RoutineResult{Summary: clip(strings.TrimSpace(rep.Summary), 2000), Body: clip(rep.Body, maxBody), Artifacts: cleanArtifacts(rep.Artifacts),
		Delivered: rep.Delivered, Error: clip(rep.Error, 2000), Model: s.routineModel(rule), Manual: strings.HasPrefix(fingerprint, "manual:"), Said: said}
	outcome := said
	if outcome == OutcomeOK {
		res.Checks = runChecks(rule.Routine, rep, day)
		for _, c := range res.Checks {
			if !c.OK {
				outcome = OutcomeFailed
				if res.Error == "" {
					res.Error = "Проверка не прошла: " + c.Label
				}
			}
		}
	}
	raw, _ := json.Marshal(res)
	now := s.now()
	r, err := s.db.Exec(ctx, `UPDATE automation_runs SET status = 'done', outcome = ?, result = ?, summary = ?, bug_issue_id = ?, mr_url = ?, error = ?, finished_at = ?
		WHERE id = ? AND status = 'running'`, outcome, string(raw), res.Summary, nullable(rep.BugIssueID), nullable(rep.MRURL), nullable(res.Error), store.Ms(now), rep.RunID)
	if err != nil {
		return Run{}, err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return Run{}, errors.New("run is no longer running")
	}
	if rep.State != nil {
		st := clip(*rep.State, maxState)
		if _, err := s.db.Exec(ctx, `UPDATE automation_rules SET state = ? WHERE id = ?`, nullable(st), rule.ID); err != nil {
			s.log.Warn("routine memory not kept", "rule", rule.ID, "error", err)
		}
		rule.State = st
	}
	run, err := s.runByID(ctx, rep.RunID)
	if err != nil {
		return Run{}, err
	}
	s.afterRoutine(ctx, rule, run)
	return run, nil
}

// afterRoutine counts the streak, pauses after three failures, turns a one-time routine off and tells the person.
func (s *Service) afterRoutine(ctx context.Context, rule Rule, run Run) {
	streak := 0
	if run.Outcome == OutcomeFailed {
		streak = rule.FailStreak + 1
	}
	paused := ""
	if streak >= failStreakPause && rule.Enabled {
		paused = fmt.Sprintf("Пауза после %d сбоев подряд: %s", streak, clip(firstNonEmpty(runError(run), "агент не справился"), 300))
	}
	off := paused != "" || (rule.Routine != nil && rule.Routine.Once && run.Outcome != OutcomeFailed)
	q := `UPDATE automation_rules SET fail_streak = ?`
	args := []any{streak}
	if off {
		q += `, enabled = 0, paused_reason = ?`
		args = append(args, nullable(paused))
	}
	if _, err := s.db.Exec(ctx, q+` WHERE id = ?`, append(args, rule.ID)...); err != nil {
		s.log.Warn("routine streak not kept", "rule", rule.ID, "error", err)
	}
	if rule.Routine == nil {
		return
	}
	if paused != "" {
		s.ring(ctx, rule, "routine-paused:"+run.ID, "«"+rule.Name+"» на паузе", paused, "alert-triangle")
		return
	}
	if !rule.Routine.Delivery.has("notice") || !rule.Routine.Delivery.heard(run.Outcome) {
		return
	}
	title, text, icon := "«"+rule.Name+"»: готово", resultSummary(run), "repeat"
	switch run.Outcome {
	case OutcomeQuiet:
		title = "«" + rule.Name + "»: нового нет"
	case OutcomeFailed:
		title, icon = "«"+rule.Name+"»: сбой", "circle-alert"
		text = firstNonEmpty(runError(run), text)
	}
	s.ring(ctx, rule, "routine:"+run.ID, title, text, icon)
}

func runError(run Run) string {
	if run.Error != "" {
		return run.Error
	}
	var res RoutineResult
	_ = json.Unmarshal(run.Result, &res)
	return res.Error
}

func resultSummary(run Run) string {
	var res RoutineResult
	if json.Unmarshal(run.Result, &res) == nil && res.Summary != "" {
		return res.Summary
	}
	return run.Summary
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// ring puts the notice in the owner's bell, linked to the routine's page.
func (s *Service) ring(ctx context.Context, rule Rule, key, title, text, icon string) {
	if s.bell == nil {
		return
	}
	var email string
	if err := s.db.QueryRow(ctx, `SELECT email FROM users WHERE id = ?`, rule.OwnerID).Scan(&email); err != nil {
		return
	}
	s.bell(ctx, rule.OwnerID, email, icon, key, title, clip(text, 600), "/routines/"+rule.ID)
}

// sweepRoutines ends routine runs that never reported, counts every failure the agent did not report itself (a start
// that failed, a day without a machine), and turns off the routines past their last day.
func (s *Service) sweepRoutines(ctx context.Context) {
	now := s.now()
	if _, err := s.db.Exec(ctx, `UPDATE automation_runs SET status = 'failed', error = ?, finished_at = ?
		WHERE status = 'running' AND lease_until IS NULL AND dispatched_at < ?
		AND rule_id IN (SELECT id FROM automation_rules WHERE routine IS NOT NULL)`,
		"Агент не прислал итог за "+strconv.Itoa(int(routineTimeout.Hours()))+" ч", store.Ms(now), store.Ms(now.Add(-routineTimeout))); err != nil {
		s.log.Warn("routine runs not swept", "error", err)
		return
	}
	rows, err := s.db.Query(ctx, `SELECT `+prefixCols("r.", runCols)+` FROM automation_runs r JOIN automation_rules ar ON ar.id = r.rule_id
		WHERE ar.routine IS NOT NULL AND r.status = 'failed' AND r.outcome IS NULL ORDER BY r.created_at LIMIT 100`)
	if err != nil {
		return
	}
	var failed []Run
	for rows.Next() {
		if r, err := scanRun(rows); err == nil {
			failed = append(failed, r)
		}
	}
	rows.Close()
	for _, run := range failed {
		res, _ := json.Marshal(RoutineResult{Error: run.Error})
		u, err := s.db.Exec(ctx, `UPDATE automation_runs SET outcome = 'failed', result = ? WHERE id = ? AND outcome IS NULL`, string(res), run.ID)
		if err != nil {
			continue
		}
		if n, _ := u.RowsAffected(); n == 0 {
			continue
		}
		rule, err := s.ruleByID(ctx, run.RuleID)
		if err != nil {
			continue
		}
		run.Outcome, run.Result = OutcomeFailed, res
		s.afterRoutine(ctx, rule, run)
	}
	rules, err := s.rules(ctx, "", true)
	if err != nil {
		return
	}
	for _, r := range rules {
		if r.Routine == nil || r.Routine.Until == "" {
			continue
		}
		if now.In(routineZone(r)).Format("2006-01-02") > r.Routine.Until {
			_, _ = s.db.Exec(ctx, `UPDATE automation_rules SET enabled = 0, paused_reason = ? WHERE id = ?`, "Закончилась "+r.Routine.Until, r.ID)
		}
	}
}

// RoutineView is a routine with what the screen shows next to it.
type RoutineView struct {
	Rule
	Next  []string `json:"next"`
	Last  []RunDot `json:"last"`
	Stats Stats    `json:"stats"`
}

// RunDot is one of the last runs on the list.
type RunDot struct {
	ID      string `json:"id"`
	At      string `json:"at"`
	Status  string `json:"status"`
	Outcome string `json:"outcome,omitempty"`
}

// Stats are a routine's counts over its kept runs.
type Stats struct {
	Runs   int `json:"runs"`
	OK     int `json:"ok"`
	Quiet  int `json:"quiet"`
	Failed int `json:"failed"`
}

// nextSlots are the schedule's next starts.
func nextSlots(r Rule, from time.Time, n int) []string {
	out := []string{}
	if r.Trigger.Schedule == nil || !r.Enabled {
		return out
	}
	c, err := parseCron(r.Trigger.Schedule.Cron)
	if err != nil {
		return out
	}
	loc := routineZone(r)
	t := from.In(loc)
	for i := 0; i < n; i++ {
		nx, ok := c.next(t)
		if !ok {
			break
		}
		if r.Routine != nil && r.Routine.Until != "" && nx.Format("2006-01-02") > r.Routine.Until {
			break
		}
		out = append(out, nx.UTC().Format(time.RFC3339))
		t = nx
	}
	return out
}

func (s *Service) routineViews(ctx context.Context, owner string) ([]RoutineView, error) {
	rules, err := s.rules(ctx, owner, false)
	if err != nil {
		return nil, err
	}
	out := []RoutineView{}
	now := s.now()
	for _, r := range rules {
		if r.Routine == nil {
			continue
		}
		v := RoutineView{Rule: r, Next: nextSlots(r, now, 3), Last: []RunDot{}}
		rows, err := s.db.Query(ctx, `SELECT id, created_at, status, outcome FROM automation_runs WHERE rule_id = ? ORDER BY created_at DESC LIMIT 200`, r.ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var d RunDot
			var at int64
			var outc *string
			if err := rows.Scan(&d.ID, &at, &d.Status, &outc); err != nil {
				rows.Close()
				return nil, err
			}
			d.At = iso(at)
			if outc != nil {
				d.Outcome = *outc
			}
			if len(v.Last) < 10 {
				v.Last = append(v.Last, d)
			}
			switch d.Outcome {
			case OutcomeOK:
				v.Stats.OK++
			case OutcomeQuiet:
				v.Stats.Quiet++
			case OutcomeFailed:
				v.Stats.Failed++
			}
			if d.Outcome != "" {
				v.Stats.Runs++
			}
		}
		rows.Close()
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) listRoutines(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	views, err := s.routineViews(r.Context(), p.User.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"routines": views, "defaultModel": s.routineModel(Rule{}), "triggers": s.triggersOn(r.Context(), p.User.ID)})
	return nil
}

// runNow queues a run of the routine at once, outside its schedule.
func (s *Service) runNow(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	rule, err := ruleOrNotFound(s.ruleOf(r.Context(), r.PathValue("id"), p.User.ID))
	if err != nil {
		return err
	}
	out, err := s.queueManual(r.Context(), rule, p.User.Email)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusAccepted, out)
	return nil
}

func (s *Service) queueManual(ctx context.Context, rule Rule, by string) (QueueOutcome, error) {
	if !rule.Enabled {
		return QueueOutcome{}, httpx.New(http.StatusConflict, "paused", "Задача на паузе: сначала возобнови её")
	}
	now := s.now()
	event, _ := json.Marshal(map[string]any{"manual": map[string]any{"by": by, "at": now.UTC().Format(time.RFC3339)}})
	out, err := s.queueRun(ctx, rule, "manual:"+strconv.FormatInt(now.UnixMilli(), 10), event)
	if err != nil {
		return out, err
	}
	if !out.Queued {
		return out, httpx.New(http.StatusTooManyRequests, out.Reason, "Дневной лимит запусков исчерпан")
	}
	return out, nil
}

// FeedRun is a run of the person's rule for a service that builds the person's day (the inbox's brief, HRN-1653,
// HRN-1353): a routine's run (kind "routine": done with an outcome, its page in URL) or a trigger's (kind "trigger": an
// event, a schedule or a wait queued it; awaiting_approval waits for the person's ok, MRURL is what the ok merges).
type FeedRun struct {
	RunID     string     `json:"runId"`
	RuleID    string     `json:"ruleId"`
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	Source    string     `json:"source,omitempty"`
	Status    string     `json:"status"`
	Outcome   string     `json:"outcome,omitempty"`
	Summary   string     `json:"summary,omitempty"`
	Error     string     `json:"error,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	MRURL     string     `json:"mrUrl,omitempty"`
	At        string     `json:"at"`
	URL       string     `json:"url,omitempty"`
}

// Kinds of a FeedRun.
const (
	FeedRoutineRun = "routine"
	FeedTriggerRun = "trigger"
)

// FeedRoutine is a routine of the person for the day: what is next and whether it is paused.
type FeedRoutine struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Words  string `json:"words,omitempty"`
	Next   string `json:"next,omitempty"`
	Paused string `json:"paused,omitempty"`
	On     bool   `json:"on"`
	URL    string `json:"url"`
}

// feed answers a service with the launch token: the person's routines and their runs since a moment, for the day
// on the home screen. GET <p>/../routines/feed?person=<email>&since=<RFC3339>.
func (s *Service) feed(w http.ResponseWriter, r *http.Request) error {
	key := s.cfg.LaunchToken
	if len(key) < 32 {
		return httpx.ErrNotFound
	}
	given := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(given), []byte(key)) != 1 {
		return httpx.ErrForbidden
	}
	if !s.Enabled() {
		return errOff
	}
	person := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("person")))
	var owner string
	if err := s.db.QueryRow(r.Context(), `SELECT id FROM users WHERE LOWER(email) = ? AND disabled_at IS NULL`, person).Scan(&owner); err != nil {
		return httpx.New(http.StatusNotFound, "unknown_person", "Нет такого человека")
	}
	since := s.now().Add(-36 * time.Hour)
	if v := r.URL.Query().Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = t
		}
	}
	base := strings.TrimRight(s.cfg.PublicURL, "/")
	views, err := s.routineViews(r.Context(), owner)
	if err != nil {
		return err
	}
	routines := []FeedRoutine{}
	for _, v := range views {
		fr := FeedRoutine{ID: v.ID, Name: v.Name, Words: v.Routine.Words, Paused: v.PausedReason, On: v.Enabled, URL: base + "/routines/" + v.ID}
		if len(v.Next) > 0 {
			fr.Next = v.Next[0]
		}
		routines = append(routines, fr)
	}
	runs, err := s.feedRuns(r.Context(), owner, since, base)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"routines": routines, "runs": runs})
	return nil
}

// feedRuns are the runs of the person's rules since a moment, newest first: the routines' and the triggers'.
func (s *Service) feedRuns(ctx context.Context, owner string, since time.Time, base string) ([]FeedRun, error) {
	rows, err := s.db.Query(ctx, `SELECT `+prefixCols("r.", runCols)+`, ar.name, ar.routine IS NOT NULL, ar.trigger FROM automation_runs r
		JOIN automation_rules ar ON ar.id = r.rule_id
		WHERE ar.owner_id = ? AND r.created_at >= ? ORDER BY r.created_at DESC LIMIT 100`, owner, store.Ms(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []FeedRun{}
	for rows.Next() {
		var (
			name, trg string
			routine   bool
		)
		run, err := scanRun(alsoScan{rows, []any{&name, &routine, &trg}})
		if err != nil {
			return nil, err
		}
		var res RoutineResult
		_ = json.Unmarshal(run.Result, &res)
		at := run.CreatedAt
		if run.FinishedAt != "" {
			at = run.FinishedAt
		}
		fr := FeedRun{RunID: run.ID, RuleID: run.RuleID, Name: name, Kind: FeedRoutineRun, Status: run.Status, Outcome: run.Outcome,
			Summary: firstNonEmpty(res.Summary, run.Summary), Error: firstNonEmpty(res.Error, run.Error), Artifacts: res.Artifacts,
			MRURL: run.MRURL, At: at, URL: base + "/routines/" + run.RuleID + "?run=" + run.ID}
		if !routine {
			var t Trigger
			_ = json.Unmarshal([]byte(trg), &t)
			fr.Kind, fr.Source, fr.URL = FeedTriggerRun, t.Source, run.MRURL
		}
		runs = append(runs, fr)
	}
	return runs, rows.Err()
}

// alsoScan reads the columns a query has after a run's.
type alsoScan struct {
	rows  rowScanner
	extra []any
}

func (a alsoScan) Scan(dest ...any) error { return a.rows.Scan(append(dest, a.extra...)...) }
