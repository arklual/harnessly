// Package caldav is the calendar connector (docs/connectors.md): any CalDAV calendar (Yandex, iCloud, Fastmail,
// Nextcloud, a server of one's own) found by .well-known/caldav and the principal, or, read-only, a calendar's secret
// .ics address (Google, Outlook). Its source tells a meeting about to start (calendar.starting), one created, moved,
// changed or cancelled (calendar.changed), one that ended (calendar.ended) and the day's schedule in the morning
// (calendar.day); the agent reads the events of a period, the free windows and one meeting whole, and proposes to
// create, move or cancel a meeting or to answer an invitation, which runs only on the person's yes.
package caldav

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// ID names the connector.
const ID = "caldav"

// Event types.
const (
	EventStarting = "calendar.starting"
	EventChanged  = "calendar.changed"
	EventEnded    = "calendar.ended"
	EventDay      = "calendar.day"
)

// Providers of the form: each a preset of the CalDAV address, or the read-only link.
const (
	Yandex    = "yandex"
	ICloud    = "icloud"
	Fastmail  = "fastmail"
	Nextcloud = "nextcloud"
	Other     = "other"
	ICS       = "ics"
)

// presets are the CalDAV addresses of the known providers; the principal is found from there.
var presets = map[string]string{
	Yandex:   "https://caldav.yandex.ru",
	ICloud:   "https://caldav.icloud.com",
	Fastmail: "https://caldav.fastmail.com",
}

const (
	defaultRemind  = 15
	defaultMorning = 8
)

var caldavProviders = []string{Yandex, ICloud, Fastmail, Nextcloud, Other}

type calConn struct{}

func init() { connectors.Register(calConn{}) }

