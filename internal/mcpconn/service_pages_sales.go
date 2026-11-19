package mcpconn

// salesSignIn is the connection steps of a remote server that signs in through OAuth: the same three moves, with
// what the vendor asks of the person on its own sign-in page added to the second.
func salesSignIn(name, nameRu, note, noteRu string) ([]string, []string) {
	return []string{
			"Press \"Sign in with " + name + "\": its page opens in a new window.",
			"Sign in to your " + name + " account and allow access for the agent." + note,
			"The window closes by itself and the server shows its tools here.",
		}, []string{
			"Нажмите «Войти через " + nameRu + "»: откроется страница сервиса в новом окне.",
			"Войдите в аккаунт " + nameRu + " и разрешите доступ агенту." + noteRu,
			"Окно закроется само, а здесь появятся инструменты сервера.",
		}
}

func salesOAuthGuide(g guide, name, nameRu, note, noteRu string) guide {
	g.Steps, g.StepsRu = salesSignIn(name, nameRu, note, noteRu)
	return g
}

// guidesSales is the pages of the CRM, support, meeting and payment services.
var guidesSales = map[string]guide{
	"granola": salesOAuthGuide(guide{
		About:   "Granola is a notepad that records your meetings and writes the notes. Connected, the agent searches those notes and transcripts and answers from them. The free plan reaches only the last 30 days of notes; transcripts and editing need a paid plan.",
		AboutRu: "Granola записывает ваши встречи и пишет по ним заметки. Подключённый, агент ищет по заметкам и расшифровкам и отвечает по ним. На бесплатном тарифе доступны заметки только за последние 30 дней; расшифровки и правки требуют платного тарифа.",
		Can: []string{
			"Search meetings by topic, attendee, company, date or folder",
			"Read a meeting's summary, private notes and attendees",
			"Find passages in transcripts and read a full transcript (paid plans)",
			"Ask Granola Chat a question across the whole meeting history",
			"Create folders, move notes between them and rename meetings (paid plans)",
		},
		CanRu: []string{
			"Найдёт встречи по теме, участнику, компании, дате или папке",
			"Прочитает итоги встречи, личные заметки и список участников",
			"Найдёт места в расшифровках и прочитает расшифровку целиком (платные тарифы)",
			"Задаст вопрос Granola Chat по всей истории встреч",
			"Создаст папки, разложит по ним заметки и переименует встречи (платные тарифы)",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.granola.ai/help-center/sharing/integrations/mcp"),
			link(LinkHome, "https://www.granola.ai/"),
			link(LinkPricing, "https://www.granola.ai/pricing"),
		},
	}, "Granola", "Granola", " Use the account whose notes you want the agent to see.", " Выберите тот аккаунт, чьи заметки должен видеть агент."),

	"fireflies": {
		About:   "Fireflies records and transcribes calls and keeps summaries and action items. Connected with your API key, the agent searches transcripts, reads summaries and can organise meetings. The key acts as you, so it sees what your account sees.",
		AboutRu: "Fireflies записывает и расшифровывает звонки, хранит итоги и задачи. С вашим API-ключом агент ищет по расшифровкам, читает итоги и может наводить порядок во встречах. Ключ действует от вашего имени, поэтому агент видит то же, что и вы.",
		Can: []string{
			"Search meetings by keyword, date, organizer, participant or folder",
			"Read a full transcript with speakers and timestamps",
			"Fetch a summary with the overview, keywords and action items",
			"Share a meeting by email, rename it or move it to a folder",
			"Cut a soundbite from a transcript and show team meeting analytics",
		},
		CanRu: []string{
			"Найдёт встречи по ключевому слову, дате, организатору, участнику или папке",
			"Прочитает расшифровку целиком, с говорящими и временными метками",
			"Достанет итоги: обзор, ключевые слова и задачи",
			"Откроет встречу по почте, переименует её или переложит в папку",
			"Вырежет короткий фрагмент из расшифровки и покажет аналитику встреч команды",
		},
		Steps: []string{
			"Open Fireflies, go to Settings and then Developer Settings, and copy your API key.",
			"Paste the key in the field below: the Bearer prefix is added for you.",
			"Press Connect and wait for the tools to appear.",
		},
		StepsRu: []string{
			"Откройте Fireflies, зайдите в Settings, затем в Developer Settings и скопируйте свой API-ключ.",
			"Вставьте ключ в поле ниже: префикс Bearer добавится сам.",
			"Нажмите «Подключить» и дождитесь списка инструментов.",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.fireflies.ai/getting-started/mcp-configuration"),
			link(LinkSetup, "https://docs.fireflies.ai/mcp-tools/overview"),
			link(LinkToken, "https://docs.fireflies.ai/fundamentals/authorization"),
			link(LinkHome, "https://fireflies.ai/"),
			link(LinkPricing, "https://fireflies.ai/pricing"),
		},
	},

	"intercom": salesOAuthGuide(guide{
		About:   "Intercom is a customer messaging and support platform. Connected, the agent searches conversations, contacts, companies and tickets and works with Help Center articles. This entry uses the US address of the server: EU-hosted workspaces have their own, and Australian ones are not supported yet.",
		AboutRu: "Intercom — платформа для переписки с клиентами и поддержки. Подключённый, агент ищет диалоги, контакты, компании и тикеты и работает со статьями Справочного центра. Запись использует американский адрес сервера: у рабочих пространств в ЕС свой адрес, а австралийские пока не поддерживаются.",
		Can: []string{
			"Search conversations and contacts with filters on state, source, assignee and date",
			"Open a conversation or ticket with all its parts",
			"Find a contact or a company and read its custom attributes",
			"Search Help Center articles and read them in full",
			"Create and update Help Center articles as drafts or published",
		},
		CanRu: []string{
			"Найдёт диалоги и контакты по статусу, каналу, ответственному и дате",
			"Откроет диалог или тикет со всей перепиской",
			"Найдёт контакт или компанию и покажет их пользовательские атрибуты",
			"Поищет статьи Справочного центра и прочитает их целиком",
			"Создаст и обновит статьи Справочного центра: черновиком или сразу с публикацией",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.intercom.com/docs/guides/mcp"),
			link(LinkHome, "https://www.intercom.com/"),
			link(LinkPricing, "https://www.intercom.com/pricing"),
		},
	}, "Intercom", "Intercom", " Pick the workspace the agent should work in.", " Выберите рабочее пространство, в котором должен работать агент."),

	"paypal": salesOAuthGuide(guide{
		About:   "PayPal's own server for merchants: the agent works with invoices, orders, subscriptions, disputes and transactions of your business account. It acts on the live account, so money actions deserve a careful read before you confirm them.",
		AboutRu: "Сервер от самого PayPal для продавцов: агент работает со счетами, заказами, подписками, спорами и транзакциями вашего бизнес-аккаунта. Он действует на боевом аккаунте, поэтому действия с деньгами стоит внимательно читать перед подтверждением.",
		Can: []string{
			"Create, send and cancel invoices and send a reminder",
			"Create an order and pay an authorized one",
			"List transactions and open a refund or a dispute",
			"Manage products, subscription plans and subscriptions",
			"Add shipment tracking to an order",
		},
		CanRu: []string{
			"Создаст, отправит и отменит счёт и отправит напоминание",
			"Создаст заказ и оплатит авторизованный",
			"Покажет транзакции и откроет возврат или спор",
			"Заведёт товары и планы подписки и управится с подписками",
			"Добавит отслеживание отправления к заказу",
		},
		Links: []Link{
			link(LinkDocs, "https://developer.paypal.com/tools/mcp-server/"),
			link(LinkSetup, "https://github.com/paypal/paypal-mcp-server"),
			link(LinkHome, "https://www.paypal.com/"),
		},
	}, "PayPal", "PayPal", " Allow it for the business account you work with.", " Разрешите доступ к бизнес-аккаунту, с которым работаете."),

	"brevo": salesOAuthGuide(guide{
		About:   "Brevo is a marketing and sales platform: email, SMS and WhatsApp campaigns, contacts and a light CRM. Connected, the agent works with the same data you see in the Brevo account. This is the main server with all modules; Brevo also publishes smaller servers for single areas.",
		AboutRu: "Brevo — платформа для маркетинга и продаж: рассылки по почте, SMS и WhatsApp, контакты и лёгкая CRM. Подключённый, агент работает с теми же данными, что вы видите в аккаунте Brevo. Это основной сервер со всеми модулями; для отдельных областей у Brevo есть серверы поменьше.",
		Can: []string{
			"Find, add and update contacts, lists and segments",
			"Prepare an email campaign and read its performance",
			"Work with SMS and WhatsApp campaigns",
			"Manage CRM deals, companies, tasks and notes",
			"Edit email templates and transactional templates",
		},
		CanRu: []string{
			"Найдёт, добавит и обновит контакты, списки и сегменты",
			"Подготовит email-рассылку и прочитает её результаты",
			"Поработает с кампаниями в SMS и WhatsApp",
			"Поведёт сделки, компании, задачи и заметки в CRM",
			"Поправит шаблоны писем и транзакционные шаблоны",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.brevo.com/docs/mcp-protocol"),
			link(LinkHome, "https://www.brevo.com/"),
			link(LinkPricing, "https://www.brevo.com/pricing/"),
		},
	}, "Brevo", "Brevo", " Pick the account whose contacts and campaigns the agent should see.", " Выберите аккаунт, чьи контакты и кампании должен видеть агент."),

	"mailchimp": salesOAuthGuide(guide{
		About:   "Mailchimp is an email marketing service. Its server asks for access to audiences, contacts, segments, campaigns, reports and content, so the agent can look through your lists and the results of your mailings. Mailchimp has not published a tool list for this address, so check what your client shows after connecting.",
		AboutRu: "Mailchimp — сервис email-рассылок. Его сервер запрашивает доступ к аудиториям, контактам, сегментам, кампаниям, отчётам и контенту, поэтому агент может просмотреть ваши списки и результаты рассылок. Список инструментов для этого адреса Mailchimp не публиковал, так что смотрите, что покажет клиент после подключения.",
		Can: []string{
			"Look through audiences, contacts and segments",
			"Read campaigns and their reports",
			"Work with email content and templates",
			"Read e-commerce data and webhooks of the account",
		},
		CanRu: []string{
			"Просмотрит аудитории, контакты и сегменты",
			"Прочитает кампании и их отчёты",
			"Поработает с содержимым писем и шаблонами",
			"Прочитает данные интернет-магазина и вебхуки аккаунта",
		},
		Links: []Link{
			link(LinkDocs, "https://mailchimp.com/developer/"),
			link(LinkHome, "https://mailchimp.com/"),
			link(LinkPricing, "https://mailchimp.com/pricing/"),
		},
	}, "Mailchimp", "Mailchimp", " Choose the account that holds the audiences.", " Выберите аккаунт, в котором лежат аудитории."),

	"mercury": salesOAuthGuide(guide{
		About:   "Mercury is a US bank for startups. Its hosted server lets the agent read business accounts, balances, transactions and statements. Sign-in is an OAuth approval in your Mercury account, so the agent sees only what that account can see.",
		AboutRu: "Mercury — американский банк для стартапов. Его сервер позволяет агенту читать бизнес-счета, остатки, операции и выписки. Вход идёт через подтверждение в аккаунте Mercury, так что агент видит только то, что доступно этому аккаунту.",
		Can: []string{
			"Show accounts and their current balances",
			"List transactions of an account and open one by its ID",
			"Fetch account statements for a period",
			"Look up cards, recipients and treasury accounts",
		},
		CanRu: []string{
			"Покажет счета и их текущие остатки",
			"Выведет операции по счёту и откроет нужную по ID",
			"Достанет выписки по счёту за период",
			"Найдёт карты, получателей и казначейские счета",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.mercury.com/docs/mcp"),
			link(LinkHome, "https://mercury.com/"),
			link(LinkPricing, "https://mercury.com/pricing"),
		},
	}, "Mercury", "Mercury", " Approve only the access you want to give.", " Подтверждайте только тот доступ, который готовы дать."),

	"ramp": salesOAuthGuide(guide{
		About:   "Ramp is corporate cards and spend management. Connected, the agent analyses spend and works with cards, bills, reimbursements and approvals, with the rights of the person who signed in. This entry uses Ramp's earlier server address; Ramp now documents mcp.ramp.com for the same service.",
		AboutRu: "Ramp — корпоративные карты и управление расходами. Подключённый, агент разбирает траты и работает с картами, счетами, возмещениями и согласованиями с правами того, кто вошёл. Запись использует прежний адрес сервера: Ramp теперь описывает для этого сервиса адрес mcp.ramp.com.",
		Can: []string{
			"Find card transactions by merchant, person, date or amount",
			"Build a spend report by team, category or period",
			"Look up bills and reimbursements and their status",
			"Process approvals the signed-in user is allowed to give",
		},
		CanRu: []string{
			"Найдёт траты по картам по продавцу, сотруднику, дате или сумме",
			"Соберёт отчёт по расходам по команде, категории или периоду",
			"Посмотрит счета и возмещения и их статус",
			"Проведёт согласования, которые разрешены вошедшему пользователю",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.ramp.com/developer-api/v1/ramp-mcp"),
			link(LinkSetup, "https://agents.ramp.com/docs/guides/connecting"),
			link(LinkHome, "https://ramp.com/"),
			link(LinkPricing, "https://ramp.com/pricing"),
		},
	}, "Ramp", "Ramp", " On the consent screen choose Read-only if you only want reports; the connection expires after 30 days.", " На экране согласия выберите Read-only, если нужны только отчёты; подключение истекает через 30 дней."),

	"airwallex": salesOAuthGuide(guide{
		About:   "Airwallex is a global business account with payments, transfers and cards. Connected, the agent reads balances, accounts and transfers, and with the edit permissions you grant it can also work with cards and payment links. You approve each scope on the sign-in page.",
		AboutRu: "Airwallex — глобальный бизнес-счёт с платежами, переводами и картами. Подключённый, агент читает остатки, счета и переводы, а при выданных правах на изменение может работать и с картами и платёжными ссылками. Каждое право вы подтверждаете на странице входа.",
		Can: []string{
			"Show balances and global accounts by currency",
			"List transfers and currency conversions",
			"Read issued cards, cardholders and card transactions",
			"Look up billing invoices, subscriptions and payment links",
			"Create payment links when you allow editing",
		},
		CanRu: []string{
			"Покажет остатки и глобальные счета по валютам",
			"Выведет переводы и конвертации валют",
			"Прочитает выпущенные карты, держателей и операции по картам",
			"Найдёт счета, подписки и платёжные ссылки",
			"Создаст платёжную ссылку, если вы разрешите изменения",
		},
		Links: []Link{
			link(LinkDocs, "https://www.airwallex.com/docs/developer-tools/ai-and-mcp"),
			link(LinkHome, "https://www.airwallex.com/"),
		},
	}, "Airwallex", "Airwallex", " Untick the permissions you do not want to give.", " Снимите галочки с тех прав, которые давать не хотите."),

	"apollo": salesOAuthGuide(guide{
		About:   "Apollo.io is a sales database and outreach tool. Connected, the agent finds people and companies, enriches them and works with your contacts, deals and sequences. Enrichment spends credits of your Apollo plan, and Apollo forbids using its data to train models.",
		AboutRu: "Apollo.io — база контактов для продаж и инструмент рассылок. Подключённый, агент ищет людей и компании, обогащает их данные и работает с вашими контактами, сделками и последовательностями. Обогащение тратит кредиты вашего тарифа Apollo, а обучать на данных Apollo модели запрещено.",
		Can: []string{
			"Search companies and people by industry, headcount, title and location",
			"Enrich a person or a company, one at a time or in bulk",
			"Create and update contacts and search the saved ones in plain words",
			"Find deals in your pipeline",
			"Write or improve sequences and enroll contacts in them",
		},
		CanRu: []string{
			"Найдёт компании и людей по отрасли, размеру, должности и месту",
			"Обогатит данные человека или компании: по одному и пачкой",
			"Создаст и обновит контакты и найдёт сохранённые обычными словами",
			"Найдёт сделки в вашей воронке",
			"Напишет или улучшит последовательность и добавит в неё контакты",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.apollo.io/docs/apollo-mcp"),
			link(LinkHome, "https://www.apollo.io/"),
			link(LinkPricing, "https://www.apollo.io/pricing"),
		},
	}, "Apollo.io", "Apollo.io", " Turn model training off in your AI client first: Apollo asks for that.", " Сначала отключите обучение моделей в вашем ИИ-клиенте: этого требует Apollo."),

	"attio": salesOAuthGuide(guide{
		About:   "Attio is a customizable CRM. Connected, the agent reads and updates records of people, companies, deals and your own objects, notes, tasks and lists. Reads go through at once; writes ask for confirmation.",
		AboutRu: "Attio — настраиваемая CRM. Подключённый, агент читает и обновляет записи о людях, компаниях, сделках и ваших объектах, заметки, задачи и списки. Чтение проходит сразу, а запись просит подтверждения.",
		Can: []string{
			"Search records by name, email or domain and read their full details",
			"Create a record or update one by a matching attribute such as email",
			"Add notes and create tasks with deadlines",
			"Work with lists, views and comments",
			"Search call recordings and meeting notes, and run reporting queries in SQL",
		},
		CanRu: []string{
			"Найдёт записи по имени, почте или домену и прочитает их целиком",
			"Создаст запись или обновит её по совпадению, например по почте",
			"Добавит заметки и создаст задачи со сроком",
			"Поработает со списками, представлениями и комментариями",
			"Найдёт записи звонков и заметки встреч и выполнит отчётные запросы на SQL",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.attio.com/mcp/overview"),
			link(LinkHome, "https://attio.com/"),
			link(LinkPricing, "https://attio.com/pricing"),
		},
	}, "Attio", "Attio", " Pick the workspace to give access to.", " Выберите рабочее пространство, к которому даёте доступ."),

	"circleback": salesOAuthGuide(guide{
		About:   "Circleback records meetings and writes notes, action items and insights. Connected, the agent searches meetings and transcripts and can also update notes, share meetings and manage action items.",
		AboutRu: "Circleback записывает встречи и пишет заметки, задачи и выводы. Подключённый, агент ищет по встречам и расшифровкам, а ещё может править заметки, делиться встречами и вести задачи.",
		Can: []string{
			"Find meetings by keyword, date, tag, attendee, domain or folder",
			"Read notes, attendees, insights and the full transcript with speakers",
			"Search action items by status, assignee or date and create new ones",
			"Update a meeting's name, notes, tags or folders",
			"Share a meeting with a person, a team or by link",
		},
		CanRu: []string{
			"Найдёт встречи по слову, дате, тегу, участнику, домену или папке",
			"Прочитает заметки, участников, выводы и расшифровку с говорящими",
			"Найдёт задачи по статусу, исполнителю или дате и создаст новые",
			"Обновит название встречи, заметки, теги или папки",
			"Откроет встречу человеку, команде или по ссылке",
		},
		Links: []Link{
			link(LinkDocs, "https://circleback.ai/docs/mcp"),
			link(LinkHome, "https://circleback.ai/"),
			link(LinkPricing, "https://circleback.ai/pricing"),
		},
	}, "Circleback", "Circleback", " Use the account that owns the meetings.", " Войдите в аккаунт, которому принадлежат встречи."),

	"close": salesOAuthGuide(guide{
		About:   "Close is a CRM for sales teams with calls, email and SMS inside. Connected, the agent searches leads, contacts, opportunities and activities and, with the write scopes you allow, creates and updates them. Sending email or SMS and deleting data are separate, stricter scopes.",
		AboutRu: "Close — CRM для отделов продаж со звонками, почтой и SMS внутри. Подключённый, агент ищет лиды, контакты, сделки и активности и, при выданных правах на запись, создаёт и обновляет их. Отправка писем и SMS и удаление данных вынесены в отдельные, более строгие права.",
		Can: []string{
			"Search leads, contacts and opportunities and count or sum them by field",
			"Find calls, meetings and emails and read a meeting transcript",
			"Create leads, contacts, notes, tasks and opportunities",
			"Draft an email or an SMS from a template",
			"Look up pipelines, statuses and custom fields",
		},
		CanRu: []string{
			"Найдёт лиды, контакты и сделки и посчитает их по полям",
			"Найдёт звонки, встречи и письма и прочитает расшифровку встречи",
			"Создаст лиды, контакты, заметки, задачи и сделки",
			"Подготовит черновик письма или SMS по шаблону",
			"Покажет воронки, статусы и пользовательские поля",
		},
		Links: []Link{
			link(LinkDocs, "https://developer.close.com/mcp/tools"),
			link(LinkSetup, "https://developer.close.com/"),
			link(LinkHome, "https://www.close.com/"),
			link(LinkPricing, "https://www.close.com/pricing"),
		},
	}, "Close", "Close", " Allow only the scopes you need: read, safe write or destructive.", " Давайте только нужные права: чтение, безопасная запись или удаление."),

	"fathom": salesOAuthGuide(guide{
		About:   "Fathom records and summarizes video calls. Its official server gives the agent your meeting data: recordings, summaries and transcripts. Fathom has not published a tool list, so what the agent can do is shown by your client after connecting.",
		AboutRu: "Fathom записывает видеозвонки и делает по ним итоги. Его официальный сервер отдаёт агенту данные ваших встреч: записи, итоги и расшифровки. Список инструментов Fathom не публиковал, так что что именно умеет агент, покажет ваш клиент после подключения.",
		Can: []string{
			"Find a call by topic, date or participant",
			"Read the summary and action items of a call",
			"Read a transcript and tell who said what",
		},
		CanRu: []string{
			"Найдёт звонок по теме, дате или участнику",
			"Прочитает итоги звонка и задачи из него",
			"Прочитает расшифровку и подскажет, кто что сказал",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.fathom.ai/mcp-docs"),
			link(LinkSetup, "https://developers.fathom.ai/"),
			link(LinkHome, "https://fathom.ai/"),
			link(LinkPricing, "https://fathom.ai/pricing"),
		},
	}, "Fathom", "Fathom", " Use the account that holds the calls.", " Войдите в аккаунт, в котором лежат звонки."),

	"klaviyo": salesOAuthGuide(guide{
		About:   "Klaviyo is email and SMS marketing for online stores. Connected, the agent reads profiles, lists, segments, campaigns, flows and metrics and can prepare drafts for you to review. Remote access needs an Owner, Admin or Manager role in Klaviyo.",
		AboutRu: "Klaviyo — email- и SMS-маркетинг для интернет-магазинов. Подключённый, агент читает профили, списки, сегменты, кампании, потоки и метрики и может подготовить черновики для вашей проверки. Для удалённого доступа в Klaviyo нужна роль Owner, Admin или Manager.",
		Can: []string{
			"Report on campaign and flow performance by period",
			"Rank campaigns by click rate or revenue and compare email with SMS",
			"Find profiles and look up segments and lists",
			"Draft a segment, a template or a campaign for a person to review",
		},
		CanRu: []string{
			"Расскажет, как отработали кампании и потоки за период",
			"Расставит кампании по кликам или выручке и сравнит почту с SMS",
			"Найдёт профили и посмотрит сегменты и списки",
			"Подготовит черновик сегмента, шаблона или кампании на проверку человеку",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.klaviyo.com/en/docs/klaviyo_mcp_server"),
			link(LinkSetup, "https://developers.klaviyo.com/en/docs/connect_to_the_klaviyo_mcp_server"),
			link(LinkHome, "https://www.klaviyo.com/"),
			link(LinkPricing, "https://www.klaviyo.com/pricing"),
		},
	}, "Klaviyo", "Klaviyo", " If you have several accounts, pick the right one on the consent screen.", " Если аккаунтов несколько, выберите нужный на экране согласия."),

	"mercadopago": salesOAuthGuide(guide{
		About:   "Mercado Pago's server is a developer tool: it helps integrate and test payments rather than read your sales. The agent searches the official documentation, manages your applications and webhooks, creates test users and runs the integration quality check. Application management works only through OAuth.",
		AboutRu: "Сервер Mercado Pago — инструмент разработчика: он помогает подключать и тестировать платежи, а не читать продажи. Агент ищет по официальной документации, управляет вашими приложениями и вебхуками, создаёт тестовых пользователей и запускает проверку качества интеграции. Управление приложениями работает только через OAuth.",
		Can: []string{
			"Search the official Mercado Pago documentation",
			"Create applications and get their credentials",
			"Configure and monitor webhook notifications",
			"Create test users and manage their funds",
			"Run the quality check of an integration before going live",
		},
		CanRu: []string{
			"Поищет по официальной документации Mercado Pago",
			"Создаст приложения и достанет их учётные данные",
			"Настроит и проверит вебхук-уведомления",
			"Создаст тестовых пользователей и распорядится их средствами",
			"Запустит проверку качества интеграции перед запуском в работу",
		},
		Links: []Link{
			link(LinkDocs, "https://www.mercadopago.com/developers/en/docs/mcp-server/overview"),
			link(LinkSetup, "https://www.mercadopago.com/developers/en/docs/mcp-server/tools"),
		},
	}, "Mercado Pago", "Mercado Pago", " Choose the country account that owns your applications.", " Выберите аккаунт той страны, которой принадлежат ваши приложения."),

	"otter": salesOAuthGuide(guide{
		About:   "Otter.ai records and transcribes meetings. Its official server is small and reads only: the agent searches meetings and their transcripts and opens the ones it finds. It sees what your account captured or what others shared with you.",
		AboutRu: "Otter.ai записывает и расшифровывает встречи. Официальный сервер небольшой и только читает: агент ищет встречи и их расшифровки и открывает найденные. Он видит то, что записал ваш аккаунт или что открыли вам другие.",
		Can: []string{
			"Search meetings by meaning, attendee, date range, folder or transcript keyword",
			"Open a found meeting with its summary and transcript",
			"Show which Otter account is connected",
		},
		CanRu: []string{
			"Найдёт встречи по смыслу, участнику, периоду, папке или слову из расшифровки",
			"Откроет найденную встречу с итогами и расшифровкой",
			"Покажет, какой аккаунт Otter подключён",
		},
		Links: []Link{
			link(LinkDocs, "https://claude.com/connectors/otter-ai"),
			link(LinkHome, "https://otter.ai/"),
			link(LinkPricing, "https://otter.ai/pricing"),
		},
	}, "Otter.ai", "Otter.ai", " Use the account that holds your meetings.", " Войдите в аккаунт, где лежат ваши встречи."),

	"readai": salesOAuthGuide(guide{
		About:   "Read AI summarizes meetings and gives them a transcript and a report. Its server is in open beta and has a small set of tools: the agent lists your meetings and reads one by its ID. Sign-in is OAuth 2.1 with your Read AI account.",
		AboutRu: "Read AI делает по встречам итоги, расшифровку и отчёт. Его сервер в открытой бете и умеет немного: агент перечисляет ваши встречи и читает одну по её ID. Вход идёт через OAuth 2.1 с вашим аккаунтом Read AI.",
		Can: []string{
			"List your meetings",
			"Read a meeting by its ID: summary, notes and transcript",
			"Tell what was decided on a call and who owns the next step",
		},
		CanRu: []string{
			"Выведет список ваших встреч",
			"Прочитает встречу по ID: итоги, заметки и расшифровку",
			"Расскажет, что решили на звонке и у кого следующий шаг",
		},
		Links: []Link{
			link(LinkDocs, "https://support.read.ai/hc/en-us/articles/49381158409491-MCP-Server"),
			link(LinkHome, "https://www.read.ai/"),
			link(LinkPricing, "https://www.read.ai/pricing"),
		},
	}, "Read AI", "Read AI", " The server is in beta, so the list of tools may change.", " Сервер в бете, поэтому набор инструментов может меняться."),

	"tldv": salesOAuthGuide(guide{
		About:   "tl;dv records Google Meet, Zoom and Teams calls and writes notes. Its hosted connector gives the agent your meeting library: lists of meetings, their details, transcripts and highlights. Some functions depend on your tl;dv plan, and tl;dv has not published the tool names for this server.",
		AboutRu: "tl;dv записывает звонки в Google Meet, Zoom и Teams и пишет заметки. Его облачный коннектор отдаёт агенту вашу библиотеку встреч: списки встреч, их данные, расшифровки и важные моменты. Часть функций зависит от вашего тарифа tl;dv, а названия инструментов этого сервера tl;dv не публиковал.",
		Can: []string{
			"List meetings and open the details of one",
			"Read a transcript of a recorded call",
			"Pull out highlights and notes of a meeting",
		},
		CanRu: []string{
			"Выведет список встреч и откроет данные одной из них",
			"Прочитает расшифровку записанного звонка",
			"Достанет важные моменты и заметки встречи",
		},
		Links: []Link{
			link(LinkDocs, "https://tldv.io/blog/how-to-connect-meeting-notes-to-claude-with-mcp/"),
			link(LinkSetup, "https://doc.tldv.io/"),
			link(LinkHome, "https://tldv.io/"),
			link(LinkPricing, "https://tldv.io/pricing/"),
		},
	}, "tl;dv", "tl;dv", " Use the account that records your meetings.", " Войдите в аккаунт, который записывает ваши встречи."),

	"braze": salesOAuthGuide(guide{
		About:   "Braze is a customer engagement platform. Its remote server shows the agent campaign and Canvas results, segments and custom attributes, and lets it prepare content. Nothing that exposes user-level personal data is available, and the agent has the rights of your Braze dashboard user.",
		AboutRu: "Braze — платформа для работы с клиентами. Его удалённый сервер показывает агенту результаты кампаний и Canvas, сегменты и пользовательские атрибуты и позволяет готовить контент. Данных, раскрывающих личные сведения отдельных пользователей, там нет, а права у агента такие же, как у вашего пользователя в дашборде Braze.",
		Can: []string{
			"Report on campaign and Canvas performance",
			"Explore segments and custom attributes",
			"Manage campaigns, segments and tags",
			"Create email templates, Content Blocks and media library assets",
			"Read the brand guidelines",
		},
		CanRu: []string{
			"Расскажет о результатах кампаний и Canvas",
			"Разберёт сегменты и пользовательские атрибуты",
			"Поведёт кампании, сегменты и теги",
			"Создаст шаблоны писем, Content Blocks и файлы медиатеки",
			"Прочитает руководство по бренду",
		},
		Links: []Link{
			link(LinkDocs, "https://www.braze.com/docs/user_guide/brazeai/mcp_server"),
			link(LinkHome, "https://www.braze.com/"),
		},
	}, "Braze", "Braze", " A company admin has to approve the client under MCP Clients in Braze before the first sign-in.", " Перед первым входом администратор компании должен одобрить клиент в разделе MCP Clients в Braze."),

	"customer-io": salesOAuthGuide(guide{
		About:   "Customer.io is a platform for messages triggered by user behaviour. Its server hands the agent the full Journeys and Pipelines APIs through a few tools: it discovers endpoints, reads, writes and deletes. The agent never gets more rights than you have, and writes can be previewed first.",
		AboutRu: "Customer.io — платформа для сообщений по поведению пользователей. Его сервер отдаёт агенту API Journeys и Pipelines целиком через несколько инструментов: он находит эндпоинты, читает, пишет и удаляет. Прав у агента не больше, чем у вас, а перед записью можно посмотреть пробный прогон.",
		Can: []string{
			"Discover the endpoints and parameters of the Journeys and Pipelines APIs",
			"Read people, segments, campaigns and messages",
			"Create and edit automations and one-time sends, with a dry-run preview",
			"Delete resources after a preview",
			"Follow step-by-step skills for multi-step jobs",
		},
		CanRu: []string{
			"Найдёт эндпоинты и параметры API Journeys и Pipelines",
			"Прочитает людей, сегменты, кампании и сообщения",
			"Создаст и поправит автоматизации и разовые рассылки с пробным прогоном",
			"Удалит ресурсы после просмотра прогона",
			"Пройдёт по готовым сценариям для многошаговых задач",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.customer.io/ai/mcp/get-started/"),
			link(LinkHome, "https://customer.io/"),
			link(LinkPricing, "https://customer.io/pricing/"),
		},
	}, "Customer.io", "Customer.io", " EU accounts are routed to their region at sign-in.", " Аккаунты из ЕС при входе направляются в свой регион."),

	"dialpad": salesOAuthGuide(guide{
		About:   "Dialpad is business phone and meetings with AI call notes. Its server is in an early access program: until your company is enrolled, every call is refused with 403. Once connected, the agent has the same rights as the person who signed in, admin rights included.",
		AboutRu: "Dialpad — бизнес-телефония и встречи с ИИ-заметками по звонкам. Его сервер работает в программе раннего доступа: пока компания не подключена к ней, любой вызов отклоняется с ошибкой 403. После подключения у агента те же права, что у вошедшего человека, включая права администратора.",
		Can: []string{
			"Search call transcripts and fetch a chosen one",
			"Search meeting transcripts and fetch a chosen one",
			"Search messages",
			"Reach other data of the Dialpad Public API that your account can read",
		},
		CanRu: []string{
			"Найдёт расшифровки звонков и достанет выбранную",
			"Найдёт расшифровки встреч и достанет выбранную",
			"Поищет по сообщениям",
			"Доберётся до других данных публичного API Dialpad, которые доступны вашему аккаунту",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.dialpad.com/docs/dialpad-mcp-server"),
			link(LinkHome, "https://www.dialpad.com/"),
			link(LinkPricing, "https://www.dialpad.com/pricing/"),
		},
	}, "Dialpad", "Dialpad", " The session lasts 24 hours, then you sign in again.", " Сессия живёт 24 часа, потом нужно войти снова."),

	"greenhouse": salesOAuthGuide(guide{
		About:   "Greenhouse is recruiting software. The server at this address answers as the Greenhouse MCP Server and signs you in through Greenhouse, but the vendor has not published its documentation or tool list. Expect it to work with jobs, candidates and applications; check what your client shows after connecting.",
		AboutRu: "Greenhouse — программа для найма. Сервер по этому адресу отвечает как Greenhouse MCP Server и пускает через вход в Greenhouse, но документацию и список инструментов вендор не публиковал. Ожидайте работу с вакансиями, кандидатами и откликами; что именно доступно, покажет клиент после подключения.",
		Can: []string{
			"Look at jobs and the candidates who applied",
			"Read the applications and their stage",
			"Answer questions about a hiring pipeline from what your account may see",
		},
		CanRu: []string{
			"Посмотрит вакансии и кандидатов, которые откликнулись",
			"Прочитает отклики и их этап",
			"Ответит про воронку найма по тому, что доступно вашему аккаунту",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.greenhouse.io/"),
			link(LinkHome, "https://www.greenhouse.com/"),
		},
	}, "Greenhouse", "Greenhouse", " You need a Greenhouse user with the rights to see candidates.", " Нужен пользователь Greenhouse с правом видеть кандидатов."),

	"helpscout": salesOAuthGuide(guide{
		About:   "Help Scout is a shared inbox for customer support. Its official server is read-only: the agent finds and summarizes conversations and customer details but cannot reply, edit or delete. It needs a plan with API access (Standard or higher); reports need Plus or Pro.",
		AboutRu: "Help Scout — общий почтовый ящик для поддержки клиентов. Официальный сервер только читает: агент находит и пересказывает обращения и данные клиентов, но не отвечает, не правит и не удаляет. Нужен тариф с доступом к API (Standard и выше); отчёты требуют Plus или Pro.",
		Can: []string{
			"Search conversations by customer, mailbox, tag or status",
			"Read a conversation thread and summarize it",
			"Look up customer details and mailbox settings",
			"Pull reports (Plus and Pro plans)",
			"Search the Docs knowledge base once a Docs API key is added",
		},
		CanRu: []string{
			"Найдёт обращения по клиенту, ящику, тегу или статусу",
			"Прочитает цепочку обращения и перескажет её",
			"Посмотрит данные клиента и настройки ящика",
			"Достанет отчёты (тарифы Plus и Pro)",
			"Поищет по базе знаний Docs, если добавить ключ Docs API",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.helpscout.com/article/1779-connect-your-ai-agent-with-help-scout-to-search-conversations-and-pull-reports"),
			link(LinkSetup, "https://developer.helpscout.com/"),
			link(LinkHome, "https://www.helpscout.com/"),
			link(LinkPricing, "https://www.helpscout.com/pricing/"),
		},
	}, "Help Scout", "Help Scout", " Each person signs in with their own Help Scout user, and the agent sees only what that user may.", " Каждый входит под своим пользователем Help Scout, а агент видит только то, что разрешено этому пользователю."),

	"hunter": salesOAuthGuide(guide{
		About:   "Hunter finds and verifies work email addresses. Connected, the agent looks up addresses by domain or by person's name, checks that an address is deliverable and enriches people and companies. Each lookup spends credits of your Hunter plan.",
		AboutRu: "Hunter ищет и проверяет рабочие email-адреса. Подключённый, агент находит адреса по домену или по имени человека, проверяет, доставляется ли адрес, и обогащает данные людей и компаний. Каждый поиск тратит кредиты вашего тарифа Hunter.",
		Can: []string{
			"Find the addresses found on a company's domain, with their sources",
			"Find the likely address of a person by name and domain",
			"Check whether an address is deliverable",
			"Enrich a person or a company from an address or a domain",
			"Save a found contact as a lead",
		},
		CanRu: []string{
			"Найдёт адреса, встречающиеся на домене компании, с источниками",
			"Подберёт вероятный адрес человека по имени и домену",
			"Проверит, доставляется ли адрес",
			"Обогатит данные человека или компании по адресу или домену",
			"Сохранит найденный контакт как лид",
		},
		Links: []Link{
			link(LinkDocs, "https://hunter.io/api-documentation"),
			link(LinkHome, "https://hunter.io/"),
			link(LinkPricing, "https://hunter.io/pricing"),
		},
	}, "Hunter", "Hunter", " The read and write scopes are asked separately.", " Права на чтение и на запись запрашиваются отдельно."),

	"lusha": salesOAuthGuide(guide{
		About:   "Lusha is a B2B contact and company database. Connected, the agent searches and enriches contacts and companies, scores them and exports the result to a CRM. Verified emails and phone numbers cost credits of your Lusha plan.",
		AboutRu: "Lusha — база B2B-контактов и компаний. Подключённый, агент ищет и обогащает контакты и компании, оценивает их и выгружает результат в CRM. Проверенные почты и телефоны стоят кредитов вашего тарифа Lusha.",
		Can: []string{
			"Search contacts and companies by title, industry and location",
			"Enrich a contact with a verified email and phone number",
			"Rank a list by a buying-signal score and an ICP fit score",
			"Find lookalikes of your best customers",
			"Export contacts to Salesforce, HubSpot, Zoho, Pipedrive or SalesLoft",
		},
		CanRu: []string{
			"Найдёт контакты и компании по должности, отрасли и месту",
			"Дополнит контакт проверенной почтой и телефоном",
			"Расставит список по оценке сигнала покупки и соответствию профилю клиента",
			"Найдёт компании, похожие на ваших лучших клиентов",
			"Выгрузит контакты в Salesforce, HubSpot, Zoho, Pipedrive или SalesLoft",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.lusha.com/user-guide/mcp/lusha-model-context-protocol-mcp-server"),
			link(LinkHome, "https://www.lusha.com/"),
			link(LinkPricing, "https://www.lusha.com/pricing/"),
		},
	}, "Lusha", "Lusha", " The agent spends credits of the account you pick.", " Агент тратит кредиты того аккаунта, который вы выберете."),

	"paddle": salesOAuthGuide(guide{
		About:   "Paddle is a billing and payments service for software sellers. Its hosted server gives the agent the Paddle Billing API through a small set of tools: subscriptions, transactions, customers, products and prices. This entry is the live server; the sandbox is a separate address that takes only an API key.",
		AboutRu: "Paddle — сервис биллинга и платежей для продавцов софта. Его сервер даёт агенту Paddle Billing API через небольшой набор инструментов: подписки, транзакции, клиенты, товары и цены. Эта запись — боевой сервер; песочница живёт по отдельному адресу и принимает только API-ключ.",
		Can: []string{
			"Find a customer's subscriptions and their status",
			"List transactions and read one in full",
			"Look up customers, addresses and businesses",
			"Read products, prices and discounts",
		},
		CanRu: []string{
			"Найдёт подписки клиента и их статус",
			"Выведет транзакции и прочитает одну целиком",
			"Найдёт клиентов, их адреса и компании",
			"Прочитает товары, цены и скидки",
		},
		Links: []Link{
			link(LinkDocs, "https://developer.paddle.com/build/tools/ai/mcp"),
			link(LinkSetup, "https://developer.paddle.com/concepts/mcp"),
			link(LinkHome, "https://www.paddle.com/"),
			link(LinkPricing, "https://www.paddle.com/pricing"),
		},
	}, "Paddle", "Paddle", " This works on the live account: limit what the agent may do.", " Это боевой аккаунт: ограничьте то, что агенту можно делать."),

	"salesloft": salesOAuthGuide(guide{
		About:   "Salesloft is a sales engagement platform: cadences, calls and email. The server at this address signs you in through Salesloft accounts, but the vendor has published no tool list. Expect work with people, cadences and activities; check what your client shows after connecting.",
		AboutRu: "Salesloft — платформа для ведения клиентов в продажах: кадансы, звонки и почта. Сервер по этому адресу пускает через вход в аккаунт Salesloft, но список инструментов вендор не публиковал. Ожидайте работу с людьми, кадансами и активностями; что именно доступно, покажет клиент после подключения.",
		Can: []string{
			"Look up people and the accounts they belong to",
			"Show cadences and who replied to them",
			"Read the activities of a person: calls, emails and tasks",
		},
		CanRu: []string{
			"Найдёт людей и аккаунты, к которым они относятся",
			"Покажет кадансы и тех, кто на них ответил",
			"Прочитает активности по человеку: звонки, письма и задачи",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.salesloft.com/"),
			link(LinkHome, "https://www.salesloft.com/"),
		},
	}, "Salesloft", "Salesloft", " Use a user whose role may see the people you ask about.", " Войдите пользователем, чья роль позволяет видеть нужных людей."),

	"zoominfo": salesOAuthGuide(guide{
		About:   "ZoomInfo is a database of companies and contacts for sales research. Connected, the agent searches and enriches companies and contacts, finds buying intent and business events. Searches are free; enrichment spends Bulk Data credits, and the API access has to be part of your subscription.",
		AboutRu: "ZoomInfo — база компаний и контактов для исследований в продажах. Подключённый, агент ищет и обогащает компании и контакты, находит сигналы намерения купить и бизнес-события. Поиск бесплатный; обогащение тратит кредиты Bulk Data, а доступ к API должен входить в вашу подписку.",
		Can: []string{
			"Search companies and contacts by firmographic criteria, role or signal",
			"Enrich up to 25 companies or contacts per call",
			"Find companies similar to a given one and recommended contacts at an account",
			"Find buyer intent signals and business-event scoops",
			"Get categorized news about a company",
		},
		CanRu: []string{
			"Найдёт компании и контакты по признакам компании, роли или сигналу",
			"Обогатит до 25 компаний или контактов за вызов",
			"Подберёт компании, похожие на заданную, и рекомендуемые контакты в аккаунте",
			"Найдёт сигналы намерения купить и бизнес-события",
			"Достанет новости о компании по темам",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.zoominfo.com/docs/available-mcp-tools"),
			link(LinkHome, "https://www.zoominfo.com/"),
			link(LinkPricing, "https://www.zoominfo.com/pricing"),
		},
	}, "ZoomInfo", "ZoomInfo", " If it fails, check that your subscription includes API access.", " Если не заходит, проверьте, что в подписку входит доступ к API."),

	"gorgias": salesOAuthGuide(guide{
		About:   "Gorgias is a helpdesk for online stores. Connected, the agent works with tickets, customers, macros and the Help Center, and can also tune the Gorgias AI Agent and read its statistics. Writing tools post to real tickets, so check replies before they go out.",
		AboutRu: "Gorgias — служба поддержки для интернет-магазинов. Подключённый, агент работает с тикетами, клиентами, макросами и Справочным центром, а ещё может настраивать ИИ-агента Gorgias и читать его статистику. Инструменты записи пишут в живые тикеты, поэтому проверяйте ответы до отправки.",
		Can: []string{
			"Search tickets by text or filters and read one with its messages",
			"Post a reply, reply and close, or leave an internal note",
			"Apply a macro, snooze, escalate or tag tickets",
			"Write and publish Help Center articles",
			"Pull reporting stats and run read-only analytics queries",
		},
		CanRu: []string{
			"Найдёт тикеты по тексту или фильтрам и прочитает один с перепиской",
			"Отправит ответ, ответит и закроет тикет или оставит внутреннюю заметку",
			"Применит макрос, отложит или эскалирует тикет, расставит теги",
			"Напишет и опубликует статьи Справочного центра",
			"Достанет статистику отчётов и выполнит аналитические запросы только на чтение",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.gorgias.com/"),
			link(LinkHome, "https://www.gorgias.com/"),
			link(LinkPricing, "https://www.gorgias.com/pricing"),
		},
	}, "Gorgias", "Gorgias", " Choose the Gorgias account of your store.", " Выберите аккаунт Gorgias вашего магазина."),

	"square": salesOAuthGuide(guide{
		About:   "Square is payments and point of sale for shops and services. Its server is in beta: instead of one tool per action it lets the agent discover the methods of the Square API and call them, so it reaches payments, orders, customers, catalog and inventory. The hosted address works with real production data only.",
		AboutRu: "Square — приём платежей и касса для магазинов и сервисов. Сервер в бете: вместо отдельного инструмента на каждое действие он позволяет агенту узнать методы Square API и вызвать их, так что агент добирается до платежей, заказов, клиентов, каталога и остатков. Облачный адрес работает только с реальными боевыми данными.",
		Can: []string{
			"Discover the methods of a Square API service and what they take",
			"Look up payments, orders and customers",
			"Read catalog items and inventory counts",
			"Make a call to the Square API with the permissions you approve",
		},
		CanRu: []string{
			"Узнает методы сервиса Square API и что они принимают",
			"Найдёт платежи, заказы и клиентов",
			"Прочитает товары каталога и остатки",
			"Выполнит вызов Square API с теми правами, что вы подтвердите",
		},
		Links: []Link{
			link(LinkDocs, "https://developer.squareup.com/docs/mcp"),
			link(LinkSetup, "https://github.com/square/square-mcp-server"),
			link(LinkHome, "https://squareup.com/"),
		},
	}, "Square", "Square", " Approve only the scopes the task needs.", " Подтверждайте только те права, которые нужны задаче."),

	"tochka-mcp": {
		About:   "A community MCP server (not from Tochka Bank) for the Tochka business API. It reads accounts, balances and statements and can prepare an outgoing ruble payment, which a person still signs in the internet bank. It runs on this machine, so the first start downloads the package.",
		AboutRu: "Сообщественный MCP-сервер (не от Точка Банка) для бизнес-API Точки. Он читает счета, остатки и выписки и может подготовить исходящий платёж в рублях, который человек всё равно подписывает в интернет-банке. Работает на этой машине, поэтому первый запуск скачивает пакет.",
		Can: []string{
			"List business accounts with their currency and status",
			"Show the balances of an account",
			"Fetch a statement for a date range",
			"Show the company details for a customer code",
			"Prepare a ruble payment for signing, as a dry-run preview first",
		},
		CanRu: []string{
			"Выведет бизнес-счета с валютой и статусом",
			"Покажет остатки по счёту",
			"Достанет выписку за период",
			"Покажет реквизиты компании по коду клиента",
			"Подготовит рублёвый платёж на подпись, сначала в виде пробного просмотра",
		},
		Steps: []string{
			"In the Tochka internet bank open Integrations and API, create an application and copy its Client ID and Client secret.",
			"Register http://localhost:8765/callback as the redirect address of the application, then run npx -y @theyahia/tochka-bank-mcp auth once: a browser opens, you approve access, and the refresh token is saved.",
			"Paste the Client ID, the Client secret and the Refresh token below, and set the payment limit in rubles: a payment above it is blocked.",
			"Press Connect. The first start downloads the package and takes a minute or two.",
		},
		StepsRu: []string{
			"В интернет-банке Точки откройте «Интеграции и API», создайте приложение и скопируйте его Client ID и Client secret.",
			"Зарегистрируйте http://localhost:8765/callback как адрес возврата приложения и один раз выполните npx -y @theyahia/tochka-bank-mcp auth: откроется браузер, вы подтвердите доступ, и токен обновления сохранится.",
			"Вставьте ниже Client ID, Client secret и Refresh token и задайте лимит платежа в рублях: платёж выше лимита блокируется.",
			"Нажмите «Подключить». Первый запуск скачивает пакет и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/theYahia/tochka-bank-mcp"),
			link(LinkSetup, "https://developers.tochka.com/docs/tochka-api"),
			link(LinkHome, "https://tochka.com/"),
		},
	},

	"amocrm": {
		About:   "amoCRM is a CRM for sales: deals, contacts, companies and tasks in pipelines. Connected with a long-lived token, the agent reads and edits them through the amoCRM API of your account. Changes go through only with your yes.",
		AboutRu: "amoCRM — CRM для продаж: сделки, контакты, компании и задачи в воронках. Подключённый по долгосрочному токену, агент читает и правит их через API amoCRM вашего аккаунта. Изменения вносятся только с вашего согласия.",
		Can: []string{
			"Find deals with their contacts by a query and open one by its ID",
			"Search contacts and companies",
			"List open tasks",
			"Show the sales pipelines and their stages",
			"Read the event log, and move a stage or add a note after your yes",
		},
		CanRu: []string{
			"Найдёт сделки вместе с контактами по запросу и откроет одну по ID",
			"Поищет контакты и компании",
			"Выведет открытые задачи",
			"Покажет воронки продаж и их этапы",
			"Прочитает журнал событий, а после вашего согласия переведёт этап или добавит примечание",
		},
		Steps: []string{
			"Take the subdomain from the address of your account: for company.amocrm.ru it is company.",
			"In amoCRM open the integrations section, create an integration and, under Keys and access, make a long-lived token.",
			"Paste the subdomain and the token below and press Connect.",
		},
		StepsRu: []string{
			"Возьмите поддомен из адреса аккаунта: для company.amocrm.ru это company.",
			"В amoCRM откройте раздел интеграций, создайте интеграцию и во вкладке «Ключи и доступы» сделайте долгосрочный токен.",
			"Вставьте поддомен и токен ниже и нажмите «Подключить».",
		},
		Links: []Link{
			link(LinkDocs, "https://www.amocrm.ru/developers/content/crm_platform/api-reference"),
			link(LinkSetup, "https://www.amocrm.ru/developers/content/oauth/step-by-step"),
			link(LinkHome, "https://www.amocrm.ru/"),
		},
	},

	"bitrix24": {
		About:   "Bitrix24 is a CRM and workspace with deals, contacts, tasks and chats. Connected through an incoming webhook, the agent calls its REST methods on your portal with the rights you gave the webhook. Writing, such as sending a chat message, only with your yes.",
		AboutRu: "Битрикс24 — CRM и рабочее пространство со сделками, контактами, задачами и чатами. Подключённый через входящий вебхук, агент вызывает REST-методы вашего портала с теми правами, что вы дали вебхуку. Запись, например отправка сообщения в чат, только с вашего согласия.",
		Can: []string{
			"List deals by stage and other filters",
			"Find contacts and open one by its ID",
			"List tasks and read one",
			"Show the profile the webhook works as",
			"Send a chat message after your yes",
		},
		CanRu: []string{
			"Выведет сделки по стадии и другим фильтрам",
			"Найдёт контакты и откроет один по ID",
			"Выведет задачи и прочитает одну",
			"Покажет профиль, от имени которого работает вебхук",
			"Отправит сообщение в чат после вашего согласия",
		},
		Steps: []string{
			"In Bitrix24 open Developer resources, then Other, then Incoming webhook, and create a webhook with the rights the agent needs.",
			"Copy its address: it looks like https://example.bitrix24.com/rest/1/abcdef0123456789/.",
			"Paste the address below and press Connect.",
		},
		StepsRu: []string{
			"В Битрикс24 откройте «Разработчикам», затем «Другое» и «Входящий вебхук» и создайте вебхук с теми правами, что нужны агенту.",
			"Скопируйте его адрес: он выглядит как https://example.bitrix24.ru/rest/1/abcdef0123456789/.",
			"Вставьте адрес ниже и нажмите «Подключить».",
		},
		Links: []Link{
			link(LinkDocs, "https://apidocs.bitrix24.ru/"),
			link(LinkSetup, "https://helpdesk.bitrix24.ru/open/5408147/"),
			link(LinkHome, "https://www.bitrix24.ru/"),
			link(LinkPricing, "https://www.bitrix24.ru/prices/"),
		},
	},
}
