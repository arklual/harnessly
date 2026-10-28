package prompts

import "github.com/ly-solutions/harnessly/internal/locale"

// The texts the routes answer people with. Each is picked by RUNTIME_LOCALE when the answer is built; the Russian ones
// are the previous runtime's texts, byte for byte.
var (
	msgNeedTabAction   = locale.T("tabId and action are required", "Нужны tabId и action")
	msgNotUploadImage  = locale.T("The image is not from uploads", "Картинка не из загрузок")
	msgNeedID          = locale.T("id is required", "Нужен id")
	msgNotQueued       = locale.T("The message is no longer in the queue", "Сообщения уже нет в очереди")
	msgNeedTabText     = locale.T("tabId and text are required", "Нужны tabId и text")
	msgTooLong         = locale.T("The message is too long", "Сообщение слишком длинное")
	msgCodexSubmit     = locale.T("Codex only accepts a message that is submitted", "Codex принимает сообщение только с отправкой")
	msgTypeFailed      = locale.T("Could not pass the message to the agent", "Не удалось передать сообщение агенту")
	msgTypeRejected    = locale.T("The agent did not accept the message; it is still in its input box", "Агент не принял сообщение — оно осталось в его поле ввода")
	msgNeedTab         = locale.T("tabId is required", "Нужен tabId")
	msgUnknownModel    = locale.T("Unknown model", "Неизвестная модель")
	msgMenuInTerminal  = locale.T("The agent opened a menu; answer it in the terminal", "Агент показал меню — ответьте в терминале")
	msgNeedSession     = locale.T("session is required", "Нужна сессия")
	msgUnknownAgent    = locale.T("Unknown agent", "Неизвестный агент")
	msgNeedLine        = locale.T("A command line is required", "Нужна строка команды")
	msgBadSession      = locale.T("Invalid session", "Неверная сессия")
	msgNoSession       = locale.T("Session not found", "Сессия не найдена")
	msgNoMirror        = locale.T("Could not read the agent's screen", "Не получилось прочитать экран агента")
	msgKeyNotAllowed   = locale.T("This key cannot be pressed from the web", "Эту клавишу из веба нажать нельзя")
	msgAgentNotRunning = locale.T("The agent is not running in this conversation", "Агент в этом диалоге не запущен")
	msgKeyFailed       = locale.T("Could not press the key", "Не получилось нажать клавишу")
	msgTerminalOff     = locale.T("The terminal is switched off on this machine: a message goes only to a running agent, and not as a shell command (a line starting with \"!\")",
		"На этой машине терминал выключен: сообщение уходит только работающему агенту и не как команда оболочки (строка, начинающаяся с «!»)")

	msgNoTab = locale.T("Tab not found", "Вкладка не найдена")

	msgNoScreen      = locale.T("Could not read the terminal screen", "Не удалось прочитать экран терминала")
	msgNoTranscript  = locale.T("Could not read the agent's session transcript", "Не удалось прочитать запись сессии агента")
	msgNotAccepted   = locale.T(`Claude did not accept "Send now"; the message is still in the queue`, "Claude не принял «Отправить сейчас» — сообщение осталось в очереди")
	msgConflict      = locale.T("The terminal input box already has text; send or clear it", "В поле ввода терминала уже есть текст — отправьте или сотрите его")
	msgMenu          = locale.T("Claude opened a menu; answer it in the card below", "Claude открыл меню — ответьте на него в карточке ниже")
	msgUnseen        = locale.T("Claude does not show the queue on screen; try again", "Claude не показывает очередь на экране — попробуйте ещё раз")
	msgWriting       = locale.T("The agent is writing a reply; the message will arrive right after it. The reply will not be interrupted", "Агент сейчас пишет ответ — сообщение дойдёт сразу после него. Прерывать ответ я не буду")
	msgInFlight      = locale.T("Already sending", "Уже отправляю")
	msgSendNowFailed = locale.T("Could not send the message", "Не удалось отправить сообщение")

	msgQueueFull  = locale.T("The queue already has too many messages", "В очереди уже слишком много сообщений")
	msgQueueEmpty = locale.T("Empty message", "Пустое сообщение")
)

// steerErrors is why a steer did not go through, by the delivery's outcome.
var steerErrors = map[Outcome]locale.Text{
	OutcomeWait:    locale.T("Could not read the agent's screen; try again", "Не удалось прочитать экран агента — попробуйте ещё раз"),
	OutcomeBlocked: locale.T("The agent's input box already has text or a menu is open; answer there", "В поле ввода агента уже есть текст или открыто меню — ответьте там"),
	OutcomeFailed:  locale.T("The images did not reach the agent; the message is still in the queue", "Картинки не дошли до агента — сообщение осталось в очереди"),
}

func msgTool(tool string) string {
	return locale.Pick(`Step "`+tool+`" is running and cannot be split off without cancelling it; the message will arrive right after it`,
		"Сейчас идёт шаг «"+tool+"» — отделить его без отмены нельзя, сообщение дойдёт сразу после него")
}