func (calConn) Spec() connectors.Spec {
	id := connectors.Arg{Name: "id", Summary: "the meeting's id from events or from an event's ref", Required: true}
	return connectors.Spec{
		ID:    ID,
		Kind:  connectors.Calendar,
		Title: connectors.T("Calendar", "Календарь"),
		Summary: connectors.T("Yandex, iCloud, Fastmail, Nextcloud or any CalDAV server; Google and Outlook read-only by the calendar's secret .ics address",
			"Яндекс, iCloud, Fastmail, Nextcloud или любой сервер CalDAV; Google и Outlook — только чтение по секретному адресу .ics"),
		Fields: []connectors.Field{
			{ID: "provider", Type: connectors.FieldChoice, Default: Yandex, Label: connectors.T("Where the calendar is", "Где календарь"),
				Choices: []connectors.Choice{
					{Value: Yandex, Label: connectors.T("Yandex", "Яндекс")},
					{Value: ICloud, Label: connectors.T("iCloud", "iCloud")},
					{Value: Fastmail, Label: connectors.T("Fastmail", "Fastmail")},
					{Value: Nextcloud, Label: connectors.T("Nextcloud", "Nextcloud")},
					{Value: Other, Label: connectors.T("Other CalDAV", "Другой CalDAV")},
					{Value: ICS, Label: connectors.T("Link .ics (read only)", "Ссылка .ics (только чтение)")},
				}},
			{ID: "server", Type: connectors.FieldURL, Label: connectors.T("CalDAV address", "Адрес CalDAV"),
				Placeholder: "https://cloud.example.com/remote.php/dav",
				Hint:        connectors.T("The server's address; the calendars are found from it", "Адрес сервера; календари найдутся по нему"),
				When:        &connectors.When{Field: "provider", Values: []string{Nextcloud, Other}}},
			{ID: "username", Type: connectors.FieldText, Label: connectors.T("Login", "Логин"), Placeholder: "me@example.com",
				When: &connectors.When{Field: "provider", Values: caldavProviders}},
			{ID: "password", Type: connectors.FieldPassword, Secret: true, Label: connectors.T("App password", "Пароль приложения"),
				Hint: connectors.T("Not the account's password: Yandex ID → Security → App passwords, Apple ID → App-Specific Passwords, Fastmail → Privacy & Security → App passwords",
					"Не пароль от аккаунта: Яндекс ID → Безопасность → Пароли приложений, Apple ID → Пароли приложений, Fastmail → Privacy & Security → App passwords"),
				When: &connectors.When{Field: "provider", Values: caldavProviders}},
			{ID: "link", Type: connectors.FieldURL, Secret: true, Label: connectors.T("Secret address in iCal format", "Секретный адрес в формате iCal"),
				Placeholder: "https://calendar.example.com/…/basic.ics",
				Hint: connectors.T("Google: calendar settings → Secret address in iCal format. Outlook: Calendar → Shared calendars → Publish → ICS. Read only: nothing can be created or changed this way",
					"Google: настройки календаря → Закрытый адрес в формате iCal. Outlook: Календарь → Общие календари → Опубликовать → ICS. Только чтение: создать или изменить встречу так нельзя"),
				When: &connectors.When{Field: "provider", Values: []string{ICS}}},
			{ID: "calendars", Type: connectors.FieldChoices, Discover: true, Optional: true, Label: connectors.T("Calendars", "Календари"),
				Hint: connectors.T("None picked: all of them", "Ничего не выбрано — все"),
				When: &connectors.When{Field: "provider", Values: caldavProviders}},
			{ID: "remind", Type: connectors.FieldNumber, Optional: true, Default: strconv.Itoa(defaultRemind),
				Label: connectors.T("Tell about a meeting, minutes before", "Сообщать о встрече за, минут")},
			{ID: "morning", Type: connectors.FieldNumber, Optional: true, Default: strconv.Itoa(defaultMorning),
				Label: connectors.T("The day's schedule at, hour", "Расписание дня в, час"),
				Hint:  connectors.T("0: no schedule in the morning", "0 — без утреннего расписания")},
			{ID: "zone", Type: connectors.FieldText, Optional: true, Label: connectors.T("Time zone", "Часовой пояс"), Placeholder: "Europe/Moscow",
				Hint: connectors.T("Empty: the calendar's zone, else the machine's", "Пусто — пояс календаря, иначе машины")},
		},
		Events: []string{EventStarting, EventChanged, EventEnded, EventDay},
		Tools: []connectors.Tool{
			{Name: "events", Summary: "the meetings of a period: time, title, place, call link, people, id", Args: []connectors.Arg{
				{Name: "from", Summary: "first day, YYYY-MM-DD, today by default"},
				{Name: "to", Summary: "last day, YYYY-MM-DD, a week after from by default"},
				{Name: "calendar", Summary: "only the calendar of this name"},
			}},
			{Name: "free", Summary: "the free windows of a day or a week within working hours", Args: []connectors.Arg{
				{Name: "date", Summary: "first day, YYYY-MM-DD, today by default"},
				{Name: "days", Summary: "how many days, 1 by default, 7 for a week (at most 14)"},
				{Name: "from", Summary: "start of the working day, HH:MM, 09:00 by default"},
				{Name: "to", Summary: "end of the working day, HH:MM, 19:00 by default"},
				{Name: "min", Summary: "shortest window in minutes, 30 by default"},
			}},
			{Name: "event", Summary: "one meeting whole: people with their answers, organizer, place, link, description", Args: []connectors.Arg{id}},
		},
		Actions: []connectors.Action{
			{Name: "create", Summary: "create a meeting; with attendees, the server sends them invitations",
				Title: connectors.T("Create the meeting", "Создать встречу"),
				Args: []connectors.Arg{
					{Name: "title", Summary: "what it is", Required: true},
					{Name: "start", Summary: "YYYY-MM-DDTHH:MM in the calendar's zone, or YYYY-MM-DD for a whole day", Required: true},
					{Name: "end", Summary: "YYYY-MM-DDTHH:MM; or minutes"},
					{Name: "minutes", Summary: "how long, 30 by default"},
					{Name: "location", Summary: "place or call link"},
					{Name: "attendees", Summary: "e-mail addresses, comma-separated"},
					{Name: "description", Summary: "the text of the invitation"},
					{Name: "calendar", Summary: "the calendar's name, the first one connected by default"},
				}},
			{Name: "move", Summary: "move a meeting (one time of a repeating one: that time only)",
				Title: connectors.T("Move the meeting", "Перенести встречу"),
				Args: []connectors.Arg{id,
					{Name: "start", Summary: "new start, YYYY-MM-DDTHH:MM", Required: true},
					{Name: "end", Summary: "new end; the length is kept by default"},
				}},
			{Name: "cancel", Summary: "cancel a meeting one organizes (one time of a repeating one: that time only); the server tells the attendees",
				Title: connectors.T("Cancel the meeting", "Отменить встречу"), Args: []connectors.Arg{id}},
			{Name: "reply", Summary: "answer an invitation: accepted, declined or tentative",
				Title: connectors.T("Answer the invitation", "Ответить на приглашение"),
				Args:  []connectors.Arg{id, {Name: "answer", Summary: "accepted, declined or tentative", Required: true}}},
		},
		Inbox: "calendar",
	}
}

// account is a connection made ready: a CalDAV client and its chosen calendars, or the link of a read-only file.
type account struct {
	link     string // the .ics address, read-only mode
	dav      *dav
	me       map[string]bool
	cals     []calendar
	loc      *time.Location
	remind   time.Duration
	morning  int
	userZone bool
}

func readOnly(in connectors.Instance) bool { return in.Config["provider"] == ICS }

func number(in connectors.Instance, field string, def, lo, hi int) int {
	n, err := strconv.Atoi(strings.TrimSpace(in.Config[field]))
	if err != nil {
		return def
	}
	return min(max(n, lo), hi)
}

func settings(in connectors.Instance, a *account) {
	a.remind = time.Duration(number(in, "remind", defaultRemind, 1, 24*60)) * time.Minute
	a.morning = number(in, "morning", defaultMorning, 0, 23)
	a.loc = time.Local
	if z := strings.TrimSpace(in.Config["zone"]); z != "" {
		if loc, err := time.LoadLocation(z); err == nil {
			a.loc, a.userZone = loc, true
		}
	}
}

