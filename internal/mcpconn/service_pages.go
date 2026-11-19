package mcpconn

import "slices"

// guide is what a service's own page says beyond the catalog's one line: written by hand for the services
// people connect most. Links are official addresses only, each opened and checked; a service without a guide shows
// its Blurb, DocsURL and TokenURL, and the page leaves out what is empty.
type guide struct {
	About, AboutRu string
	Can, CanRu     []string
	Steps, StepsRu []string
	Links          []Link
}

// withGuides puts the hand-written pages into the entries (a copy: the sources stay as they are).
func withGuides(list []CatalogEntry) []CatalogEntry {
	out := slices.Clone(list)
	for i := range out {
		g, ok := catalogGuides[out[i].ID]
		if !ok {
			continue
		}
		out[i].About, out[i].AboutRu = g.About, g.AboutRu
		out[i].Can, out[i].CanRu = g.Can, g.CanRu
		out[i].Steps, out[i].StepsRu = g.Steps, g.StepsRu
		out[i].Links = g.Links
	}
	return out
}

func link(kind, url string) Link { return Link{Kind: kind, URL: url} }

// googleGuide is the page of a local Google server: they all want an OAuth client of the person's own project and a
// refresh token from the OAuth Playground; only the API to switch on, its docs and its words differ.
func googleGuide(about, aboutRu string, can, canRu []string, apiName, apiLibrary, docs string) guide {
	return guide{
		About: about, AboutRu: aboutRu, Can: can, CanRu: canRu,
		Steps: []string{
			"In Google Cloud Console pick or create a project and switch on the " + apiName + " (the Enable button on its library page).",
			"On the OAuth consent screen add your own Google account as a test user, then under Credentials create an OAuth client ID and copy its Client ID and Client secret.",
			"Open the OAuth Playground, in its settings (the gear) tick \"Use your own OAuth credentials\" and paste the pair, pick the scope of the " + apiName + ", sign in and exchange the code for tokens: copy the Refresh token.",
			"Paste the three values below and press Connect. The first start downloads the server and takes a minute or two.",
		},
		StepsRu: []string{
			"В Google Cloud Console выберите или создайте проект и включите " + apiName + " (кнопка «Включить» на странице API).",
			"На экране согласия OAuth добавьте свой Google-аккаунт в тестовые пользователи, затем в разделе «Учётные данные» создайте OAuth client ID и скопируйте Client ID и Client secret.",
			"Откройте OAuth Playground, в его настройках (шестерёнка) отметьте «Use your own OAuth credentials» и вставьте пару, выберите доступ (scope) для " + apiName + ", войдите и обменяйте код на токены: скопируйте Refresh token.",
			"Вставьте три значения ниже и нажмите «Подключить». Первый запуск скачивает сервер и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, docs),
			link(LinkSetup, "https://support.google.com/cloud/answer/6158849"),
			link(LinkToken, "https://developers.google.com/oauthplayground/"),
			link(LinkSetup, apiLibrary),
			link(LinkPricing, "https://workspace.google.com/pricing"),
		},
	}
}

// yandex360Guide is the page of a Yandex 360 server: the same token flow, the product's words differ.
func yandex360Guide(about, aboutRu string, can, canRu []string, docs string) guide {
	return guide{
		About: about, AboutRu: aboutRu, Can: can, CanRu: canRu,
		Steps: []string{
			"Press \"Create token\" below: Yandex asks you to sign in and allow access, then shows a page with the token in its address.",
			"Copy the value after access_token= in that address bar.",
			"Paste it in the field below and press Connect.",
		},
		StepsRu: []string{
			"Нажмите «Создать токен» ниже: Яндекс попросит войти и разрешить доступ, затем откроет страницу, где токен стоит в адресной строке.",
			"Скопируйте значение после access_token= из адресной строки.",
			"Вставьте его в поле ниже и нажмите «Подключить».",
		},
		Links: []Link{
			link(LinkDocs, docs),
			link(LinkSetup, "https://yandex.ru/dev/yandex-360/mcp/"),
			link(LinkHome, "https://360.yandex.ru/"),
			link(LinkPricing, "https://360.yandex.ru/business/"),
		},
	}
}

// catalogGuides is every page, from the files service_pages_<group>.go (one map per group of the catalog, so a batch
// of pages is one file).
var catalogGuides = mergeGuides(coreGuides, guidesSearch, guidesFinance, guidesCloud, guidesSEO, guidesDev,
	guidesResearch, guidesSales, guidesMedia, guidesWork, guidesNew)

// mergeGuides joins the groups; a service written twice is a mistake that stops the start.
func mergeGuides(groups ...map[string]guide) map[string]guide {
	out := map[string]guide{}
	for _, g := range groups {
		for id, v := range g {
			if _, dup := out[id]; dup {
				panic("service page written twice: " + id)
			}
			out[id] = v
		}
	}
	return out
}

