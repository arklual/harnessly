package terminalfailure

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Rejected is a refusal of the request itself (a key that is not valid, no access, a malformed request): asking again
// gets the same answer, so the turn stops at once instead of after the CLI's minutes of retries.
const Rejected Kind = "request_rejected"

// Retry is what a failed call to the model's gateway calls for.
type Retry int

const (
	// RetryAgain: the failure may pass (no answer, a throttle, the vendor's own error); the CLI retries as before.
	RetryAgain Retry = iota
	// RetryNever: the gateway refused the request; the turn stops with the reason.
	RetryNever
)

// ClassifyStatus says whether an answer of the gateway with this HTTP status is worth retrying, and why not when it is
// not: a reason a person reads in the feed. Status 0 is no answer at all (the network).
func ClassifyStatus(status int) (Retry, string) {
	switch {
	case status == 0, status == http.StatusRequestTimeout, status == http.StatusConflict,
		status == http.StatusTooManyRequests, status >= 500:
		return RetryAgain, ""
	case status == http.StatusUnauthorized:
		return RetryNever, "Ключ доступа к модели недействителен"
	case status == http.StatusForbidden:
		return RetryNever, "Нет доступа к модели"
	case status == http.StatusBadRequest:
		return RetryNever, "Неверный запрос к модели"
	case status == http.StatusPaymentRequired:
		return RetryNever, "Оплата доступа к модели не прошла"
	case status == http.StatusNotFound:
		return RetryNever, "Модель не найдена"
	case status == http.StatusRequestEntityTooLarge:
		return RetryNever, "Запрос к модели слишком большой"
	case status >= 400:
		return RetryNever, "Шлюз модели отклонил запрос"
	}
	return RetryAgain, ""
}

var (
	// retryLineRE is a CLI's notice that it is about to send the call again: «· Retrying in 5s · attempt 1/10»,
	// «Retrying in 5 seconds… (attempt 1/10)».
	retryLineRE = regexp.MustCompile(`(?i)\bretrying\s+in\b|\battempt\s+\d+\s*/\s*\d+`)
	// retryStatusRE is the status of the failed call on that notice: «API Error (403 …)», «API Error: 401 …», or the
	// bare status the spinner line starts with («✻ 403 Forbidden · Retrying in …»).
	retryStatusRE = regexp.MustCompile(`(?i)(?:\bapi\s*error\s*[:(]?\s*|^[^\p{L}\d]*)([1-5]\d\d)\b`)
)

// retryNotice reads a CLI's retry notice: notice is false for any other line; ok says the gateway refused the call,
// which the CLI would retry for minutes without a chance. A notice of a retryable status (429, 5xx, no answer) is the
// latest word of the CLI: what failed before it has been retried past.
func retryNotice(record string) (f Failure, notice, ok bool) {
	if !retryLineRE.MatchString(record) {
		return Failure{}, false, false
	}
	m := retryStatusRE.FindStringSubmatch(record)
	if m == nil {
		return Failure{}, true, false
	}
	status, _ := strconv.Atoi(m[1])
	retry, reason := ClassifyStatus(status)
	if retry != RetryNever {
		return Failure{}, true, false
	}
	f, _ = failure(reason + " (" + m[1] + "): " + record)
	f.Kind, f.Status, f.NeedsTarget, f.Line = Rejected, status, false, strings.TrimSpace(spacesRE.ReplaceAllString(record, " "))
	return f, true, true
}