func serverOf(in connectors.Instance) string {
	if u, ok := presets[in.Config["provider"]]; ok {
		return u
	}
	return in.Config["server"]
}

// login makes the CalDAV client of a connection and finds its calendars; all is the whole list, picked the chosen.
func login(ctx context.Context, in connectors.Instance) (a *account, all []calendar, err error) {
	a = &account{}
	settings(in, a)
	if readOnly(in) {
		link, err := in.Secret("link")
		if err != nil || link == "" {
			return nil, nil, connectors.Sayf("The calendar's address is not in the safe")
		}
		a.link = strings.Replace(link, "webcal://", "https://", 1)
		return a, nil, nil
	}
	password, err := in.Secret("password")
	if err != nil {
		return nil, nil, connectors.Sayf("The password is not in the safe")
	}
	user := strings.TrimSpace(in.Config["username"])
	d, err := newDAV(serverOf(in), user, password, in.HTTP)
	if err != nil {
		return nil, nil, err
	}
	a.dav = d
	principal, err := d.principal(ctx)
	if err != nil {
		return nil, nil, say(err)
	}
	home, addrs, err := d.home(ctx, principal)
	if err != nil {
		return nil, nil, say(err)
	}
	all, err = d.calendars(ctx, home)
	if err != nil {
		return nil, nil, say(err)
	}
	if len(all) == 0 {
		return nil, nil, connectors.Sayf("The account has no calendars with events")
	}
	a.me = map[string]bool{}
	for _, addr := range addrs {
		if e, ok := strings.CutPrefix(strings.ToLower(addr), "mailto:"); ok {
			a.me[e] = true
		}
	}
	if strings.Contains(user, "@") {
		a.me[strings.ToLower(user)] = true
	}
	picked := map[string]bool{}
	for _, h := range strings.Split(in.Config["calendars"], ",") {
		if h = strings.TrimSpace(h); h != "" {
			picked[h] = true
		}
	}
	for _, c := range all {
		if len(picked) == 0 || picked[c.Href] {
			a.cals = append(a.cals, c)
		}
	}
	if len(a.cals) == 0 {
		return nil, nil, connectors.Sayf("The chosen calendars are not in the account any more")
	}
	if !a.userZone {
		for _, c := range a.cals {
			if c.Zone != "" {
				a.loc = zone(c.Zone, a.loc)
				break
			}
		}
	}
	return a, all, nil
}

// say turns a failure of the server into words for the person.
func say(err error) error {
	var said *connectors.Said
	if errors.As(err, &said) {
		return err
	}
	switch statusOf(err) {
	case http.StatusUnauthorized:
		return connectors.Sayf("The server did not take the login and password. CalDAV needs an app password, not the account's own")
	case http.StatusForbidden:
		return connectors.Sayf("The server refused: the account has no access to these calendars")
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return connectors.Sayf("There is no CalDAV at this address")
	case http.StatusPreconditionFailed:
		return connectors.Sayf("Someone changed the meeting in the meantime: read it again and propose anew")
	case 0:
	default:
		return connectors.Sayf("The calendar server answered %d", statusOf(err))
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	s := err.Error()
	if len(s) > 160 {
		s = s[:160]
	}
	return connectors.Sayf("The calendar server does not answer: %s", s)
}

func (calConn) Check(ctx context.Context, in connectors.Instance) (connectors.Account, error) {
	a, _, err := login(ctx, in)
	if err != nil {
		return connectors.Account{}, err
	}
	if a.link != "" {
		f, err := fetchICS(ctx, in.HTTP, a.link, "", "")
		if err != nil {
			return connectors.Account{}, err
		}
		label := f.name
		if label == "" {
			u, _ := url.Parse(a.link)
			label = u.Host
		}
		return connectors.Account{Label: label}, nil
	}
	label := strings.TrimSpace(in.Config["username"])
	if label == "" {
		label = a.dav.base.Host
	}
	return connectors.Account{Label: label}, nil
}

// Discover lists the account's calendars for the form: the href is the value, the name the label.
func (calConn) Discover(ctx context.Context, in connectors.Instance, field string) ([]connectors.Choice, error) {
	if field != "calendars" || readOnly(in) {
		return nil, connectors.ErrUnknownTool
	}
	_, all, err := login(ctx, in)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(all, func(i, j int) bool { return !all[i].ReadOnly && all[j].ReadOnly })
	out := make([]connectors.Choice, 0, len(all))
	for _, c := range all {
		name := c.Name
		ru := name
		if c.ReadOnly {
			name += " (read only)"
			ru += " (только чтение)"
		}
		out = append(out, connectors.Choice{Value: c.Href, Label: connectors.T(name, ru)})
	}
	return out, nil
}

// CanAct refuses every action of a calendar connected by its .ics address before a card is shown.
func (calConn) CanAct(in connectors.Instance, action string) error {
	if readOnly(in) {
		return connectors.Sayf("This calendar is connected by its .ics address and is read only: nothing can be created, moved, cancelled or answered. Connect it by CalDAV for that")
	}
	return nil
}
