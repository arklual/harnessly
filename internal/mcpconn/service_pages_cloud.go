package mcpconn

// guidesCloud: pages of the cloud and database platforms.
// cloudWithToken adds the page that makes the key to a page built by a shared helper, unless it is there already.
func cloudWithToken(g guide, url string) guide {
	for _, l := range g.Links {
		if l.URL == url {
			return g
		}
	}
	g.Links = append(g.Links, link(LinkToken, url))
	return g
}

var guidesCloud = map[string]guide{
	"clickhouse-cloud": {
		About:   "ClickHouse Cloud is a managed ClickHouse database for analytics. Its hosted MCP server lets the agent look at your services and ask them questions in SQL; every tool it exposes is read-only. MCP has to be switched on for each service in the console first.",
		AboutRu: "ClickHouse Cloud — управляемая база ClickHouse для аналитики. Её облачный MCP-сервер даёт агенту посмотреть на ваши сервисы и задавать им вопросы на SQL; все инструменты сервера только читают. MCP надо заранее включить для каждого сервиса в консоли.",
		Can: []string{
			"List the databases of a service and the tables in them with their columns",
			"Run a read-only SELECT query on a ClickHouse service",
			"Show services, backups, ClickPipes and billing data of the organization",
			"Run a read-only query on a Managed Postgres service and list its slow query patterns",
		},
		CanRu: []string{
			"Покажет базы сервиса и таблицы в них вместе со столбцами",
			"Выполнит запрос SELECT на чтение в сервисе ClickHouse",
			"Покажет сервисы, резервные копии, ClickPipes и данные по оплате организации",
			"Выполнит запрос на чтение в сервисе Managed Postgres и покажет его медленные запросы",
		},
		Steps: []string{
			"In the ClickHouse Cloud console open your service, press Connect, choose MCP and enable it (this needs the permission to manage MCP for the service).",
			"Press \"Sign in with ClickHouse Cloud\": its page opens in a new window.",
			"Sign in to ClickHouse Cloud and allow access. The agent sees only the organizations and services your account can open.",
		},
		StepsRu: []string{
			"В консоли ClickHouse Cloud откройте сервис, нажмите Connect, выберите MCP и включите его (нужно право управлять MCP для сервиса).",
			"Нажмите «Войти через ClickHouse Cloud»: откроется страница сервиса в новом окне.",
			"Войдите в ClickHouse Cloud и разрешите доступ. Агент увидит только те организации и сервисы, которые доступны вашему аккаунту.",
		},
		Links: []Link{
			link(LinkDocs, "https://clickhouse.com/docs/cloud/features/ai-ml/remote-mcp"),
			link(LinkHome, "https://clickhouse.com/cloud"),
			link(LinkPricing, "https://clickhouse.com/pricing"),
		},
	},
	"neon": {
		About:   "Neon is serverless Postgres with instant database branches. Through its MCP server the agent manages projects and branches, runs SQL and prepares schema migrations. The server can change and delete databases, so look at what the agent asks to do.",
		AboutRu: "Neon — бессерверный Postgres с мгновенными ветками базы. Через MCP-сервер агент управляет проектами и ветками, выполняет SQL и готовит миграции схемы. Сервер умеет менять и удалять базы, поэтому смотрите, что агент собирается сделать.",
		Can: []string{
			"List projects and branches and create or delete a branch",
			"Run an SQL query or a whole transaction on a branch",
			"Read the tables and the schema of a database and compare the schemas of two branches",
			"Prepare a schema migration on a temporary branch and apply it after you confirm",
			"Find slow queries and suggest how to tune them",
		},
		CanRu: []string{
			"Покажет проекты и ветки, создаст или удалит ветку",
			"Выполнит SQL-запрос или целую транзакцию на ветке",
			"Прочитает таблицы и схему базы и сравнит схемы двух веток",
			"Подготовит миграцию схемы на временной ветке и применит её после вашего подтверждения",
			"Найдёт медленные запросы и подскажет, как их ускорить",
		},
		Steps: []string{
			"Press \"Sign in with Neon\": its page opens in a new window.",
			"Sign in to your Neon account and allow access for the agent.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Neon»: откроется страница сервиса в новом окне.",
			"Войдите в аккаунт Neon и разрешите доступ агенту.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://neon.com/docs/ai/neon-mcp-server"),
			link(LinkPricing, "https://neon.com/pricing"),
			link(LinkHome, "https://neon.com/"),
		},
	},
	"prisma": {
		About:   "Prisma Postgres is a managed Postgres database from the Prisma team; the same server also covers Prisma Compute apps and Object Storage buckets. The agent works inside the workspace you choose when you sign in. Tools that change or delete resources can cause data loss.",
		AboutRu: "Prisma Postgres — управляемая база Postgres от команды Prisma; тот же сервер охватывает приложения Prisma Compute и бакеты Object Storage. Агент работает в рабочей области, которую вы выберете при входе. Инструменты, которые меняют или удаляют ресурсы, могут привести к потере данных.",
		Can: []string{
			"List, create and delete Prisma Postgres databases and their connection strings",
			"Read the schema of a database, run SQL and apply a schema update",
			"List the backups of a database and restore one into a new database",
			"Show builds, deployments and runtime logs of Prisma Compute apps, promote or roll back a deployment",
			"Search the Prisma documentation and give an answer with links",
		},
		CanRu: []string{
			"Покажет, создаст и удалит базы Prisma Postgres и их строки подключения",
			"Прочитает схему базы, выполнит SQL и применит обновление схемы",
			"Покажет резервные копии базы и восстановит одну из них в новую базу",
			"Покажет сборки, деплои и логи работы приложений Prisma Compute, переключит или откатит деплой",
			"Найдёт ответ в документации Prisma и приведёт ссылки",
		},
		Steps: []string{
			"Press \"Sign in with Prisma\": its page opens in a new window.",
			"Sign in with your Prisma account, select a workspace and approve access.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Prisma»: откроется страница сервиса в новом окне.",
			"Войдите в аккаунт Prisma, выберите рабочую область и подтвердите доступ.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://www.prisma.io/docs/postgres/integrations/mcp-server"),
			link(LinkSetup, "https://www.prisma.io/docs/ai/mcp-tools"),
			link(LinkPricing, "https://www.prisma.io/pricing"),
			link(LinkHome, "https://www.prisma.io/postgres"),
		},
	},
	"railway": {
		About:   "Railway is a platform where you deploy apps and databases from a repository or a template. Its MCP server lets the agent look through your projects and services and redeploy them, and hand a longer task to Railway's own agent. Starting a cloud agent task is billed.",
		AboutRu: "Railway — платформа, где приложения и базы разворачивают из репозитория или шаблона. MCP-сервер позволяет агенту просмотреть проекты и сервисы, перезапустить деплой и передать длинную задачу встроенному агенту Railway. Запуск задачи облачного агента тарифицируется.",
		Can: []string{
			"Show who you are signed in as, list projects and the services inside them",
			"Create a project",
			"Redeploy a service or commit staged changes and deploy them",
			"Hand a multi-step job, such as analysing logs or fixing a service setup, to the Railway agent",
			"Read and change feature flags and manage automations",
		},
		CanRu: []string{
			"Покажет, под кем вы вошли, список проектов и сервисов в них",
			"Создаст проект",
			"Перезапустит деплой сервиса или применит накопленные изменения и задеплоит их",
			"Передаст агенту Railway многошаговую работу, например разбор логов или правку настроек сервиса",
			"Прочитает и изменит feature flags, управляет автоматизациями",
		},
		Steps: []string{
			"Press \"Sign in with Railway\": its page opens in a new window.",
			"Sign in to your Railway account and allow access to the workspace.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Railway»: откроется страница сервиса в новом окне.",
			"Войдите в аккаунт Railway и разрешите доступ к рабочему пространству.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.railway.com/ai/mcp-server"),
			link(LinkPricing, "https://railway.com/pricing"),
			link(LinkHome, "https://railway.com/"),
		},
	},
	"zapier": {
		About:   "Zapier connects thousands of apps. Through its MCP server the agent runs actions in the apps you have already connected to Zapier: sends a message, adds a row, creates a task. Zapier holds the app credentials. Every successful call spends 2 tasks of your Zapier plan.",
		AboutRu: "Zapier соединяет тысячи приложений. Через его MCP-сервер агент выполняет действия в приложениях, которые вы уже подключили к Zapier: отправляет сообщение, добавляет строку, создаёт задачу. Доступы к приложениям хранит Zapier. Каждый успешный вызов тратит 2 задачи (tasks) вашего тарифа Zapier.",
		Can: []string{
			"Find the right action in a connected app during the conversation and add it",
			"Send a message in Slack or an email in Gmail",
			"Add and look up rows in Google Sheets, find events in Google Calendar",
			"Create and update tasks in Jira and similar trackers",
			"Combine several apps in one request, for example find a contact in a CRM and email them",
		},
		CanRu: []string{
			"Найдёт нужное действие в подключённом приложении прямо в разговоре и добавит его",
			"Отправит сообщение в Slack или письмо в Gmail",
			"Добавит и найдёт строки в Google Sheets, найдёт события в Google Calendar",
			"Создаст и обновит задачи в Jira и похожих трекерах",
			"Соединит несколько приложений в одном запросе, например найдёт контакт в CRM и напишет ему",
		},
		Steps: []string{
			"Press \"Sign in with Zapier\": its page opens in a new window.",
			"Sign in to Zapier and allow access. Zapier connects the agent to your default MCP server and creates one if you have none.",
			"In Zapier connect the apps the agent should work with, then ask for an action: it picks the tool itself.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Zapier»: откроется страница сервиса в новом окне.",
			"Войдите в Zapier и разрешите доступ. Zapier подключит агента к вашему серверу MCP по умолчанию и создаст его, если его ещё нет.",
			"Подключите в Zapier приложения, с которыми агент должен работать, и просите действие: инструмент он выберет сам.",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.zapier.com/mcp/home"),
			link(LinkSetup, "https://docs.zapier.com/mcp/get-started/quickstart"),
			link(LinkPricing, "https://zapier.com/pricing"),
			link(LinkHome, "https://zapier.com/mcp"),
		},
	},
	"heroku": {
		About:   "Heroku is a platform for running apps in dynos with add-ons such as Postgres. Its hosted MCP server connects the agent to the apps of your Heroku account through OAuth, with no key to copy. Heroku calls the MCP server work in progress, so tools may change.",
		AboutRu: "Heroku — платформа для запуска приложений в dyno с дополнениями вроде Postgres. Облачный MCP-сервер подключает агента к приложениям вашего аккаунта Heroku через OAuth, ключ копировать не нужно. Heroku называет MCP-сервер развивающимся, поэтому набор инструментов может меняться.",
		Can: []string{
			"List your apps and show the configuration, dynos and add-ons of one app",
			"Read application logs",
			"Scale, resize or restart dynos",
			"List add-ons and attach a new one to an app",
			"Look at Heroku Postgres: run SQL and see active queries and backups",
		},
		CanRu: []string{
			"Покажет список приложений и настройки, dyno и дополнения одного приложения",
			"Прочитает логи приложения",
			"Изменит число или размер dyno и перезапустит их",
			"Покажет дополнения и подключит новое к приложению",
			"Заглянет в Heroku Postgres: выполнит SQL, покажет активные запросы и резервные копии",
		},
		Steps: []string{
			"Press \"Sign in with Heroku\": its page opens in a new window.",
			"Sign in to your Heroku account and allow access for the agent.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Heroku»: откроется страница сервиса в новом окне.",
			"Войдите в аккаунт Heroku и разрешите доступ агенту.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://devcenter.heroku.com/articles/heroku-remote-mcp-server"),
			link(LinkPricing, "https://www.heroku.com/pricing"),
			link(LinkHome, "https://www.heroku.com/"),
		},
	},
	"instantdb": {
		About:   "InstantDB is a realtime database for web and mobile apps with a client-side query language. The MCP server lets the agent manage your Instant apps: read and push the schema and permission rules, and query or change data.",
		AboutRu: "InstantDB — база данных реального времени для веб- и мобильных приложений с языком запросов на стороне клиента. MCP-сервер позволяет агенту управлять вашими приложениями Instant: читать и применять схему и правила доступа, запрашивать и менять данные.",
		Can: []string{
			"Read the schema and the permission rules of an app",
			"Push a new schema or new permission rules to an app",
			"Run an InstaQL query and return the result as JSON",
			"Create, update, link and delete records in one transaction",
		},
		CanRu: []string{
			"Прочитает схему и правила доступа приложения",
			"Применит новую схему или новые правила доступа к приложению",
			"Выполнит запрос InstaQL и вернёт результат в JSON",
			"Создаст, обновит, свяжет и удалит записи одной транзакцией",
		},
		Steps: []string{
			"Press \"Sign in with InstantDB\": its page opens in a new window.",
			"Sign in to your Instant account and allow access to your apps.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через InstantDB»: откроется страница сервиса в новом окне.",
			"Войдите в аккаунт Instant и разрешите доступ к вашим приложениям.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://www.instantdb.com/docs/using-llms"),
			link(LinkPricing, "https://www.instantdb.com/pricing"),
			link(LinkHome, "https://www.instantdb.com/"),
		},
	},
	"motherduck": {
		About:   "MotherDuck is a cloud service for DuckDB: your databases live there and answer SQL. Its hosted MCP server lets the agent explore them and run queries; it can also change data, and you can limit it to read-only access in the MotherDuck settings.",
		AboutRu: "MotherDuck — облачный сервис на базе DuckDB: ваши базы хранятся там и отвечают на SQL. Облачный MCP-сервер позволяет агенту изучать их и выполнять запросы; он умеет и менять данные, а доступ можно ограничить режимом «только чтение» в настройках MotherDuck.",
		Can: []string{
			"Explore databases, schemas, tables and columns",
			"Run read-only or read-write DuckDB SQL",
			"Attach and detach shared databases",
			"Create and manage Dives (interactive data visualizations)",
			"Schedule and monitor Flights (Python jobs on MotherDuck compute) and answer questions about DuckDB and MotherDuck docs",
		},
		CanRu: []string{
			"Изучит базы, схемы, таблицы и столбцы",
			"Выполнит SQL DuckDB на чтение или на запись",
			"Подключит и отключит общие базы (shares)",
			"Создаст Dives (интерактивные визуализации данных) и будет ими управлять",
			"Настроит и отследит Flights (Python-задания на вычислениях MotherDuck) и ответит по документации DuckDB и MotherDuck",
		},
		Steps: []string{
			"Press \"Sign in with MotherDuck\": its page opens in a new window.",
			"Sign in to your MotherDuck account and allow access for the agent.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через MotherDuck»: откроется страница сервиса в новом окне.",
			"Войдите в аккаунт MotherDuck и разрешите доступ агенту.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://motherduck.com/docs/sql-reference/mcp/"),
			link(LinkPricing, "https://motherduck.com/product/pricing/"),
			link(LinkHome, "https://motherduck.com/"),
		},
	},
	"planetscale": {
		About:   "PlanetScale hosts MySQL (Vitess) and Postgres databases with branches and query insights. Its MCP server gives the agent your organizations, databases, branches, schema and performance data. At sign-in you choose whether it may only read or also write.",
		AboutRu: "PlanetScale хостит базы MySQL (Vitess) и Postgres с ветками и аналитикой запросов. MCP-сервер даёт агенту ваши организации, базы, ветки, схему и данные о производительности. При входе вы выбираете, может ли он только читать или ещё и писать.",
		Can: []string{
			"List organizations, databases and branches and show the schema of a branch",
			"Run a read-only query (SELECT, SHOW, EXPLAIN) on a branch",
			"Run a write query (INSERT, UPDATE, DELETE or DDL) if you gave write access",
			"Show query insights, schema recommendations, failing queries and branch metrics",
			"List invoices and search the PlanetScale documentation",
		},
		CanRu: []string{
			"Покажет организации, базы и ветки и схему ветки",
			"Выполнит запрос на чтение (SELECT, SHOW, EXPLAIN) на ветке",
			"Выполнит запрос на запись (INSERT, UPDATE, DELETE или DDL), если вы дали право на запись",
			"Покажет аналитику запросов, рекомендации по схеме, упавшие запросы и метрики ветки",
			"Покажет счета и найдёт ответ в документации PlanetScale",
		},
		Steps: []string{
			"Press \"Sign in with PlanetScale\": its page opens in a new window.",
			"Sign in to PlanetScale, pick the organizations and databases and choose no access, read-only or full access.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через PlanetScale»: откроется страница сервиса в новом окне.",
			"Войдите в PlanetScale, выберите организации и базы и укажите уровень доступа: без доступа, только чтение или полный.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://planetscale.com/docs/connect/mcp"),
			link(LinkPricing, "https://planetscale.com/pricing"),
			link(LinkHome, "https://planetscale.com/"),
		},
	},
	"appwrite": {
		About:   "Appwrite is an open-source backend: auth, databases, storage, functions and messaging. The hosted MCP server connects the agent to your Appwrite Cloud project and to the Appwrite documentation. It needs an Appwrite Cloud project and signs in with OAuth.",
		AboutRu: "Appwrite — бэкенд с открытым кодом: авторизация, базы данных, хранилище, функции и рассылки. Облачный MCP-сервер подключает агента к вашему проекту в Appwrite Cloud и к документации Appwrite. Нужен проект в Appwrite Cloud, вход идёт через OAuth.",
		Can: []string{
			"Show a summary of your account, organization and projects",
			"Find the needed API call in the catalog of Appwrite tools and run read operations such as listing users or files",
			"Change the project (create, update or delete resources) with a stated reason shown in the project activity log",
			"Search the Appwrite documentation",
		},
		CanRu: []string{
			"Покажет сводку по аккаунту, организации и проектам",
			"Найдёт нужный вызов в каталоге инструментов Appwrite и выполнит чтение, например список пользователей или файлов",
			"Изменит проект (создаст, обновит или удалит ресурсы), указав причину, которая видна в журнале активности проекта",
			"Найдёт ответ в документации Appwrite",
		},
		Steps: []string{
			"Make sure you have a project in Appwrite Cloud.",
			"Press \"Sign in with Appwrite\": its page opens in a new window.",
			"Sign in to your Appwrite account and authorize access. There are no API keys to create.",
		},
		StepsRu: []string{
			"Убедитесь, что у вас есть проект в Appwrite Cloud.",
			"Нажмите «Войти через Appwrite»: откроется страница сервиса в новом окне.",
			"Войдите в аккаунт Appwrite и подтвердите доступ. Создавать API-ключи не нужно.",
		},
		Links: []Link{
			link(LinkDocs, "https://appwrite.io/docs/tooling/mcp"),
			link(LinkPricing, "https://appwrite.io/pricing"),
			link(LinkHome, "https://appwrite.io/"),
		},
	},
	"digitalocean": {
		About:   "DigitalOcean is a cloud for droplets, App Platform, managed databases and Kubernetes. The hosted MCP Router is one endpoint in front of all DigitalOcean MCP services: the agent finds a tool by its description and runs it. Resources you create are billed by DigitalOcean.",
		AboutRu: "DigitalOcean — облако с дроплетами, App Platform, управляемыми базами и Kubernetes. Облачный MCP Router — это одна точка входа ко всем MCP-сервисам DigitalOcean: агент находит инструмент по описанию и запускает его. Созданные ресурсы оплачиваются в DigitalOcean.",
		Can: []string{
			"Find a suitable tool across all DigitalOcean services by a plain description",
			"Show the input schema of a tool before calling it",
			"Run a tool of any service: droplets, apps, databases, Kubernetes, networking, Spaces and others",
		},
		CanRu: []string{
			"Найдёт подходящий инструмент среди всех сервисов DigitalOcean по обычному описанию",
			"Покажет схему входных данных инструмента до вызова",
			"Запустит инструмент любого сервиса: дроплеты, приложения, базы, Kubernetes, сеть, Spaces и другие",
		},
		Steps: []string{
			"Press \"Sign in with DigitalOcean\": its page opens in a new window.",
			"Sign in to your DigitalOcean account and allow access for the agent.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через DigitalOcean»: откроется страница сервиса в новом окне.",
			"Войдите в аккаунт DigitalOcean и разрешите доступ агенту.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.digitalocean.com/reference/mcp/"),
			link(LinkSetup, "https://github.com/digitalocean-labs/mcp-digitalocean"),
			link(LinkPricing, "https://www.digitalocean.com/pricing"),
			link(LinkHome, "https://www.digitalocean.com/"),
		},
	},
	"make": {
		About:   "Make is a visual builder of automation scenarios. Its hosted MCP server lets the agent run your scenarios as tools and, on paid plans, edit them. At sign-in you choose the scopes that decide which tools the agent gets.",
		AboutRu: "Make — визуальный конструктор сценариев автоматизации. Облачный MCP-сервер позволяет агенту запускать ваши сценарии как инструменты, а на платных тарифах и редактировать их. При входе вы выбираете права (scopes), которые определяют, какие инструменты получит агент.",
		Can: []string{
			"Run your active and on-demand scenarios and return the result",
			"View and edit scenarios and the connections, webhooks and data stores they use",
			"View and change teams and organizations",
		},
		CanRu: []string{
			"Запустит ваши активные и вызываемые по требованию сценарии и вернёт результат",
			"Покажет и изменит сценарии, а также соединения, вебхуки и хранилища данных, которые они используют",
			"Покажет и изменит команды и организации",
		},
		Steps: []string{
			"Press \"Sign in with Make\": its page opens in a new window.",
			"Sign in to Make, choose the scopes for the agent and allow access. Running scenarios works on every plan, managing them needs a paid plan.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Make»: откроется страница сервиса в новом окне.",
			"Войдите в Make, выберите права для агента и разрешите доступ. Запуск сценариев доступен на любом тарифе, управление ими — на платных.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.make.com/mcp-server"),
			link(LinkPricing, "https://www.make.com/en/pricing"),
			link(LinkHome, "https://www.make.com/"),
		},
	},
	"openrouter": {
		About:   "OpenRouter is one API in front of hundreds of AI models. Its MCP server lets the agent read live data about models, prices, providers and rankings, check your credit balance and send test messages. Test messages and generated images are billed to your credit.",
		AboutRu: "OpenRouter — единый API к сотням ИИ-моделей. MCP-сервер позволяет агенту читать живые данные о моделях, ценах, провайдерах и рейтингах, смотреть остаток кредитов и отправлять тестовые сообщения. Тестовые сообщения и сгенерированные картинки списываются с вашего баланса.",
		Can: []string{
			"Search the live model catalog and show the details, providers and prices of one model",
			"Show daily rankings of models and apps and third-party benchmark scores",
			"Show your remaining credit and the cost and tokens of a past generation",
			"Send a test message to any model or generate an image from a prompt",
			"Search the OpenRouter documentation",
		},
		CanRu: []string{
			"Найдёт модель в живом каталоге и покажет её подробности, провайдеров и цены",
			"Покажет ежедневные рейтинги моделей и приложений и результаты сторонних тестов",
			"Покажет остаток кредитов, стоимость и число токенов прошлой генерации",
			"Отправит тестовое сообщение любой модели или сгенерирует картинку по описанию",
			"Найдёт ответ в документации OpenRouter",
		},
		Steps: []string{
			"Press \"Sign in with OpenRouter\": its consent page opens in a new window.",
			"Sign in to OpenRouter and approve the dedicated key created for this connection; it expires on its own after some days.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через OpenRouter»: откроется страница согласия в новом окне.",
			"Войдите в OpenRouter и подтвердите отдельный ключ, который создаётся для этого подключения; через несколько дней он истечёт сам.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://openrouter.ai/docs/guides/overview/mcp-server"),
			link(LinkPricing, "https://openrouter.ai/pricing"),
			link(LinkHome, "https://openrouter.ai/"),
		},
	},
	"runpod": {
		About:   "Runpod rents GPUs for training and inference: Pods, Serverless endpoints and network volumes. Its hosted MCP server drives the Runpod REST API for the agent. Pods and endpoints that the agent starts are billed to your Runpod balance.",
		AboutRu: "Runpod сдаёт в аренду GPU для обучения и инференса: Pods, Serverless-эндпоинты и сетевые тома. Облачный MCP-сервер управляет REST API Runpod от имени агента. Pods и эндпоинты, которые запустит агент, оплачиваются с вашего баланса Runpod.",
		Can: []string{
			"Create, start, stop and delete Pods",
			"Create and manage Serverless endpoints",
			"Manage templates, network volumes and container registry credentials",
		},
		CanRu: []string{
			"Создаст, запустит, остановит и удалит Pod",
			"Создаст Serverless-эндпоинт и будет им управлять",
			"Управляет шаблонами, сетевыми томами и доступами к реестрам контейнеров",
		},
		Steps: []string{
			"Press \"Sign in with Runpod\": its page opens in a new window.",
			"Sign in to the Runpod console and approve the request.",
			"The window closes by itself and the server shows its tools here.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Runpod»: откроется страница сервиса в новом окне.",
			"Войдите в консоль Runpod и подтвердите запрос.",
			"Окно закроется само, а здесь появятся инструменты сервера.",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.runpod.io/get-started/mcp-servers"),
			link(LinkPricing, "https://www.runpod.io/pricing"),
			link(LinkHome, "https://www.runpod.io/"),
		},
	},
	"vultr": {
		About:   "Vultr rents cloud servers, bare metal, managed databases and Kubernetes. Its hosted MCP server is in public preview and strictly read-only: the agent can look at your infrastructure and billing, but cannot create, change or delete anything and has no tools that spend money.",
		AboutRu: "Vultr сдаёт в аренду облачные серверы, bare metal, управляемые базы и Kubernetes. Облачный MCP-сервер сейчас в публичном превью и работает только на чтение: агент смотрит на инфраструктуру и счета, но не может ничего создать, изменить или удалить, и инструментов, которые тратят деньги, у него нет.",
		Can: []string{
			"Show which instances run in a region and on which plans",
			"Summarize invoices and spending for a month",
			"Check whether a Kubernetes (VKE) cluster has an upgrade available",
			"Look at DNS, firewalls, load balancers, VPCs, managed databases and storage",
			"List plans, regions and operating systems from the catalog",
		},
		CanRu: []string{
			"Покажет, какие серверы работают в регионе и на каких тарифах",
			"Сведёт счета и расходы за месяц",
			"Проверит, есть ли обновление для кластера Kubernetes (VKE)",
			"Посмотрит DNS, файрволы, балансировщики, VPC, управляемые базы и хранилища",
			"Покажет тарифы, регионы и операционные системы из каталога",
		},
		Steps: []string{
			"Ask an administrator of your Vultr organization to enable the MCP server and to attach the \"Use MCP\" permission policy to your user.",
			"In the Vultr Console select the organization you want to work with: the login is tied to the organization chosen when you press Authorize.",
			"Press \"Sign in with Vultr\": its page opens in a new window. Sign in and press Authorize.",
		},
		StepsRu: []string{
			"Попросите администратора вашей организации в Vultr включить MCP-сервер и выдать вам политику доступа «Use MCP».",
			"В консоли Vultr выберите организацию, с которой будете работать: вход привязывается к той, что выбрана в момент нажатия Authorize.",
			"Нажмите «Войти через Vultr»: откроется страница сервиса в новом окне. Войдите и нажмите Authorize.",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.vultr.com/reference/mcp-server/overview"),
			link(LinkSetup, "https://docs.vultr.com/reference/mcp-server/getting-started"),
			link(LinkPricing, "https://www.vultr.com/pricing/"),
			link(LinkHome, "https://www.vultr.com/"),
		},
	},
	"convex": {
		About:   "Convex is a reactive backend with a database and server functions written in TypeScript. This hosted server is a guide for the agent: it gives the commands and step-by-step playbooks for starting a Convex app. It does not read or change data in your deployments; for that Convex has its own local server.",
		AboutRu: "Convex — реактивный бэкенд с базой данных и серверными функциями на TypeScript. Этот облачный сервер служит агенту путеводителем: он выдаёт команды и пошаговые инструкции для старта приложения на Convex. Данные в ваших деплоях он не читает и не меняет; для этого у Convex есть отдельный локальный сервер.",
		Can: []string{
			"Give the scaffold command and the next steps for a new Convex app",
			"Explain how to add Convex to an existing JavaScript or TypeScript project",
			"Give guidance on scaling: indexes, pagination, large tables, realtime queries, storage and multi-tenant apps",
			"Fetch the quickstart runbook from scaffolding to a verified running app",
		},
		CanRu: []string{
			"Выдаст команду создания и следующие шаги для нового приложения на Convex",
			"Объяснит, как добавить Convex в существующий проект на JavaScript или TypeScript",
			"Даст советы по масштабированию: индексы, пагинация, большие таблицы, запросы в реальном времени, хранилище и мультиарендность",
			"Достанет пошаговую инструкцию от создания проекта до проверенного рабочего приложения",
		},
		Steps: []string{
			"Press \"Sign in with Convex\". The server answered without a login when we checked it, so if no window opens, the connection is already ready.",
			"If the Convex page opens, sign in to your Convex account and allow access.",
			"The server shows its tools here. To work with data of a deployment, run the local server of Convex instead.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Convex». При проверке сервер отвечал без входа, поэтому если окно не открылось, подключение уже готово.",
			"Если откроется страница Convex, войдите в аккаунт Convex и разрешите доступ.",
			"Инструменты сервера появятся здесь. Для работы с данными деплоя запустите локальный сервер Convex.",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.convex.dev/ai/convex-mcp-server"),
			link(LinkPricing, "https://www.convex.dev/pricing"),
			link(LinkHome, "https://www.convex.dev/"),
		},
	},
	"yandex-360-admin": cloudWithToken(yandex360Guide(
		"Yandex 360 for Business is the mail, disk and office suite for companies. This server (preview from Yandex) lets the agent manage the structure of the organization on behalf of an administrator: users, groups and departments. The token must belong to an organization administrator.",
		"Яндекс 360 для бизнеса — почта, диск и офис для компаний. Этот сервер (предварительная версия от Яндекса) позволяет агенту работать со структурой организации от имени администратора: сотрудники, группы и подразделения. Токен должен принадлежать администратору организации.",
		[]string{
			"Find an employee by login and show their card",
			"Create a domain employee and update an employee's data",
			"Create a group, add members to it and remove them",
			"Create a department and read the data of the organization",
		},
		[]string{
			"Найдёт сотрудника по логину и покажет его карточку",
			"Создаст доменного сотрудника и обновит данные сотрудника",
			"Создаст группу, добавит в неё участников и удалит их",
			"Создаст подразделение и прочитает данные организации",
		},
		"https://yandex.ru/dev/yandex-360/mcp/ru/admin",
	), "https://oauth.yandex.ru/authorize?client_id=becf154492d64526950174523f55887c&response_type=token"),
	"render": {
		About:   "Render is a cloud for web services, static sites, cron jobs and databases. Its hosted MCP server lets the agent look at your services, read logs and metrics and start deploys. It cannot delete resources or change scaling: for that use the dashboard.",
		AboutRu: "Render — облако для веб-сервисов, статических сайтов, cron-задач и баз данных. Облачный MCP-сервер позволяет агенту смотреть сервисы, читать логи и метрики и запускать деплои. Удалять ресурсы и менять масштабирование он не умеет: для этого есть панель Render.",
		Can: []string{
			"List workspaces and services and show the details of a service",
			"Trigger a deploy and show the deploy history",
			"Read logs by filters and fetch CPU, memory, instance count and response metrics",
			"Update the environment variables of a service",
			"Create web services, static sites, cron jobs, Postgres and Key Value, and run a read-only SQL query on a Render Postgres database",
		},
		CanRu: []string{
			"Покажет рабочие области и сервисы и подробности сервиса",
			"Запустит деплой и покажет историю деплоев",
			"Прочитает логи по фильтрам и достанет метрики: процессор, память, число экземпляров, ответы",
			"Обновит переменные окружения сервиса",
			"Создаст веб-сервисы, статические сайты, cron-задачи, Postgres и Key Value и выполнит SQL-запрос на чтение в базе Render Postgres",
		},
		Steps: []string{
			"In the Render Dashboard open Account Settings, then API Keys, and create a key.",
			"Paste the key in the field below. It goes to the server in the Authorization header.",
			"Press Connect, then tell the agent which workspace to use.",
		},
		StepsRu: []string{
			"В панели Render откройте Account Settings, затем API Keys и создайте ключ.",
			"Вставьте ключ в поле ниже. Он уйдёт на сервер в заголовке Authorization.",
			"Нажмите «Подключить», затем скажите агенту, какую рабочую область использовать.",
		},
		Links: []Link{
			link(LinkDocs, "https://render.com/docs/mcp-server"),
			link(LinkToken, "https://dashboard.render.com/u/settings/api-keys"),
			link(LinkPricing, "https://render.com/pricing"),
			link(LinkHome, "https://render.com/"),
		},
	},
	"aws-knowledge": {
		About:   "A hosted server from AWS with its documentation and guidance. The agent answers from current AWS pages instead of model memory. It needs no AWS account or keys and does not touch your resources.",
		AboutRu: "Облачный сервер AWS с документацией и рекомендациями. Агент отвечает по актуальным страницам AWS, а не по памяти модели. Аккаунт AWS и ключи не нужны, ваши ресурсы сервер не трогает.",
		Can: []string{
			"Search the AWS documentation and return the matching passages of the real pages",
			"Read a full documentation page as Markdown",
			"List AWS regions and check where a service, API or CloudFormation resource is available",
			"Retrieve AWS agent skills: workflows and references for a domain",
		},
		CanRu: []string{
			"Найдёт в документации AWS подходящие фрагменты настоящих страниц",
			"Прочитает страницу документации целиком в виде Markdown",
			"Покажет регионы AWS и проверит, где доступен сервис, API или ресурс CloudFormation",
			"Достанет навыки (skills) AWS для агента: сценарии и справочные материалы по области",
		},
		Steps: []string{
			"No sign-in or key is needed: press Connect.",
			"The server shows its tools here. Ask the agent an AWS question and it searches the documentation itself.",
		},
		StepsRu: []string{
			"Вход и ключ не нужны: нажмите «Подключить».",
			"Инструменты сервера появятся здесь. Задайте агенту вопрос про AWS, и он сам поищет в документации.",
		},
		Links: []Link{
			link(LinkDocs, "https://awslabs.github.io/mcp/servers/aws-knowledge-mcp-server"),
			link(LinkSetup, "https://github.com/awslabs/mcp"),
			link(LinkHome, "https://aws.amazon.com/"),
		},
	},
	"azure": {
		About:   "The official Azure MCP server from Microsoft runs on this machine and works with Azure through a service principal, so the agent gets exactly the access that principal's roles give. It covers more than 45 service areas.",
		AboutRu: "Официальный сервер Azure от Microsoft запускается на этой машине и работает с Azure через сервис-принципал, поэтому агент получает ровно тот доступ, который дают роли принципала. Он охватывает более 45 областей сервисов.",
		Can: []string{
			"Work with storage, Cosmos DB, Key Vault, App Service, Container Apps and container registries",
			"Look at monitoring data and Azure Advisor recommendations",
			"Work with AKS, virtual machines and disks",
			"Manage Microsoft Foundry models and agents and Azure AI Search",
			"Generate Azure CLI commands from a plain request",
		},
		CanRu: []string{
			"Поработает с хранилищами, Cosmos DB, Key Vault, App Service, Container Apps и реестрами контейнеров",
			"Посмотрит данные мониторинга и рекомендации Azure Advisor",
			"Поработает с AKS, виртуальными машинами и дисками",
			"Займётся моделями и агентами Microsoft Foundry и Azure AI Search",
			"Составит команды Azure CLI по обычному запросу",
		},
		Steps: []string{
			"In the Azure portal open Microsoft Entra ID, then App registrations, and register a new application. Copy its Directory (tenant) ID and Application (client) ID.",
			"In the application open Certificates & secrets, create a client secret and copy its value at once: it is shown only once.",
			"On your subscription open Access control (IAM) and give the application a role. Pick the narrowest one, for example Reader: it sets what the agent can do.",
			"Paste the values below (the subscription ID is optional) and press Connect. The first start downloads the server with npx and takes a minute or two.",
		},
		StepsRu: []string{
			"В портале Azure откройте Microsoft Entra ID, затем App registrations и зарегистрируйте новое приложение. Скопируйте его Directory (tenant) ID и Application (client) ID.",
			"В приложении откройте Certificates & secrets, создайте client secret и сразу скопируйте его значение: оно показывается один раз.",
			"В своей подписке откройте Access control (IAM) и назначьте приложению роль. Берите самую узкую, например Reader: она определяет, что сможет агент.",
			"Вставьте значения ниже (ID подписки необязателен) и нажмите «Подключить». Первый запуск скачивает сервер через npx и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://learn.microsoft.com/azure/developer/azure-mcp-server/overview"),
			link(LinkSetup, "https://github.com/microsoft/mcp"),
			link(LinkToken, "https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade"),
			link(LinkPricing, "https://azure.microsoft.com/pricing/"),
			link(LinkHome, "https://azure.microsoft.com/"),
		},
	},
	"yandex-cloud-docs": {
		About:   "The official proxy from Yandex Cloud, connected here to its documentation server. The agent searches the current documentation of Yandex Cloud and answers from it. No account or key is needed, and your cloud resources are not touched.",
		AboutRu: "Официальный прокси Yandex Cloud, здесь подключённый к серверу документации. Агент ищет по актуальной документации Yandex Cloud и отвечает по ней. Аккаунт и ключ не нужны, ваши ресурсы в облаке сервер не затрагивает.",
		Can: []string{
			"Answer a question about a Yandex Cloud service from the official documentation",
			"Search the documentation in real time with generative search, so answers follow the current pages",
			"Work without an account: it has no access to your cloud",
		},
		CanRu: []string{
			"Ответит на вопрос о сервисе Yandex Cloud по официальной документации",
			"Найдёт ответ в документации в реальном времени с помощью генеративного поиска, так что ответы опираются на актуальные страницы",
			"Работает без аккаунта: доступа к вашему облаку у него нет",
		},
		Steps: []string{
			"No sign-in or key is needed: press Connect.",
			"The first start downloads the @yandex-cloud/mcp package with npx (Node.js 18 or newer is required) and takes a minute.",
		},
		StepsRu: []string{
			"Вход и ключ не нужны: нажмите «Подключить».",
			"Первый запуск скачивает пакет @yandex-cloud/mcp через npx (нужен Node.js 18 или новее) и занимает около минуты.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/yandex-cloud/mcp"),
			link(LinkPricing, "https://yandex.cloud/ru/prices"),
			link(LinkHome, "https://yandex.cloud/ru"),
		},
	},
	"yandex-cloud-api": {
		About:   "An unofficial MCP server for the Yandex Cloud API, a small community project, so check what it does before giving it write access. By its README it covers compute, networks, storage, managed databases and Kubernetes. It acts with your IAM token or the key of a service account.",
		AboutRu: "Неофициальный MCP-сервер для API Yandex Cloud, небольшой проект сообщества, поэтому перед выдачей прав на запись проверьте, что он делает. По его README он охватывает вычисления, сети, хранилище, управляемые базы и Kubernetes. Действует с вашим IAM-токеном или ключом сервисного аккаунта.",
		Can: []string{
			"Work with Compute Cloud virtual machines, disks and images",
			"Work with VPC networks and subnets",
			"Work with Object Storage buckets",
			"Work with managed databases (PostgreSQL, MySQL, MongoDB, Redis, ClickHouse) and Managed Kubernetes",
		},
		CanRu: []string{
			"Поработает с виртуальными машинами, дисками и образами Compute Cloud",
			"Поработает с сетями и подсетями VPC",
			"Поработает с бакетами Object Storage",
			"Поработает с управляемыми базами (PostgreSQL, MySQL, MongoDB, Redis, ClickHouse) и Managed Kubernetes",
		},
		Steps: []string{
			"Get a token: run yc iam create-token (it lives 12 hours), or create an API key of a service account in the console. Fill in one of the two.",
			"Copy the ID of the folder and the ID of the cloud from the console, from the pages of the folder and the cloud.",
			"Paste the values below and press Connect. The first start downloads the server with npx and takes a minute or two.",
		},
		StepsRu: []string{
			"Получите токен: выполните yc iam create-token (он живёт 12 часов) или создайте API-ключ сервисного аккаунта в консоли. Заполните что-то одно.",
			"Скопируйте ID каталога и ID облака со страниц каталога и облака в консоли.",
			"Вставьте значения ниже и нажмите «Подключить». Первый запуск скачивает сервер через npx и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://www.npmjs.com/package/yandex-cloud-mcp"),
			link(LinkSetup, "https://yandex.cloud/ru/docs/iam/operations/iam-token/create"),
			link(LinkToken, "https://console.yandex.cloud/"),
			link(LinkPricing, "https://yandex.cloud/ru/prices"),
		},
	},
	"ssh-sftp": {
		About:   "A local MCP server that opens an SSH session to one Linux server and works there for the agent. It is a small third-party project: it blocks the most dangerous commands and asks you before deleting files or rebooting, but give it a user with limited rights.",
		AboutRu: "Локальный MCP-сервер, который открывает SSH-сессию на одном Linux-сервере и работает там за агента. Это небольшой сторонний проект: самые опасные команды он блокирует, а перед удалением файлов и перезагрузкой спрашивает вас, но лучше дать ему пользователя с ограниченными правами.",
		Can: []string{
			"Run a shell command and return its exit code, output and errors separately",
			"Read, write and list files on the server",
			"Show system load and manage processes",
			"Download and upload files over SFTP",
		},
		CanRu: []string{
			"Выполнит команду оболочки и вернёт код завершения, вывод и ошибки по отдельности",
			"Прочитает, запишет и перечислит файлы на сервере",
			"Покажет загрузку системы и поможет управлять процессами",
			"Скачает и загрузит файлы по SFTP",
		},
		Steps: []string{
			"Enter the server address and the user name to log in with.",
			"Choose how to log in: a password, or the path to a private key file on the machine where the server runs. A key is the better choice.",
			"Change the port only if the server does not use 22, then press Connect. The first start downloads the server with uvx and takes a minute or two.",
		},
		StepsRu: []string{
			"Укажите адрес сервера и имя пользователя для входа.",
			"Выберите способ входа: пароль или путь к файлу приватного ключа на машине, где запущен сервер. Ключ лучше.",
			"Порт меняйте, только если сервер слушает не 22, и нажмите «Подключить». Первый запуск скачивает сервер через uvx и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/mytsx/mcp-servers"),
			link(LinkSetup, "https://docs.astral.sh/uv/getting-started/installation/"),
			link(LinkHome, "https://pypi.org/project/mcp-server-ssh/"),
		},
	},
	"hetzner": {
		About:   "A local community MCP server for the Hetzner Cloud API with about 160 tools; each tool says whether it only reads or can destroy. A token gives access to the whole project, so for looking around create a token with Read rights only.",
		AboutRu: "Локальный MCP-сервер сообщества для Hetzner Cloud API примерно со 160 инструментами; каждый сообщает, только читает он или может что-то уничтожить. Токен открывает весь проект, поэтому для просмотра создайте токен только с правом Read.",
		Can: []string{
			"List servers, create, rebuild, reboot and delete them",
			"Manage networks, firewalls, load balancers and volumes",
			"Manage DNS zones, certificates and snapshots",
			"Work with Storage Boxes",
		},
		CanRu: []string{
			"Покажет серверы, создаст, пересоберёт, перезагрузит и удалит их",
			"Управляет сетями, файрволами, балансировщиками и томами",
			"Управляет DNS-зонами, сертификатами и снимками",
			"Работает с Storage Box",
		},
		Steps: []string{
			"In the Hetzner Cloud Console open your project, then Security, then API tokens, and generate a token. Choose Read for look-ups only or Read & Write to change things.",
			"Copy the token at once: it is shown only once.",
			"Paste it below and press Connect. The first start downloads the server with npx and takes a minute or two.",
		},
		StepsRu: []string{
			"В Hetzner Cloud Console откройте проект, затем Security, затем API tokens и создайте токен. Выберите Read для просмотра или Read & Write, если нужно менять.",
			"Сразу скопируйте токен: он показывается один раз.",
			"Вставьте его ниже и нажмите «Подключить». Первый запуск скачивает сервер через npx и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/Xodus-CO/hcloud-mcp"),
			link(LinkSetup, "https://docs.hetzner.com/cloud/api/getting-started/generating-api-token"),
			link(LinkToken, "https://console.hetzner.cloud/"),
			link(LinkPricing, "https://www.hetzner.com/cloud/"),
		},
	},
	"digitalocean-mcp": {
		About:   "The official DigitalOcean MCP server run locally with your personal access token. The agent gets the access rights of that token. For sign-in without a token use the DigitalOcean entry with OAuth.",
		AboutRu: "Официальный MCP-сервер DigitalOcean, запущенный локально с вашим персональным токеном доступа. Агент получает права этого токена. Для входа без токена есть запись DigitalOcean с OAuth.",
		Can: []string{
			"Manage App Platform apps and droplets",
			"Manage managed databases and Kubernetes clusters",
			"Manage networking: domains, firewalls, load balancers and reserved IPs",
			"Manage Spaces access keys",
		},
		CanRu: []string{
			"Управляет приложениями App Platform и дроплетами",
			"Управляет управляемыми базами и кластерами Kubernetes",
			"Управляет сетью: домены, файрволы, балансировщики и зарезервированные IP",
			"Управляет ключами доступа к Spaces",
		},
		Steps: []string{
			"Open the API tokens page of DigitalOcean and press Generate New Token. Choose the scopes and the expiry.",
			"Copy the token at once: it is shown only once.",
			"Paste it below and press Connect. The first start downloads the server with npx and takes a minute or two.",
		},
		StepsRu: []string{
			"Откройте страницу API tokens в DigitalOcean и нажмите Generate New Token. Выберите права (scopes) и срок действия.",
			"Сразу скопируйте токен: он показывается один раз.",
			"Вставьте его ниже и нажмите «Подключить». Первый запуск скачивает сервер через npx и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/digitalocean-labs/mcp-digitalocean"),
			link(LinkToken, "https://cloud.digitalocean.com/account/api/tokens"),
			link(LinkPricing, "https://www.digitalocean.com/pricing"),
			link(LinkHome, "https://www.digitalocean.com/"),
		},
	},
	"aws": {
		About:   "The AWS Labs MCP server that runs AWS CLI commands for the agent with your access keys, on any AWS service. It acts within the IAM permissions of the key. AWS notes that this server is being superseded by the official AWS MCP server.",
		AboutRu: "Сервер AWS Labs, который выполняет за агента команды AWS CLI с вашими ключами доступа, для любого сервиса AWS. Он действует в пределах IAM-прав ключа. AWS отмечает, что этот сервер заменяется официальным MCP-сервером AWS.",
		Can: []string{
			"Run an AWS CLI command with checks and error handling",
			"Suggest the most likely CLI commands for a request in plain words",
			"Reach AWS services and features that appeared after the model's training",
		},
		CanRu: []string{
			"Выполнит команду AWS CLI с проверкой и обработкой ошибок",
			"Предложит наиболее вероятные команды CLI по запросу обычными словами",
			"Доберётся до сервисов и возможностей AWS, появившихся после обучения модели",
		},
		Steps: []string{
			"In the AWS console open IAM, then your user, then Security credentials, and create an access key. Use a user with the narrowest permissions that fit your task.",
			"Copy the Access key ID and the Secret access key at once: the secret is shown only once. For temporary STS keys also copy the Session token.",
			"Enter the default region, for example eu-central-1, paste the values below and press Connect. The first start downloads the server with uvx and takes a minute or two.",
		},
		StepsRu: []string{
			"В консоли AWS откройте IAM, затем своего пользователя, затем Security credentials и создайте ключ доступа. Берите пользователя с самыми узкими правами, которых хватает для задачи.",
			"Сразу скопируйте Access key ID и Secret access key: secret показывается один раз. Для временных ключей STS скопируйте ещё Session token.",
			"Укажите регион по умолчанию, например eu-central-1, вставьте значения ниже и нажмите «Подключить». Первый запуск скачивает сервер через uvx и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/awslabs/mcp"),
			link(LinkSetup, "https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_access-keys.html"),
			link(LinkToken, "https://console.aws.amazon.com/iam/home#/security_credentials"),
			link(LinkPricing, "https://aws.amazon.com/pricing/"),
		},
	},
	"selectel": {
		About:   "Selectel is a Russian cloud and hosting provider. Here the agent calls its API with a static API key: it looks at projects, balance and DNS. Servers of a project need a separate Keystone token, so this entry is for listing and billing. Paid resources are not created without your yes.",
		AboutRu: "Selectel — российский облачный и хостинг-провайдер. Здесь агент обращается к его API по статическому API-ключу: смотрит проекты, баланс и DNS. Для серверов проекта нужен отдельный токен Keystone, поэтому запись подходит для просмотра и биллинга. Платные ресурсы без вашего согласия не создаются.",
		Can: []string{
			"List the projects of the account",
			"Show the balance",
			"List the DNS zones of your domains",
		},
		CanRu: []string{
			"Покажет проекты аккаунта",
			"Покажет баланс",
			"Покажет DNS-зоны ваших доменов",
		},
		Steps: []string{
			"In my.selectel.ru open Profile and settings, then API keys, and create a static key.",
			"Copy the key and paste it in the field below. It goes to the API in the X-Token header.",
			"Press Connect.",
		},
		StepsRu: []string{
			"В my.selectel.ru откройте «Профиль и настройки», затем «API-ключи» и создайте статический ключ.",
			"Скопируйте ключ и вставьте в поле ниже. Он уйдёт в API в заголовке X-Token.",
			"Нажмите «Подключить».",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.selectel.ru/docs/control-panel/"),
			link(LinkToken, "https://my.selectel.ru/profile/apikeys"),
			link(LinkPricing, "https://selectel.ru/prices/"),
			link(LinkHome, "https://selectel.ru/"),
		},
	},
	"hetzner-cloud": {
		About:   "Hetzner Cloud through its plain API: the agent calls it with the token of one project. It looks at servers, volumes, firewalls, load balancers, metrics and prices, and changes things only with your yes.",
		AboutRu: "Hetzner Cloud через обычный API: агент ходит в него с токеном одного проекта. Смотрит серверы, тома, файрволы, балансировщики, метрики и цены, а менять что-либо будет только с вашего согласия.",
		Can: []string{
			"List servers and show one server with its settings",
			"Fetch CPU and other metrics of a server for a period",
			"List volumes, firewalls and load balancers",
			"Show the prices of Hetzner Cloud",
		},
		CanRu: []string{
			"Покажет серверы и один сервер с его настройками",
			"Достанет метрики сервера, например процессор, за период",
			"Покажет тома, файрволы и балансировщики",
			"Покажет цены Hetzner Cloud",
		},
		Steps: []string{
			"In the Hetzner Cloud Console open your project, then Security, then API tokens, and generate a token. Choose Read for look-ups only or Read & Write to change things.",
			"Copy the token at once: it is shown only once. It works for this one project.",
			"Paste it below and press Connect.",
		},
		StepsRu: []string{
			"В Hetzner Cloud Console откройте проект, затем Security, затем API tokens и создайте токен. Выберите Read для просмотра или Read & Write, если нужно менять.",
			"Сразу скопируйте токен: он показывается один раз и действует для этого проекта.",
			"Вставьте его ниже и нажмите «Подключить».",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.hetzner.cloud/"),
			link(LinkSetup, "https://docs.hetzner.com/cloud/api/getting-started/generating-api-token"),
			link(LinkToken, "https://console.hetzner.cloud/"),
			link(LinkPricing, "https://www.hetzner.com/cloud/"),
		},
	},
	"digitalocean-api": {
		About:   "DigitalOcean through its plain API: the agent calls it with your personal access token. It looks at droplets, apps, databases, domains and the balance, and changes things only with your yes.",
		AboutRu: "DigitalOcean через обычный API: агент ходит в него с вашим персональным токеном доступа. Смотрит дроплеты, приложения, базы, домены и баланс, а менять что-либо будет только с вашего согласия.",
		Can: []string{
			"Show the account and the current balance",
			"List droplets",
			"List App Platform apps and managed databases",
			"List domains",
		},
		CanRu: []string{
			"Покажет аккаунт и текущий баланс",
			"Покажет дроплеты",
			"Покажет приложения App Platform и управляемые базы",
			"Покажет домены",
		},
		Steps: []string{
			"Open the API tokens page of DigitalOcean and press Generate New Token. Choose the scopes and the expiry.",
			"Copy the token at once: it is shown only once.",
			"Paste it below and press Connect.",
		},
		StepsRu: []string{
			"Откройте страницу API tokens в DigitalOcean и нажмите Generate New Token. Выберите права (scopes) и срок действия.",
			"Сразу скопируйте токен: он показывается один раз.",
			"Вставьте его ниже и нажмите «Подключить».",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.digitalocean.com/reference/api/"),
			link(LinkToken, "https://cloud.digitalocean.com/account/api/tokens"),
			link(LinkPricing, "https://www.digitalocean.com/pricing"),
			link(LinkHome, "https://www.digitalocean.com/"),
		},
	},
}
