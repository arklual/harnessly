package asks

import (
	"context"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/notifybots"
)

// A bot's poller reads its Telegram updates while an ask through it waits: a button press or a reply closes the ask.
// It stops when nothing waits; a new ask starts it again. Telegram gives one bot's updates to one reader at a time,
// so a binding of a chat in Settings and the poller take turns: each retries after the other's "conflict".

const defaultPollWait = 25 * time.Second

const defaultErrPause = 2 * time.Second

func (s *Service) pollWait() time.Duration {
	if s.PollWait > 0 {
		return s.PollWait
	}
	return defaultPollWait
}

func (s *Service) errPause() time.Duration {
	if s.ErrPause > 0 {
		return s.ErrPause
	}
	return defaultErrPause
}

// Start resumes the pollers of the asks that wait (after a restart of the runtime).
func (s *Service) Start() {
	asks, err := s.Store.List()
	if err != nil {
		return
	}
	for _, a := range asks {
		if a.Status == StatusWaiting {
			s.ensurePoller(a.BotID)
		}
	}
}

// Stop ends every poller and waits until they are gone, so nothing writes to the store after it returns (tests; the
// process ends with the runtime otherwise).
func (s *Service) Stop() {
	s.mu.Lock()
	for id, cancel := range s.pollers {
		cancel()
		delete(s.pollers, id)
	}
	s.mu.Unlock()
	s.running.Wait() // outside the lock: a poller takes it on its way out
}

func (s *Service) ensurePoller(bot string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pollers == nil {
		s.pollers = map[string]context.CancelFunc{}
	}
	if s.pollers[bot] != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.pollers[bot] = cancel
	s.running.Add(1)
	go func() {
		defer s.running.Done()
		s.poll(ctx, bot)
	}()
}

// waitingOn are the asks through a bot that wait, the overdue ones closed on the way.
func (s *Service) waitingOn(bot string) []Ask {
	asks, err := s.Store.List()
	if err != nil {
		return nil
	}
	var out []Ask
	for _, a := range asks {
		if a.BotID != bot || a.Status != StatusWaiting {
			continue
		}
		if a = s.expire(a); a.Status == StatusWaiting {
			out = append(out, a)
		}
	}
	return out
}

func (s *Service) poll(ctx context.Context, botID string) {
	done := func() bool {
		// Under the lock: a new ask that comes now sees no poller and starts one.
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.waitingOnLocked(botID)) > 0 && ctx.Err() == nil {
			return false
		}
		if s.pollers[botID] != nil {
			s.pollers[botID]()
			delete(s.pollers, botID)
		}
		return true
	}
	pause := func(d time.Duration) {
		select {
		case <-ctx.Done():
		case <-time.After(d):
		}
	}
	for {
		s.waitingOn(botID) // closes the overdue ones
		if done() {
			return
		}
		bot, err := s.Bots.Get(botID)
		if err != nil {
			s.failWaiting(botID, "the bot was removed")
			continue
		}
		offset := s.Store.Offset(botID)
		asked := time.Now()
		updates, err := s.Telegram.GetFullUpdates(ctx, bot.Token, offset, s.pollWait())
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if notifybots.ReasonOf(err) == notifybots.ReasonInvalidToken {
				s.failWaiting(botID, "Telegram no longer takes the bot's token")
				continue
			}
			// The binding reads now, the webhook is set, a network hiccup or Telegram's 429: later.
			pause(s.errPause())
			continue
		}
		if len(updates) == 0 && time.Since(asked) < s.pollWait()/10 {
			pause(s.errPause())
		}
		next := offset
		for _, u := range updates {
			if u.UpdateID >= next {
				next = u.UpdateID + 1
			}
			s.handle(ctx, bot, u)
		}
		if next != offset {
			_ = s.Store.SetOffset(botID, next)
		}
	}
}

// waitingOnLocked is waitingOn for a caller that holds s.mu: it reads the file only (closing waits for the next turn).
func (s *Service) waitingOnLocked(bot string) []Ask {
	asks, err := s.Store.List()
	if err != nil {
		return nil
	}
	var out []Ask
	for _, a := range asks {
		if a.BotID == bot && a.Status == StatusWaiting {
			out = append(out, a)
		}
	}
	return out
}

func (s *Service) failWaiting(bot, why string) {
	for _, a := range s.waitingOn(bot) {
		s.close(a.ID, func(x *Ask) { x.Status, x.Error = StatusFailed, why })
	}
}

