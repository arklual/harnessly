package layoutapi

import "github.com/ly-solutions/harnessly/internal/locale"

// The texts the launch and platform routes answer people with, picked by RUNTIME_LOCALE when the answer is built; the
// Russian ones are the previous runtime's texts, byte for byte.
var (
	msgUnknownAgent    = locale.T("Unknown agent", "Неизвестный агент")
	msgBadWorkspace    = locale.T("Invalid project", "Неверный проект")
	msgEmptyAutomation = locale.T("The automation prompt is empty", "Пустой промпт автоматизации")
	msgEmptyMessage    = locale.T("Empty message", "Пустое сообщение")
	msgNeedTmuxPane    = locale.T("tmuxPane is required", "Нужен tmuxPane")
	msgBadRun          = locale.T("Invalid run", "Неверный прогон")
	msgIncompleteRun   = locale.T("Incomplete run request", "Неполный запрос прогона")
	msgNoRunner        = locale.T("This release has no eval runner", "В этом выпуске нет прогонщика")
	msgNeedJSON        = locale.T("application/json is required", "Нужен application/json")
	msgSideChatBusy    = locale.T("The previous question is still running", "Предыдущий вопрос ещё в работе")
	msgBadLaunchShape  = locale.T("Invalid model or reasoning effort", "Недопустимая модель или глубина рассуждения")

	msgNeedTab        = locale.T("tabId is required", "Нужен tabId")
	msgNoConversation = locale.T("Conversation not found", "Разговор не найден")
	msgNeedIntentIDs  = locale.T("Intent ids are required", "Нужны id намерений")
	msgBadDevice      = locale.T("device: desktop or phone", "device: desktop или phone")
	msgBadResult      = locale.T("Invalid result", "Некорректный ответ")
	msgCommandGone    = locale.T("The command is already done or not found", "Команда уже выполнена или не найдена")
	msgVaultAskGone   = locale.T("The request is already closed or not found", "Просьба уже закрыта или не найдена")
)
