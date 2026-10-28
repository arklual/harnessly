package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const (
	codeTTL     = 10 * time.Minute
	resendAfter = 60 * time.Second
	maxAttempts = 5

	emailPerAddress = 5  // codes per address per hour
	emailPerIP      = 20 // codes per IP (IPv6 by /64) per hour
	signinPerIP     = 30 // sign-in attempts per IP per 10 minutes
	verifyPerEmail  = 10 // code checks per address per 10 minutes, whoever asks and from wherever
)

var sixDigits = regexp.MustCompile(`^[0-9]{6}$`)

type emailChallengeData struct {
	Next string `json:"next,omitempty"`
}

func codeHash(challengeID, code string) string { return store.HashToken(challengeID + ":" + code) }

func randomCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%06d", n.Int64())
}

// limitSignin counts one sign-in attempt of the caller's address.
func (s *Service) limitSignin(r *http.Request) error {
	if ok, wait := s.lim.Allow("signin:"+ratelimit.IPKey(s.ClientIP(r)), signinPerIP, 10*time.Minute); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(wait))
	}
	return nil
}

// emailStart answers "send me a code". The answer is the same whether the account exists or not; the one thing it
// may reveal is that signing up is closed.
func (s *Service) emailStart(w http.ResponseWriter, r *http.Request) error {
	var req struct{ Email, Intent, Next string }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	email, ok := NormalizeEmail(req.Email)
	if !ok {
		return httpx.Invalid("email", "Укажите почту, например name@company.ru")
	}
	if req.Intent != "" && req.Intent != "signin" && req.Intent != "signup" {
		return httpx.Invalid("intent", "Неизвестное намерение")
	}
	ctx := r.Context()
	st := s.db.S()

	// A fresh code is not sent while the last one is under a minute old.
	if prev, err := st.LatestChallenge(ctx, "email", email); err == nil {
		if wait := prev.CreatedAt.Add(resendAfter).Sub(s.now()); wait > 0 {
			return httpx.RateLimited(ratelimit.RetrySeconds(wait))
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	ip := ratelimit.IPKey(s.ClientIP(r))
	if ok, wait := s.lim.Allow("email-ip:"+ip, emailPerIP, time.Hour); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(wait))
	}
	if ok, wait := s.lim.Allow("email:"+email, emailPerAddress, time.Hour); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(wait))
	}

	// An account that does not exist yet is made when the code is verified, if signing up is allowed.
	if _, err := st.UserByEmail(ctx, email); errors.Is(err, store.ErrNotFound) {
		if err := s.signupAllowed(ctx, st, email, req.Next, true); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	code := randomCode()
	id := store.RandomBase32(16)
	data, _ := json.Marshal(emailChallengeData{Next: SafeNext(req.Next)})
	now := s.now().UTC()
	if err := st.DeleteChallengesByEmail(ctx, "email", email); err != nil {
		return err
	}
	if err := st.CreateChallenge(ctx, store.Challenge{
		ID: id, Kind: "email", Email: email, CodeHash: codeHash(id, code), Data: string(data),
		ExpiresAt: now.Add(codeTTL), CreatedAt: now, IP: s.ClientIP(r),
	}); err != nil {
		return err
	}
	if err := s.mailer.SendCode(ctx, email, code, codeTTL); err != nil {
		_, _ = st.ConsumeChallenge(ctx, id)
		s.log.Error("sign-in code not sent", "error", err)
		return httpx.New(http.StatusInternalServerError, "internal", "Не удалось отправить письмо, попробуйте позже")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]int{"resendAfter": int(resendAfter.Seconds()), "expiresIn": int(codeTTL.Seconds())})
	return nil
}

// emailVerify checks the code and signs the person in, making the account if it is new.
func (s *Service) emailVerify(w http.ResponseWriter, r *http.Request) error {
	var req struct{ Email, Code, Next string }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	email, ok := NormalizeEmail(req.Email)
	if !ok {
		return httpx.Invalid("email", "Укажите почту")
	}
	if !sixDigits.MatchString(req.Code) {
		return httpx.Invalid("code", "Код состоит из шести цифр")
	}
	if err := s.limitSignin(r); err != nil {
		return err
	}
	// The limit per address is what holds when the guesses come from many addresses at once.
	if ok, wait := s.lim.Allow("email-verify:"+email, verifyPerEmail, 10*time.Minute); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(wait))
	}
	ctx := r.Context()
	st := s.db.S()
	ch, err := st.LatestChallenge(ctx, "email", email)
	if errors.Is(err, store.ErrNotFound) {
		return httpx.ErrExpired
	}
	if err != nil {
		return err
	}
	if !s.now().Before(ch.ExpiresAt) {
		_, _ = st.ConsumeChallenge(ctx, ch.ID)
		return httpx.ErrExpired
	}
	// The try is spent before the code is looked at, atomically: with the count made after the comparison, parallel
	// requests would all compare against the same unspent challenge and could try many more codes than maxAttempts.
	n, ok, err := st.TakeAttempt(ctx, ch.ID, maxAttempts)
	if errors.Is(err, store.ErrNotFound) {
		return httpx.ErrExpired
	}
	if err != nil {
		return err
	}
	if !ok {
		_, _ = st.ConsumeChallenge(ctx, ch.ID)
		return httpx.ErrExpired
	}
	if subtle.ConstantTimeCompare([]byte(ch.CodeHash), []byte(codeHash(ch.ID, req.Code))) != 1 {
		if n >= maxAttempts {
			_, _ = st.ConsumeChallenge(ctx, ch.ID)
			return httpx.ErrExpired
		}
		return httpx.New(http.StatusBadRequest, "invalid_code", "Неверный код").With("attemptsLeft", maxAttempts-n)
	}
	// Single use, even when two requests with the right code race.
	if used, err := st.ConsumeChallenge(ctx, ch.ID); err != nil {
		return err
	} else if !used {
		return httpx.ErrExpired
	}

	var data emailChallengeData
	_ = json.Unmarshal([]byte(ch.Data), &data)
	next := SafeNext(req.Next)
	if next == "" {
		next = data.Next
	}
	u, isNew, err := s.resolve(ctx, identityInfo{Provider: providerEmail, Subject: email, Email: email, EmailVerified: true}, resolveOpts{Next: next})
	if err != nil {
		return err
	}
	return s.signedIn(ctx, w, r, u, isNew, "email", next)
}