func personOf(u *notifybots.User) Person {
	if u == nil {
		return Person{}
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	return Person{Name: name, Username: u.Username, TelegramID: u.ID}
}

// addressedTo says whether the one who answered is the person the ask is for (anyone when it is for the chat).
func addressedTo(a Ask, p Person) bool {
	if a.To == nil {
		return true
	}
	if a.To.TelegramID != 0 && a.To.TelegramID == p.TelegramID {
		return true
	}
	return a.To.Username != "" && strings.EqualFold(a.To.Username, p.Username)
}

func (s *Service) mayAnswer(a Ask, p Person) bool {
	if addressedTo(a, p) {
		return true
	}
	if a.Kind != KindApproval {
		return false
	}
	set, err := s.Settings.Get()
	if err != nil {
		return false
	}
	for _, c := range set.Contacts {
		if c.CanApprove && addressedTo(Ask{To: &Person{Username: c.Username, TelegramID: c.TelegramID}}, p) {
			return true
		}
	}
	return false
}

func (s *Service) handle(ctx context.Context, bot notifybots.Bot, u notifybots.FullUpdate) {
	if q := u.CallbackQuery; q != nil {
		s.press(ctx, bot, q)
		return
	}
	m := u.Message
	if m == nil || m.From == nil || m.From.IsBot {
		return
	}
	text := strings.TrimSpace(m.Text)
	if text == "" {
		text = strings.TrimSpace(m.Caption)
	}
	if text == "" {
		return
	}
	from := personOf(m.From)
	s.Settings.Learn(from)
	reply := Reply{From: from, Text: text, Via: "telegram", At: m.Date * 1000}
	if reply.At == 0 {
		reply.At = s.now().UnixMilli()
	}
	asks, err := s.Store.List()
	if err != nil {
		return
	}
	// A reply to the ask's message: the answer while it waits (from the person it is for), a late word after.
	if m.ReplyTo != nil {
		for _, a := range asks {
			if a.BotID != bot.ID || a.ChatID != m.Chat.ID || a.MessageID != m.ReplyTo.MessageID {
				continue
			}
			if a.Status == StatusWaiting && s.mayAnswer(a, from) {
				_, _ = s.answer(a.ID, reply)
			} else {
				_, _ = s.addReply(a.ID, reply)
			}
			return
		}
		return
	}
	// A message without a reply: the answer when exactly one ask in this chat waits for this person, by name.
	var only *Ask
	for i, a := range asks {
		if a.BotID != bot.ID || a.ChatID != m.Chat.ID || a.Status != StatusWaiting || a.To == nil || !addressedTo(a, from) ||
			reply.At < a.CreatedAt-1000 {
			continue
		}
		if only != nil {
			return
		}
		only = &asks[i]
	}
	if only == nil && m.Chat.Type == "private" {
		// In a private chat with the bot every message is to the bot: the one waiting ask there takes it.
		for i, a := range asks {
			if a.BotID == bot.ID && a.ChatID == m.Chat.ID && a.Status == StatusWaiting && reply.At >= a.CreatedAt-1000 {
				if only != nil {
					return
				}
				only = &asks[i]
			}
		}
	}
	if only != nil {
		_, _ = s.answer(only.ID, reply)
	}
}

func (s *Service) press(ctx context.Context, bot notifybots.Bot, q *notifybots.CallbackQuery) {
	toast := ""
	defer func() {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = s.Telegram.AnswerCallback(cctx, bot.Token, q.ID, toast)
		cancel()
	}()
	id, choice, ok := parseData(q.Data)
	if !ok {
		return
	}
	a, err := s.Store.Get(id)
	if err != nil || a.BotID != bot.ID {
		toast = locale.Pick("This question is gone", "Этого вопроса больше нет")
		return
	}
	from := personOf(&q.From)
	if a.Status.Final() {
		toast = locale.Pick("The question is closed already", "Вопрос уже закрыт")
		return
	}
	if !s.mayAnswer(a, from) {
		toast = locale.Pick("This question is for ", "Этот вопрос адресован ") + a.To.Label()
		return
	}
	s.Settings.Learn(from)
	if _, err := s.answer(id, Reply{From: from, Choice: choice, Via: "telegram", At: s.now().UnixMilli()}); err != nil {
		toast = locale.Pick("Not taken: ", "Не принято: ") + RefusalOf(err).Reason
		return
	}
	toast = locale.Pick("Done, the agent got it", "Готово, агент получил ответ")
}