var coreGuides = map[string]guide{
	"higgsfield-mcp": {
		About:   "Higgsfield is a studio for AI images and video: Kling, Seedance, Soul and other models in one place. Connected, the agent makes pictures and clips from a brief and brings back the files. Generation spends credits of your Higgsfield plan.",
		AboutRu: "Higgsfield — студия ИИ-картинок и видео: Kling, Seedance, Soul и другие модели в одном месте. Подключённый, агент делает картинки и ролики по описанию и приносит готовые файлы. Генерация тратит кредиты вашего тарифа Higgsfield.",
		Can: []string{
			"Draw images from a text brief, in the style you ask for",
			"Make a video clip from a prompt or from your picture",
			"Choose a model and a length and say what it costs before it starts",
			"Check the status of a job and fetch the finished file",
		},
		CanRu: []string{
			"Нарисует картинку по описанию в нужном стиле",
			"Сделает видеоролик по тексту или по вашей картинке",
			"Выберет модель и длину и скажет цену до запуска",
			"Проверит статус задачи и принесёт готовый файл",
		},
		Steps: []string{
			"Press \"Sign in with Higgsfield\": its page opens in a new window.",
			"Sign in to your Higgsfield account and allow access for the agent.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Higgsfield»: откроется страница сервиса в новом окне.",
			"Войдите в аккаунт Higgsfield и разрешите доступ агенту.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkHome, "https://higgsfield.ai/"),
			link(LinkPricing, "https://higgsfield.ai/pricing"),
			link(LinkDocs, "https://docs.higgsfield.ai/"),
		},
	},
	"higgsfield": {
		About:   "The same Higgsfield studio through its plain API: the agent calls it with your API key. Use it when the MCP server above is not enough or you want a key of your own with its own limits.",
		AboutRu: "Та же студия Higgsfield через обычный API: агент ходит в неё с вашим ключом. Нужен, когда MCP-сервера мало или хочется свой ключ со своими лимитами.",
		Can: []string{
			"List the available image and video models",
			"Estimate the price of a generation before it starts",
			"Start a generation and wait for its result",
			"Show how much of the plan has been used",
		},
		CanRu: []string{
			"Покажет доступные модели картинок и видео",
			"Оценит цену генерации до запуска",
			"Запустит генерацию и дождётся результата",
			"Покажет, сколько тарифа уже потрачено",
		},
		Steps: []string{
			"Open the API keys page of Higgsfield Cloud and create a key.",
			"Copy its ID and its secret: the secret is shown once.",
			"Paste both below and press Connect.",
		},
		StepsRu: []string{
			"Откройте страницу API-ключей Higgsfield Cloud и создайте ключ.",
			"Скопируйте его ID и secret: secret показывают один раз.",
			"Вставьте оба значения ниже и нажмите «Подключить».",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.higgsfield.ai/"),
			link(LinkToken, "https://cloud.higgsfield.ai/api-keys"),
			link(LinkPricing, "https://higgsfield.ai/pricing"),
			link(LinkHome, "https://higgsfield.ai/"),
		},
	},
	"figma": {
		About:   "Figma is where interfaces are drawn. This server (Framelink) reads a file by its link and gives the agent the layout of frames, spacing, colours, text and components, so it can build the screen as it was drawn. It runs on this machine and needs your personal access token.",
		AboutRu: "Figma — место, где рисуют интерфейсы. Этот сервер (Framelink) читает файл по ссылке и отдаёт агенту раскладку фреймов, отступы, цвета, тексты и компоненты, чтобы он собрал экран как нарисовано. Работает на этой машине и просит ваш персональный токен.",
		Can: []string{
			"Read a frame or a whole file by its Figma link",
			"Take sizes, spacing, colours, fonts and texts from the design",
			"Find the components and styles a screen is made of",
			"Export an image of a frame for the agent to look at",
		},
		CanRu: []string{
			"Прочитает фрейм или весь файл по ссылке на Figma",
			"Возьмёт размеры, отступы, цвета, шрифты и тексты из макета",
			"Найдёт компоненты и стили, из которых собран экран",
			"Выгрузит картинку фрейма, чтобы агент на неё посмотрел",
		},
		Steps: []string{
			"Open Figma, then Settings and the Security tab.",
			"In \"Personal access tokens\" press Generate new token, give it a name and read-only access to files.",
			"Copy the token (it starts with figd_; it is shown once) and paste it below.",
			"Press Connect. The first start downloads the server and takes a minute or two.",
		},
		StepsRu: []string{
			"Откройте Figma, затем Settings и вкладку Security.",
			"В разделе «Personal access tokens» нажмите Generate new token, дайте имя и доступ к файлам только на чтение.",
			"Скопируйте токен (начинается с figd_, показывают один раз) и вставьте ниже.",
			"Нажмите «Подключить». Первый запуск скачивает сервер и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/GLips/Figma-Context-MCP"),
			link(LinkSetup, "https://help.figma.com/hc/en-us/articles/8085703771159-Manage-personal-access-tokens"),
			link(LinkToken, "https://www.figma.com/settings"),
			link(LinkHome, "https://www.figma.com/"),
			link(LinkPricing, "https://www.figma.com/pricing/"),
		},
	},
	"google-drive": googleGuide(
		"Google Drive keeps your files and folders. Connected, the agent finds files, reads them and puts new ones in place. It signs in as you through an OAuth client of your own Google project, so nothing passes through a third party.",
		"Google Drive хранит ваши файлы и папки. Подключённый, агент найдёт файл, прочитает его и положит новый на место. Вход идёт от вашего имени через OAuth-клиент вашего проекта Google, посредников нет.",
		[]string{"Search files and folders by name or content", "Read a document, a table or a PDF", "Upload a file or create a folder", "Share a file and say who can open it"},
		[]string{"Найдёт файлы и папки по имени или содержимому", "Прочитает документ, таблицу или PDF", "Загрузит файл или создаст папку", "Откроет доступ к файлу и скажет, кто его видит"},
		"Google Drive API", "https://console.cloud.google.com/apis/library/drive.googleapis.com",
		"https://developers.google.com/drive/api/guides/about-sdk"),
	"google-docs": googleGuide(
		"Google Docs is where text lives. Connected, the agent reads a document, writes a new one and edits the one you point to. It signs in as you through an OAuth client of your own Google project.",
		"Google Docs — это тексты. Подключённый, агент прочитает документ, напишет новый и поправит тот, на который вы укажете. Вход идёт от вашего имени через OAuth-клиент вашего проекта Google.",
		[]string{"Read a document with its headings and tables", "Write a new document from a brief", "Insert, replace and format text in an existing one", "Find a place in a long text and change just it"},
		[]string{"Прочитает документ с заголовками и таблицами", "Напишет новый документ по описанию", "Вставит, заменит и оформит текст в готовом", "Найдёт место в длинном тексте и поправит только его"},
		"Google Docs API", "https://console.cloud.google.com/apis/library/docs.googleapis.com",
		"https://developers.google.com/docs/api/how-tos/overview"),
	"google-calendar": googleGuide(
		"Google Calendar holds your meetings and free time. Connected, the agent reads the schedule, finds a slot and puts a meeting in. It signs in as you through an OAuth client of your own Google project.",
		"Google Календарь хранит встречи и свободное время. Подключённый, агент посмотрит расписание, найдёт окно и поставит встречу. Вход идёт от вашего имени через OAuth-клиент вашего проекта Google.",
		[]string{"Show what is on today, tomorrow or next week", "Find a free slot for a meeting", "Create, move and cancel events with guests", "Add a Meet link and a reminder"},
		[]string{"Покажет, что запланировано на сегодня, завтра или неделю", "Найдёт свободное окно для встречи", "Создаст, перенесёт и отменит событие с гостями", "Добавит ссылку на Meet и напоминание"},
		"Google Calendar API", "https://console.cloud.google.com/apis/library/calendar-json.googleapis.com",
		"https://developers.google.com/calendar/api/guides/overview"),
	"google-sheets-oauth": googleGuide(
		"Google Sheets is for tables and small databases. Connected, the agent reads ranges, fills cells and builds a table from your data. It signs in as you through an OAuth client of your own Google project.",
		"Google Таблицы — для таблиц и небольших баз. Подключённый, агент прочитает диапазон, заполнит ячейки и соберёт таблицу из ваших данных. Вход идёт от вашего имени через OAuth-клиент вашего проекта Google.",
		[]string{"Read a range or a whole sheet", "Write and update cells, rows and formulas", "Create a spreadsheet from data you give", "Add a sheet and tidy up the columns"},
		[]string{"Прочитает диапазон или весь лист", "Запишет и обновит ячейки, строки и формулы", "Создаст таблицу из ваших данных", "Добавит лист и наведёт порядок в столбцах"},
		"Google Sheets API", "https://console.cloud.google.com/apis/library/sheets.googleapis.com",
		"https://developers.google.com/sheets/api/guides/concepts"),
	"google-slides": googleGuide(
		"Google Slides is for presentations. Connected, the agent reads a deck and builds or edits slides. It signs in as you through an OAuth client of your own Google project.",
		"Google Презентации — для слайдов. Подключённый, агент прочитает презентацию, соберёт или поправит слайды. Вход идёт от вашего имени через OAuth-клиент вашего проекта Google.",
		[]string{"Read the slides of a deck with their texts", "Create a presentation from an outline", "Add, reorder and delete slides", "Replace texts and images on a slide"},
		[]string{"Прочитает слайды презентации вместе с текстами", "Создаст презентацию по плану", "Добавит, переставит и удалит слайды", "Заменит тексты и картинки на слайде"},
		"Google Slides API", "https://console.cloud.google.com/apis/library/slides.googleapis.com",
		"https://developers.google.com/slides/api/guides/overview"),
	"google-analytics": {
		About:   "Google Analytics 4 counts visits to your site and app. This is Google's own server: the agent builds reports, compares periods and looks at the live audience. It reads through a service account that you give access to your property.",
		AboutRu: "Google Analytics 4 считает визиты на сайт и в приложение. Это официальный сервер Google: агент соберёт отчёт, сравнит периоды и посмотрит аудиторию прямо сейчас. Читает через сервисный аккаунт, которому вы даёте доступ к ресурсу.",
		Can: []string{
			"List the accounts and properties you can see",
			"Build a report by dimensions and metrics for any period",
			"Show who is on the site right now",
			"Compare two periods and point out what changed",
		},
		CanRu: []string{
			"Покажет аккаунты и ресурсы, которые вам доступны",
			"Соберёт отчёт по измерениям и метрикам за любой период",
			"Покажет, кто на сайте прямо сейчас",
			"Сравнит два периода и укажет, что изменилось",
		},
		Steps: []string{
			"In Google Cloud open IAM, then Service accounts, and create an account (or take one you have).",
			"In its Keys tab add a key of type JSON: a file downloads.",
			"In Google Analytics open Admin, Property access management, and add the account's email with the Viewer role.",
			"Also switch on the Google Analytics Data API and Admin API in the project, paste the file's contents below and press Connect.",
		},
		StepsRu: []string{
			"В Google Cloud откройте IAM, затем «Сервисные аккаунты» и создайте аккаунт (или возьмите свой).",
			"На вкладке «Ключи» добавьте ключ типа JSON: скачается файл.",
			"В Google Analytics откройте «Администратор», «Управление доступом к ресурсу» и добавьте email этого аккаунта с ролью «Читатель».",
			"Включите в проекте Google Analytics Data API и Admin API, вставьте содержимое файла ниже и нажмите «Подключить».",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/googleanalytics/google-analytics-mcp"),
			link(LinkToken, "https://console.cloud.google.com/iam-admin/serviceaccounts"),
			link(LinkSetup, "https://developers.google.com/analytics/devguides/reporting/data/v1"),
			link(LinkHome, "https://analytics.google.com/"),
		},
	},
	"yandex-mail": yandex360Guide(
		"Yandex Mail inside Yandex 360. Connected, the agent reads and searches your mail, sorts it into folders and labels and drafts replies for you to check. Needs a Yandex 360 plan.",
		"Яндекс Почта в составе Яндекс 360. Подключённый, агент прочитает и найдёт письма, разложит их по папкам и меткам и подготовит черновики ответов на проверку. Нужна подписка Яндекс 360.",
		[]string{"Find a letter by sender, topic or date", "Read a thread and tell what is asked of you", "Sort mail into folders and labels", "Write a draft reply that you send yourself"},
		[]string{"Найдёт письмо по отправителю, теме или дате", "Прочитает цепочку и скажет, чего от вас хотят", "Разложит почту по папкам и меткам", "Напишет черновик ответа, который отправите вы"},
		"https://yandex.ru/dev/yandex-360/mcp/ru/mail"),
	"yandex-calendar": yandex360Guide(
		"Yandex Calendar inside Yandex 360. Connected, the agent reads the schedule and free time and creates or moves meetings. Needs a Yandex 360 plan.",
		"Яндекс Календарь в составе Яндекс 360. Подключённый, агент посмотрит расписание и свободное время, создаст и перенесёт встречи. Нужна подписка Яндекс 360.",
		[]string{"Show the schedule for a day or a week", "Find a time when everyone is free", "Create a meeting with guests", "Move or cancel a meeting"},
		[]string{"Покажет расписание на день или неделю", "Найдёт время, когда свободны все", "Создаст встречу с гостями", "Перенесёт или отменит встречу"},
		"https://yandex.ru/dev/yandex-360/mcp/ru/calendar"),
	"yandex-disk-mcp": yandex360Guide(
		"Yandex Disk inside Yandex 360. Connected, the agent finds files and folders, reads them and shares a link.",
		"Яндекс Диск в составе Яндекс 360. Подключённый, агент найдёт файлы и папки, прочитает их и откроет доступ по ссылке.",
		[]string{"Search files and folders by name", "Read a file or list a folder", "Upload a file to the place you name", "Publish a link to a file"},
		[]string{"Найдёт файлы и папки по имени", "Прочитает файл или покажет содержимое папки", "Загрузит файл в нужное место", "Опубликует ссылку на файл"},
		"https://yandex.ru/dev/yandex-360/mcp/ru/disk.md"),
	"yandex-telemost": yandex360Guide(
		"Yandex Telemost inside Yandex 360. Connected, the agent creates a video meeting and gives you the link to send.",
		"Яндекс Телемост в составе Яндекс 360. Подключённый, агент создаст видеовстречу и даст ссылку, которую можно отправить участникам.",
		[]string{"Create a meeting and return its link", "Show meetings you have created", "Change a meeting's settings", "Invite participants by email"},
		[]string{"Создаст встречу и вернёт ссылку", "Покажет созданные вами встречи", "Изменит настройки встречи", "Пригласит участников по почте"},
		"https://yandex.ru/dev/yandex-360/mcp/ru/telemost.md"),
	"telegram-bot": {
		About:   "A Telegram bot is an account that a program drives. Connected through its token, the agent reads what people write to the bot and writes back or posts to a channel where the bot is an admin. The bot sees only chats it was added to or written to.",
		AboutRu: "Telegram-бот — это аккаунт, которым управляет программа. Подключённый по токену, агент читает, что пишут боту, отвечает и публикует в канал, где бот администратор. Бот видит только чаты, куда его добавили или где ему написали.",
		Can: []string{
			"Read the messages that came to the bot",
			"Answer a person or post to a group or a channel",
			"Send a file, a picture or a document",
			"Look up a chat: its title and members count",
		},
		CanRu: []string{
			"Прочитает сообщения, пришедшие боту",
			"Ответит человеку или опубликует в группе или канале",
			"Отправит файл, картинку или документ",
			"Посмотрит чат: название и число участников",
		},
		Steps: []string{
			"Open @BotFather in Telegram and send /newbot (or /token for a bot you have).",
			"Pick a name and a username that ends in bot: BotFather answers with the token.",
			"Paste the token below and press Connect.",
			"Write something to the bot (or add it to a group or channel) so that it has a chat to work with.",
		},
		StepsRu: []string{
			"Откройте @BotFather в Telegram и отправьте /newbot (или /token для готового бота).",
			"Придумайте имя и username, оканчивающийся на bot: BotFather ответит токеном.",
			"Вставьте токен ниже и нажмите «Подключить».",
			"Напишите боту (или добавьте его в группу или канал), чтобы у него появился чат для работы.",
		},
		Links: []Link{
			link(LinkDocs, "https://core.telegram.org/bots/api"),
			link(LinkSetup, "https://core.telegram.org/bots/tutorial"),
			link(LinkToken, "https://t.me/BotFather"),
			link(LinkHome, "https://telegram.org/"),
		},
	},
	"slack": {
		About:   "Slack is the team chat. This server runs on this machine and signs in with your Slack token: the agent reads channels and threads, searches and writes where you could write. Nothing is sent unless you ask.",
		AboutRu: "Slack — рабочий чат команды. Сервер работает на этой машине и входит по вашему токену Slack: агент читает каналы и треды, ищет и пишет там, где писать можете вы. Ничего не отправляется без вашей просьбы.",
		Can: []string{
			"Read the history of a channel or a thread",
			"Search messages and people across the workspace",
			"Write a message or a reply in a thread",
			"Summarise what was missed in a channel since yesterday",
		},
		CanRu: []string{
			"Прочитает историю канала или тред",
			"Найдёт сообщения и людей по всему рабочему пространству",
			"Напишет сообщение или ответ в тред",
			"Перескажет, что было в канале со вчерашнего дня",
		},
		Steps: []string{
			"Open the Slack API apps page and create an app from scratch in your workspace.",
			"In OAuth & Permissions add user scopes: channels:history, groups:history, im:history, mpim:history, channels:read, groups:read, users:read, search:read, chat:write.",
			"Install the app to the workspace and copy the User OAuth Token (it starts with xoxp-).",
			"Paste it below and press Connect. The first start downloads the server and takes a minute or two.",
		},
		StepsRu: []string{
			"Откройте страницу приложений Slack API и создайте приложение «с нуля» в вашем рабочем пространстве.",
			"В OAuth & Permissions добавьте user scopes: channels:history, groups:history, im:history, mpim:history, channels:read, groups:read, users:read, search:read, chat:write.",
			"Установите приложение в пространство и скопируйте User OAuth Token (начинается с xoxp-).",
			"Вставьте его ниже и нажмите «Подключить». Первый запуск скачивает сервер и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/korotovsky/slack-mcp-server"),
			link(LinkSetup, "https://github.com/korotovsky/slack-mcp-server/blob/master/docs/01-authentication-setup.md"),
			link(LinkToken, "https://api.slack.com/apps"),
			link(LinkHome, "https://slack.com/"),
			link(LinkPricing, "https://slack.com/pricing"),
		},
	},
	"discord": {
		About:   "Discord is a chat for communities. This server runs on this machine with the token of a bot you create: the agent reads channels and forums of the servers where the bot is invited and writes there.",
		AboutRu: "Discord — чат для сообществ. Сервер работает на этой машине с токеном бота, которого вы создаёте: агент читает каналы и форумы серверов, куда приглашён бот, и пишет в них.",
		Can: []string{
			"List the servers and channels the bot can see",
			"Read recent messages of a channel or a forum post",
			"Send a message, a reply or a reaction",
			"Create a forum post or a webhook",
		},
		CanRu: []string{
			"Покажет серверы и каналы, которые видит бот",
			"Прочитает свежие сообщения канала или пост форума",
			"Отправит сообщение, ответ или реакцию",
			"Создаст пост на форуме или вебхук",
		},
		Steps: []string{
			"Open the Discord Developer Portal and create an application.",
			"In its Bot tab press Reset Token and copy the token; switch on Message Content Intent.",
			"In OAuth2, URL Generator pick the bot scope with the rights you need and invite the bot to your server by the link it makes.",
			"Paste the token below and press Connect. The first start downloads the server and takes a minute or two.",
		},
		StepsRu: []string{
			"Откройте Discord Developer Portal и создайте приложение.",
			"На вкладке Bot нажмите Reset Token и скопируйте токен; включите Message Content Intent.",
			"В OAuth2, URL Generator выберите scope bot с нужными правами и пригласите бота на свой сервер по полученной ссылке.",
			"Вставьте токен ниже и нажмите «Подключить». Первый запуск скачивает сервер и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/barryyip0625/mcp-discord"),
			link(LinkSetup, "https://discord.com/developers/docs/intro"),
			link(LinkToken, "https://discord.com/developers/applications"),
			link(LinkHome, "https://discord.com/"),
		},
	},
	"notion": {
		About:   "Notion keeps notes, wikis and databases of a team. Connected through Notion's own server, the agent searches the workspace, reads pages and writes to them with the rights of your account.",
		AboutRu: "Notion хранит заметки, вики и базы команды. Подключённый через официальный сервер Notion, агент ищет по рабочему пространству, читает страницы и пишет в них с правами вашего аккаунта.",
		Can: []string{
			"Search pages and databases of the workspace",
			"Read a page with its blocks and comments",
			"Create a page or a database row from your notes",
			"Update properties and text of an existing page",
		},
		CanRu: []string{
			"Найдёт страницы и базы рабочего пространства",
			"Прочитает страницу вместе с блоками и комментариями",
			"Создаст страницу или строку базы из ваших заметок",
			"Обновит свойства и текст готовой страницы",
		},
		Steps: []string{
			"Press \"Sign in with Notion\": its page opens in a new window.",
			"Sign in to Notion and choose the workspace: the agent sees only what you can open there.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Notion»: откроется страница сервиса в новом окне.",
			"Войдите в Notion и выберите рабочее пространство: агент увидит только то, что доступно вам.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.notion.com/docs/mcp"),
			link(LinkHome, "https://www.notion.com/"),
			link(LinkPricing, "https://www.notion.com/pricing"),
		},
	},
	"github": {
		About:   "GitHub hosts code, issues and pull requests. Connected with a personal access token, the agent reads repositories, works with issues and reviews pull requests within the rights you give the token.",
		AboutRu: "GitHub хранит код, задачи и пул-реквесты. Подключённый по персональному токену, агент читает репозитории, работает с задачами и смотрит пул-реквесты в рамках прав, которые вы дали токену.",
		Can: []string{
			"Read files, branches and history of a repository",
			"Find, open and comment on issues",
			"Read a pull request with its diff and checks",
			"Create a branch, a pull request or a review comment",
		},
		CanRu: []string{
			"Прочитает файлы, ветки и историю репозитория",
			"Найдёт, заведёт и прокомментирует задачу",
			"Прочитает пул-реквест вместе с диффом и проверками",
			"Создаст ветку, пул-реквест или комментарий ревью",
		},
		Steps: []string{
			"Press \"Create token\" below: GitHub opens the form for a fine-grained token.",
			"Choose the repositories and give read access to contents, issues and pull requests (write only if the agent should change something).",
			"Press Generate token and copy it: GitHub shows it once.",
			"Paste it below and press Connect.",
		},
		StepsRu: []string{
			"Нажмите «Создать токен» ниже: GitHub откроет форму токена с точной настройкой прав.",
			"Выберите репозитории и дайте чтение для contents, issues и pull requests (запись — только если агент должен что-то менять).",
			"Нажмите Generate token и скопируйте его: GitHub показывает токен один раз.",
			"Вставьте его ниже и нажмите «Подключить».",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/github/github-mcp-server"),
			link(LinkSetup, "https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens"),
			link(LinkToken, "https://github.com/settings/personal-access-tokens/new"),
			link(LinkHome, "https://github.com/"),
			link(LinkPricing, "https://github.com/pricing"),
		},
	},
	"gitlab": {
		About:   "GitLab hosts code, issues, merge requests and pipelines. Connected through GitLab's own server on gitlab.com, the agent reads projects and works with them with the rights of your account.",
		AboutRu: "GitLab хранит код, задачи, merge request'ы и пайплайны. Подключённый через официальный сервер GitLab на gitlab.com, агент читает проекты и работает с ними с правами вашего аккаунта.",
		Can: []string{
			"Find projects, issues and merge requests",
			"Read a merge request with its changes and discussion",
			"Look at the pipelines and the logs of a failed job",
			"Create an issue or a note in a merge request",
		},
		CanRu: []string{
			"Найдёт проекты, задачи и merge request'ы",
			"Прочитает merge request вместе с изменениями и обсуждением",
			"Посмотрит пайплайны и логи упавшей задачи",
			"Заведёт задачу или оставит комментарий в merge request",
		},
		Steps: []string{
			"Press \"Sign in with GitLab\": its page opens in a new window.",
			"Sign in to GitLab and allow access: the agent works with your rights in your projects.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через GitLab»: откроется страница сервиса в новом окне.",
			"Войдите в GitLab и разрешите доступ: агент работает с вашими правами в ваших проектах.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.gitlab.com/user/gitlab_duo/model_context_protocol/mcp_server/"),
			link(LinkHome, "https://gitlab.com/"),
			link(LinkPricing, "https://about.gitlab.com/pricing/"),
		},
	},
	"atlassian": {
		About:   "Atlassian's server covers Jira (tasks) and Confluence (wiki) of your cloud site. Connected, the agent finds and writes issues and pages with the rights of your account.",
		AboutRu: "Сервер Atlassian закрывает Jira (задачи) и Confluence (вики) вашего облачного сайта. Подключённый, агент находит и пишет задачи и страницы с правами вашего аккаунта.",
		Can: []string{
			"Search Jira issues with a text or a JQL query",
			"Create an issue and change its status or assignee",
			"Read and write Confluence pages",
			"Summarise a sprint or an epic from its issues",
		},
		CanRu: []string{
			"Найдёт задачи Jira по тексту или JQL-запросу",
			"Заведёт задачу и сменит ей статус или исполнителя",
			"Прочитает и напишет страницы Confluence",
			"Подведёт итог спринта или эпика по его задачам",
		},
		Steps: []string{
			"Press \"Sign in with Atlassian\": its page opens in a new window.",
			"Sign in to Atlassian, choose the site and the products (Jira, Confluence) the agent may reach, and allow access.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Atlassian»: откроется страница сервиса в новом окне.",
			"Войдите в Atlassian, выберите сайт и продукты (Jira, Confluence), к которым агенту можно обращаться, и разрешите доступ.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://support.atlassian.com/atlassian-rovo-mcp-server/docs/getting-started-with-the-atlassian-remote-mcp-server/"),
			link(LinkHome, "https://www.atlassian.com/platform/remote-mcp-server"),
			link(LinkPricing, "https://www.atlassian.com/software/jira/pricing"),
		},
	},
	"linear": {
		About:   "Linear is a tracker for product teams. Connected through Linear's own server, the agent finds issues, writes new ones and moves them along, with the rights of your account.",
		AboutRu: "Linear — трекер для продуктовых команд. Подключённый через официальный сервер Linear, агент находит задачи, заводит новые и двигает их по статусам с правами вашего аккаунта.",
		Can: []string{
			"Find issues by text, assignee, label or status",
			"Create an issue with a description and a project",
			"Change status, priority and assignee",
			"Read projects and cycles and tell where the work stands",
		},
		CanRu: []string{
			"Найдёт задачи по тексту, исполнителю, метке или статусу",
			"Заведёт задачу с описанием и проектом",
			"Поменяет статус, приоритет и исполнителя",
			"Прочитает проекты и циклы и скажет, как идёт работа",
		},
		Steps: []string{
			"Press \"Sign in with Linear\": its page opens in a new window.",
			"Sign in to Linear, choose the workspace and allow access.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Linear»: откроется страница сервиса в новом окне.",
			"Войдите в Linear, выберите рабочее пространство и разрешите доступ.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://linear.app/docs/mcp"),
			link(LinkHome, "https://linear.app/"),
			link(LinkPricing, "https://linear.app/pricing"),
		},
	},
	"todoist": {
		About:   "Todoist is a to-do list. Connected through Todoist's own server, the agent reads your lists, adds tasks with dates and ticks the done ones.",
		AboutRu: "Todoist — список дел. Подключённый через официальный сервер Todoist, агент читает ваши списки, добавляет задачи со сроками и отмечает сделанное.",
		Can: []string{
			"Show tasks for today, the week or a project",
			"Add a task with a due date, a priority and a label",
			"Move, rename and complete tasks",
			"Break a big task into subtasks",
		},
		CanRu: []string{
			"Покажет задачи на сегодня, неделю или проект",
			"Добавит задачу со сроком, приоритетом и меткой",
			"Перенесёт, переименует и завершит задачи",
			"Разобьёт большую задачу на подзадачи",
		},
		Steps: []string{
			"Press \"Sign in with Todoist\": its page opens in a new window.",
			"Sign in to Todoist and allow access to your tasks and projects.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Todoist»: откроется страница сервиса в новом окне.",
			"Войдите в Todoist и разрешите доступ к задачам и проектам.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/Doist/todoist-ai"),
			link(LinkHome, "https://todoist.com/"),
			link(LinkPricing, "https://todoist.com/pricing"),
		},
	},
	"airtable": {
		About:   "Airtable is a spreadsheet that works like a database. Connected through Airtable's own server, the agent reads bases and tables and adds and updates records.",
		AboutRu: "Airtable — таблицы, которые работают как база данных. Подключённый через официальный сервер Airtable, агент читает базы и таблицы, добавляет и обновляет записи.",
		Can: []string{
			"List your bases, tables and fields",
			"Search and filter records",
			"Add records from a list or a file you give",
			"Update fields of existing records",
		},
		CanRu: []string{
			"Покажет ваши базы, таблицы и поля",
			"Найдёт и отфильтрует записи",
			"Добавит записи из списка или файла, который вы дадите",
			"Обновит поля существующих записей",
		},
		Steps: []string{
			"Press \"Sign in with Airtable\": its page opens in a new window.",
			"Sign in to Airtable and choose the bases the agent may read and edit.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Airtable»: откроется страница сервиса в новом окне.",
			"Войдите в Airtable и выберите базы, которые агент может читать и править.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://airtable.com/developers/agents/mcp/getting-started"),
			link(LinkHome, "https://airtable.com/"),
			link(LinkPricing, "https://airtable.com/pricing"),
		},
	},
	"sentry": {
		About:   "Sentry collects errors of your apps. Connected through Sentry's own server, the agent finds an issue, reads its stack trace and the release it came with, and proposes a fix.",
		AboutRu: "Sentry собирает ошибки ваших приложений. Подключённый через официальный сервер Sentry, агент находит ошибку, читает стектрейс и релиз, в котором она появилась, и предлагает исправление.",
		Can: []string{
			"Find the issues that appear most often or just started",
			"Read the stack trace, tags and breadcrumbs of an event",
			"Tell which release brought the error",
			"List projects and teams of the organization",
		},
		CanRu: []string{
			"Найдёт самые частые и только что появившиеся ошибки",
			"Прочитает стектрейс, теги и хлебные крошки события",
			"Скажет, какой релиз принёс ошибку",
			"Покажет проекты и команды организации",
		},
		Steps: []string{
			"Press \"Sign in with Sentry\": its page opens in a new window.",
			"Sign in to Sentry, choose the organization and allow access.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Sentry»: откроется страница сервиса в новом окне.",
			"Войдите в Sentry, выберите организацию и разрешите доступ.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.sentry.io/product/sentry-mcp/"),
			link(LinkHome, "https://sentry.io/"),
			link(LinkPricing, "https://sentry.io/pricing/"),
		},
	},
	"stripe": {
		About:   "Stripe takes payments online. Connected through Stripe's own server, the agent looks at payments, customers and subscriptions and searches the Stripe docs. Use a restricted key or a test account if you only want to look.",
		AboutRu: "Stripe принимает платежи онлайн. Подключённый через официальный сервер Stripe, агент смотрит платежи, клиентов и подписки и ищет ответы в документации Stripe. Если нужно только смотреть, используйте ограниченный ключ или тестовый аккаунт.",
		Can: []string{
			"Find a payment, a customer or an invoice",
			"Show subscriptions and their statuses",
			"Create a product, a price or a payment link",
			"Answer a question about the Stripe API from its docs",
		},
		CanRu: []string{
			"Найдёт платёж, клиента или счёт",
			"Покажет подписки и их статусы",
			"Создаст продукт, цену или ссылку на оплату",
			"Ответит на вопрос по API Stripe из документации",
		},
		Steps: []string{
			"Press \"Sign in with Stripe\": its page opens in a new window.",
			"Sign in to Stripe and choose the account: try a sandbox first, since the agent can create real objects.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Stripe»: откроется страница сервиса в новом окне.",
			"Войдите в Stripe и выберите аккаунт: сначала попробуйте песочницу, ведь агент может создавать настоящие объекты.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.stripe.com/mcp"),
			link(LinkHome, "https://stripe.com/"),
			link(LinkPricing, "https://stripe.com/pricing"),
		},
	},
	"vercel": {
		About:   "Vercel hosts and deploys web apps. Connected through Vercel's own server, the agent sees projects and deployments and reads build and runtime logs to find why a deploy failed.",
		AboutRu: "Vercel хостит и выкатывает веб-приложения. Подключённый через официальный сервер Vercel, агент видит проекты и деплои и читает логи сборки и работы, чтобы найти причину упавшего деплоя.",
		Can: []string{
			"List projects and their latest deployments",
			"Read the build log of a failed deployment",
			"Read the runtime logs of a running one",
			"Search the Vercel documentation",
		},
		CanRu: []string{
			"Покажет проекты и их последние деплои",
			"Прочитает лог сборки упавшего деплоя",
			"Прочитает логи работающего деплоя",
			"Найдёт ответ в документации Vercel",
		},
		Steps: []string{
			"Press \"Sign in with Vercel\": its page opens in a new window.",
			"Sign in to Vercel, choose the team and allow access.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Vercel»: откроется страница сервиса в новом окне.",
			"Войдите в Vercel, выберите команду и разрешите доступ.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://vercel.com/docs/mcp/vercel-mcp"),
			link(LinkHome, "https://vercel.com/"),
			link(LinkPricing, "https://vercel.com/pricing"),
		},
	},
	"supabase": {
		About:   "Supabase gives an app a Postgres database, sign-in and storage. Connected through Supabase's own server, the agent looks at your projects, queries the database and changes tables. Pick a dev project first: the agent can change data.",
		AboutRu: "Supabase даёт приложению базу Postgres, вход и хранилище. Подключённый через официальный сервер Supabase, агент смотрит ваши проекты, делает запросы к базе и меняет таблицы. Начните с учебного проекта: агент может менять данные.",
		Can: []string{
			"List projects and the tables of a database",
			"Run a SQL query and explain the result",
			"Create a table or a migration",
			"Read the logs and deploy an edge function",
		},
		CanRu: []string{
			"Покажет проекты и таблицы базы",
			"Выполнит SQL-запрос и объяснит результат",
			"Создаст таблицу или миграцию",
			"Прочитает логи и выложит edge-функцию",
		},
		Steps: []string{
			"Press \"Sign in with Supabase\": its page opens in a new window.",
			"Sign in to Supabase, choose the organization and allow access.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Supabase»: откроется страница сервиса в новом окне.",
			"Войдите в Supabase, выберите организацию и разрешите доступ.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://supabase.com/docs/guides/getting-started/mcp"),
			link(LinkHome, "https://supabase.com/"),
			link(LinkPricing, "https://supabase.com/pricing"),
		},
	},
	"cloudflare": {
		About:   "Cloudflare runs sites, DNS and edge code. Connected through Cloudflare's own server, the agent works with your account: Workers, DNS records, storage and the rest of its API.",
		AboutRu: "Cloudflare обслуживает сайты, DNS и код на границе сети. Подключённый через официальный сервер Cloudflare, агент работает с вашим аккаунтом: Workers, записи DNS, хранилища и остальной API.",
		Can: []string{
			"List zones and DNS records and change them",
			"Look at Workers and their settings",
			"Read the state of R2, KV and D1 storage",
			"Search the Cloudflare documentation",
		},
		CanRu: []string{
			"Покажет зоны и записи DNS и поправит их",
			"Посмотрит Workers и их настройки",
			"Прочитает состояние хранилищ R2, KV и D1",
			"Найдёт ответ в документации Cloudflare",
		},
		Steps: []string{
			"Press \"Sign in with Cloudflare\": its page opens in a new window.",
			"Sign in to Cloudflare, choose the accounts the agent may use and allow access.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Cloudflare»: откроется страница сервиса в новом окне.",
			"Войдите в Cloudflare, выберите аккаунты, которыми агенту можно пользоваться, и разрешите доступ.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.cloudflare.com/agents/model-context-protocol/mcp-servers-for-cloudflare/"),
			link(LinkHome, "https://www.cloudflare.com/"),
			link(LinkPricing, "https://www.cloudflare.com/plans/"),
		},
	},
	"context7": {
		About:   "Context7 gives the agent fresh documentation of libraries and frameworks, so it writes code against today's API rather than what it remembers. It works without an account; a key from its dashboard raises the limits.",
		AboutRu: "Context7 подаёт агенту свежую документацию библиотек и фреймворков, чтобы он писал код под сегодняшний API, а не под то, что помнит. Работает без аккаунта; ключ из личного кабинета поднимает лимиты.",
		Can: []string{
			"Find the library you mean by its name",
			"Pull the current docs and examples for a topic",
			"Answer \"how do I do X in version Y\" from the docs",
			"Keep the answer short enough for the agent's context",
		},
		CanRu: []string{
			"Найдёт нужную библиотеку по названию",
			"Подтянет актуальные доки и примеры по теме",
			"Ответит «как сделать X в версии Y» по документации",
			"Уложит ответ в размер, удобный для контекста агента",
		},
		Steps: []string{
			"Press Connect: the server is public and needs no sign-in or key.",
			"Ask the agent for the docs of a library by name, for example \"React Query\"; it finds the right version itself.",
		},
		StepsRu: []string{
			"Нажмите «Подключить»: сервер общедоступный, входить и вводить ключ не нужно.",
			"Попросите у агента документацию библиотеки по названию, например «React Query»: нужную версию он найдёт сам.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/upstash/context7"),
			link(LinkToken, "https://context7.com/dashboard"),
			link(LinkHome, "https://context7.com/"),
		},
	},
	"huggingface": {
		About:   "Hugging Face is the hub of open models, datasets and demo Spaces. Connected through its own server, the agent searches the hub and reads what it finds.",
		AboutRu: "Hugging Face — каталог открытых моделей, датасетов и демо-приложений Spaces. Подключённый через официальный сервер, агент ищет по каталогу и читает найденное.",
		Can: []string{
			"Search models by task, size or licence",
			"Read a model's card and files",
			"Find datasets and look at their structure",
			"Find a Space and see how it works",
		},
		CanRu: []string{
			"Найдёт модели по задаче, размеру или лицензии",
			"Прочитает карточку и файлы модели",
			"Найдёт датасеты и покажет их структуру",
			"Найдёт Space и покажет, как он устроен",
		},
		Steps: []string{
			"Press \"Sign in with Hugging Face\": its page opens in a new window.",
			"Sign in to Hugging Face and allow access to the models, datasets and Spaces you can see.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Hugging Face»: откроется страница сервиса в новом окне.",
			"Войдите в Hugging Face и разрешите доступ к моделям, датасетам и Spaces, которые вам видны.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://huggingface.co/docs/hub/agents-mcp"),
			link(LinkSetup, "https://huggingface.co/settings/mcp"),
			link(LinkHome, "https://huggingface.co/"),
			link(LinkPricing, "https://huggingface.co/pricing"),
		},
	},
	"posthog": {
		About:   "PostHog is product analytics: events, funnels, flags and experiments. Connected through PostHog's own server, the agent counts, builds an insight and looks at errors.",
		AboutRu: "PostHog — продуктовая аналитика: события, воронки, флаги и эксперименты. Подключённый через официальный сервер PostHog, агент посчитает, соберёт инсайт и посмотрит ошибки.",
		Can: []string{
			"Answer a product question with a query over events",
			"Build a funnel or a trend and say what it shows",
			"List feature flags and experiments and their state",
			"Find the errors users hit most",
		},
		CanRu: []string{
			"Ответит на продуктовый вопрос запросом по событиям",
			"Соберёт воронку или тренд и скажет, что они показывают",
			"Покажет флаги и эксперименты и их состояние",
			"Найдёт ошибки, на которые чаще всего попадают пользователи",
		},
		Steps: []string{
			"Press \"Sign in with PostHog\": its page opens in a new window.",
			"Sign in to PostHog and allow access to your project's data.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через PostHog»: откроется страница сервиса в новом окне.",
			"Войдите в PostHog и разрешите доступ к данным вашего проекта.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://posthog.com/docs/model-context-protocol"),
			link(LinkHome, "https://posthog.com/"),
			link(LinkPricing, "https://posthog.com/pricing"),
		},
	},
	"calendly": {
		About:   "Calendly lets people book time with you. Connected through Calendly's own server, the agent sees booked meetings and event types, gives a booking link and cancels a meeting.",
		AboutRu: "Calendly позволяет людям записываться к вам на время. Подключённый через официальный сервер Calendly, агент видит назначенные встречи и типы событий, даёт ссылку на запись и отменяет встречу.",
		Can: []string{
			"Show upcoming and past meetings",
			"List your event types and their booking links",
			"Check free slots of an event type",
			"Cancel a meeting with a reason",
		},
		CanRu: []string{
			"Покажет предстоящие и прошедшие встречи",
			"Покажет типы событий и их ссылки на запись",
			"Проверит свободные слоты типа события",
			"Отменит встречу с указанием причины",
		},
		Steps: []string{
			"Press \"Sign in with Calendly\": its page opens in a new window.",
			"Sign in to Calendly and allow access to your scheduling data.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Calendly»: откроется страница сервиса в новом окне.",
			"Войдите в Calendly и разрешите доступ к данным о расписании.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://developer.calendly.com/calendly-mcp-server"),
			link(LinkHome, "https://calendly.com/"),
			link(LinkPricing, "https://calendly.com/pricing"),
		},
	},
	"microsoft-365": {
		About:   "Microsoft 365 means Outlook mail and calendar, OneDrive, Word, Excel, PowerPoint, Teams and Planner. The server runs on this machine and works through Microsoft Graph; you sign in once by a code and the sign-in stays here.",
		AboutRu: "Microsoft 365 — это почта и календарь Outlook, OneDrive, Word, Excel, PowerPoint, Teams и Planner. Сервер работает на этой машине через Microsoft Graph; вы входите один раз по коду, и вход остаётся здесь.",
		Can: []string{
			"Read and search Outlook mail and write drafts",
			"Show the calendar and create meetings",
			"Find and read files of OneDrive, Word and Excel",
			"Read Teams chats and Planner tasks",
		},
		CanRu: []string{
			"Прочитает и найдёт письма Outlook, напишет черновики",
			"Покажет календарь и создаст встречи",
			"Найдёт и прочитает файлы OneDrive, Word и Excel",
			"Прочитает чаты Teams и задачи Planner",
		},
		Steps: []string{
			"Press Connect: no keys are needed, the shared app is used. For a work account of an organization you may add its Tenant ID.",
			"The first start downloads the server and takes a minute or two.",
			"Ask the agent to sign in to Microsoft 365: it gives you an address and a code. Open the address, enter the code and allow access.",
		},
		StepsRu: []string{
			"Нажмите «Подключить»: ключи не нужны, работает общее приложение. Для рабочего аккаунта организации можно указать её Tenant ID.",
			"Первый запуск скачивает сервер и занимает минуту-две.",
			"Попросите агента войти в Microsoft 365: он даст адрес и код. Откройте адрес, введите код и разрешите доступ.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/Softeria/ms-365-mcp-server"),
			link(LinkSetup, "https://learn.microsoft.com/graph/overview"),
			link(LinkHome, "https://www.microsoft.com/microsoft-365"),
		},
	},
	"evernote": {
		About:   "Evernote keeps notes and notebooks. Connected through its own server, the agent finds a note and creates a new one.",
		AboutRu: "Evernote хранит заметки и блокноты. Подключённый через официальный сервер, агент найдёт заметку и создаст новую.",
		Can: []string{
			"Search notes by text and tag",
			"Read a note",
			"Create a note in the notebook you name",
			"List your notebooks",
		},
		CanRu: []string{
			"Найдёт заметки по тексту и тегу",
			"Прочитает заметку",
			"Создаст заметку в нужном блокноте",
			"Покажет ваши блокноты",
		},
		Steps: []string{
			"Press \"Sign in with Evernote\": its page opens in a new window.",
			"Sign in to Evernote and allow access to your notes.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Evernote»: откроется страница сервиса в новом окне.",
			"Войдите в Evernote и разрешите доступ к вашим заметкам.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkHome, "https://evernote.com/"),
			link(LinkPricing, "https://evernote.com/compare-plans"),
		},
	},
	"joplin": {
		About:   "Joplin is an open-source notes app. The server runs on this machine and talks to the Joplin app on it through its Web Clipper service, so Joplin must be open with the service on.",
		AboutRu: "Joplin — заметки с открытым кодом. Сервер работает на этой машине и говорит с приложением Joplin через его сервис Web Clipper, поэтому Joplin должен быть открыт, а сервис включён.",
		Can: []string{
			"Search notes and notebooks",
			"Read a note",
			"Create a note or change an existing one",
			"Put tags and move notes between notebooks",
		},
		CanRu: []string{
			"Найдёт заметки и блокноты",
			"Прочитает заметку",
			"Создаст заметку или поправит существующую",
			"Расставит теги и перенесёт заметки между блокнотами",
		},
		Steps: []string{
			"In Joplin open Tools, Options, Web Clipper and switch the service on.",
			"In the same place, under Advanced options, copy the authorization token.",
			"Paste it below and press Connect. The first start downloads the server and takes a minute or two.",
		},
		StepsRu: []string{
			"В Joplin откройте «Инструменты», «Параметры», «Web Clipper» и включите сервис.",
			"Там же в «Дополнительных параметрах» скопируйте токен авторизации.",
			"Вставьте его ниже и нажмите «Подключить». Первый запуск скачивает сервер и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://joplinapp.org/help/api/references/rest_api/"),
			link(LinkSetup, "https://joplinapp.org/help/apps/clipper"),
			link(LinkHome, "https://joplinapp.org/"),
		},
	},
	"obsidian": {
		About:   "Obsidian is notes as plain files in a vault. The server runs on this machine and talks to the vault through the Local REST API plugin, so Obsidian must be open with the plugin on.",
		AboutRu: "Obsidian — заметки в виде обычных файлов в хранилище. Сервер работает на этой машине и говорит с хранилищем через плагин Local REST API, поэтому Obsidian должен быть открыт, а плагин включён.",
		Can: []string{
			"Search notes across the vault",
			"Read a note with its links",
			"Append to a note or change a part of it",
			"List the files of a folder",
		},
		CanRu: []string{
			"Найдёт заметки по всему хранилищу",
			"Прочитает заметку со ссылками",
			"Допишет заметку или поправит её часть",
			"Покажет файлы папки",
		},
		Steps: []string{
			"In Obsidian install and turn on the community plugin Local REST API.",
			"Open the plugin's settings and copy its API key.",
			"Paste it below and press Connect. The first start downloads the server and takes a minute or two.",
		},
		StepsRu: []string{
			"В Obsidian установите и включите community-плагин Local REST API.",
			"Откройте настройки плагина и скопируйте API-ключ.",
			"Вставьте его ниже и нажмите «Подключить». Первый запуск скачивает сервер и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/coddingtonbear/obsidian-local-rest-api"),
			link(LinkHome, "https://obsidian.md/"),
			link(LinkPricing, "https://obsidian.md/pricing"),
		},
	},
	"elevenlabs": {
		About:   "ElevenLabs makes lifelike speech and voices. The server runs on this machine with your API key: the agent reads text aloud, clones or picks a voice and saves the audio, spending your plan's characters.",
		AboutRu: "ElevenLabs делает живую речь и голоса. Сервер работает на этой машине с вашим API-ключом: агент озвучит текст, выберет или клонирует голос и сохранит звук, потратив символы вашего тарифа.",
		Can: []string{
			"Turn a text into speech with the voice you pick",
			"List the voices available to your account",
			"Save the audio as a file",
			"Check how much of the plan is left",
		},
		CanRu: []string{
			"Озвучит текст выбранным голосом",
			"Покажет голоса, доступные в вашем аккаунте",
			"Сохранит звук в файл",
			"Проверит, сколько тарифа осталось",
		},
		Steps: []string{
			"Open the API keys page of ElevenLabs and create a key (a restricted one is enough for speech).",
			"Copy the key and paste it in the field below.",
			"Press Connect. The first start downloads the server and takes a minute.",
		},
		StepsRu: []string{
			"Откройте страницу API-ключей ElevenLabs и создайте ключ (для речи хватит ограниченного).",
			"Скопируйте ключ и вставьте его в поле ниже.",
			"Нажмите «Подключить». Первый запуск скачивает сервер и занимает минуту.",
		},
		Links: []Link{
			link(LinkDocs, "https://elevenlabs.io/docs"),
			link(LinkToken, "https://elevenlabs.io/app/settings/api-keys"),
			link(LinkPricing, "https://elevenlabs.io/pricing"),
		},
	},
	"youtube": {
		About:   "YouTube Data brings public data of YouTube to the agent: videos, channels, comments and transcripts. It runs on this machine with an API key of your Google project; the key is free within a daily quota.",
		AboutRu: "YouTube Data даёт агенту открытые данные YouTube: видео, каналы, комментарии и субтитры. Работает на этой машине с API-ключом вашего проекта Google; ключ бесплатный в пределах суточной квоты.",
		Can: []string{
			"Search videos and channels",
			"Show views, likes and the growth of a channel",
			"Fetch the transcript of a video",
			"Read comments and find trends in a niche",
		},
		CanRu: []string{
			"Найдёт видео и каналы",
			"Покажет просмотры, лайки и рост канала",
			"Достанет субтитры видео",
			"Прочитает комментарии и найдёт тренды в нише",
		},
		Steps: []string{
			"Open the YouTube Data API v3 page in Google Cloud and press Enable.",
			"Under Credentials create an API key and copy it.",
			"Paste it below and press Connect. The first start downloads the server and takes a minute or two.",
		},
		StepsRu: []string{
			"Откройте страницу YouTube Data API v3 в Google Cloud и нажмите «Включить».",
			"В разделе «Учётные данные» создайте API-ключ и скопируйте его.",
			"Вставьте его ниже и нажмите «Подключить». Первый запуск скачивает сервер и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/kirbah/mcp-youtube"),
			link(LinkSetup, "https://developers.google.com/youtube/v3/getting-started"),
			link(LinkToken, "https://console.cloud.google.com/apis/library/youtube.googleapis.com"),
		},
	},
}
