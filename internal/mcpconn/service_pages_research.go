package mcpconn

var guidesResearch = map[string]guide{
	"canva": {
		About:   "Canva is a design editor for posts, presentations, documents and more. Connected through Canva's own server, the agent works with the designs of your account: it finds them, makes new ones from a brief and exports the result. Which features are open depends on your Canva plan.",
		AboutRu: "Canva — редактор для постов, презентаций, документов и другой графики. Подключённый через официальный сервер Canva, агент работает с макетами вашего аккаунта: находит их, делает новые по описанию и выгружает готовое. Какие функции доступны, зависит от вашего тарифа Canva.",
		Can: []string{
			"Find your designs by a keyword or a filter",
			"Read the details of a design and get a link to it",
			"Create a new design from a text brief",
			"Export a design to a file in a format it supports",
		},
		CanRu: []string{
			"Найдёт ваши макеты по слову или фильтру",
			"Прочитает сведения о макете и даст ссылку на него",
			"Создаст новый макет по текстовому описанию",
			"Выгрузит макет в файл в поддерживаемом формате",
		},
		Steps: []string{
			"Press \"Sign in with Canva\": its page opens in a new window.",
			"Sign in to your Canva account and allow the agent to use your designs.",
			"Come back and ask for a design: the agent will pick up the connection.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Canva»: откроется страница сервиса в новом окне.",
			"Войдите в свой аккаунт Canva и разрешите агенту работать с вашими макетами.",
			"Вернитесь и попросите макет: агент подхватит подключение.",
		},
		Links: []Link{
			link(LinkDocs, "https://www.canva.dev/docs/connect/canva-mcp-server-setup/"),
			link(LinkPricing, "https://www.canva.com/pricing/"),
			link(LinkHome, "https://www.canva.com/"),
		},
	},
	"consensus": {
		About:   "Consensus is a search engine over scholarly papers that answers a question from the studies themselves. Connected, the agent finds peer-reviewed papers for a question and cites them. There is a free plan; paid plans return more papers per search and give more usage.",
		AboutRu: "Consensus — поисковик по научным статьям, который отвечает на вопрос по самим исследованиям. Подключённый, агент находит рецензируемые статьи под вопрос и ссылается на них. Есть бесплатный тариф; платные дают больше статей за поиск и больше запросов.",
		Can: []string{
			"Search scholarly papers for a question and cite them",
			"Run a wide search of a hundred papers and more for a literature review, as far as your plan allows",
			"Compare what several studies say about the same claim",
		},
		CanRu: []string{
			"Найдёт научные статьи под вопрос и сошлётся на них",
			"Сделает широкий поиск от сотни статей для обзора литературы в пределах вашего тарифа",
			"Сопоставит, что говорят разные исследования об одном утверждении",
		},
		Steps: []string{
			"Press \"Sign in with Consensus\": its page opens in a new window.",
			"Sign in to your Consensus account (create a free one if you have none) and approve access.",
			"Ask a research question: the agent will search with your account's limits.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Consensus»: откроется страница сервиса в новом окне.",
			"Войдите в аккаунт Consensus (бесплатный можно создать тут же) и подтвердите доступ.",
			"Задайте исследовательский вопрос: агент будет искать в рамках лимитов вашего аккаунта.",
		},
		Links: []Link{
			link(LinkDocs, "https://consensus.app/home/mcp/"),
			link(LinkPricing, "https://consensus.app/pricing"),
			link(LinkHome, "https://consensus.app/"),
		},
	},
	"miro": {
		About:   "Miro is an online whiteboard for diagrams, workshops and planning. Connected through Miro's own server, the agent reads your boards and draws on them. It works on every plan, with a daily cap of tool calls that grows with the plan.",
		AboutRu: "Miro — онлайн-доска для схем, воркшопов и планирования. Подключённый через официальный сервер Miro, агент читает ваши доски и рисует на них. Работает на любом тарифе; в сутки действует лимит вызовов, который растёт с тарифом.",
		Can: []string{
			"Search boards, spaces and the text inside boards",
			"Read a board or an area of it and summarize it",
			"Draw a diagram, a table or a kanban on a new or existing board",
			"List, answer and resolve comments on a board",
			"Share a board with a user and set their role",
		},
		CanRu: []string{
			"Найдёт доски, пространства и текст внутри досок",
			"Прочитает доску или её участок и перескажет",
			"Нарисует схему, таблицу или канбан на новой или готовой доске",
			"Покажет комментарии к доске, ответит на них и закроет",
			"Откроет доску пользователю и задаст его роль",
		},
		Steps: []string{
			"Press \"Sign in with Miro\": its page opens in a new window.",
			"Sign in to Miro, pick the team the agent may work in and approve access.",
			"Ask for a board by its name or link: the agent will find it.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Miro»: откроется страница сервиса в новом окне.",
			"Войдите в Miro, выберите команду, в которой агенту можно работать, и подтвердите доступ.",
			"Попросите доску по названию или ссылке: агент её найдёт.",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.miro.com/docs/miro-mcp"),
			link(LinkSetup, "https://developers.miro.com/docs/connecting-to-miro-mcp"),
			link(LinkPricing, "https://miro.com/pricing/"),
			link(LinkHome, "https://miro.com/"),
		},
	},
	"scite": {
		About:   "Scite is a literature database that shows how a paper is cited: whether later work supports it, disputes it or just mentions it. Connected, the agent searches the full text of papers and checks claims and references against it. The server needs a paid Scite plan or a free trial.",
		AboutRu: "Scite — база научной литературы, которая показывает, как статью цитируют: подтверждают, оспаривают или просто упоминают. Подключённый, агент ищет по полному тексту статей и проверяет по нему утверждения и ссылки. Серверу нужен платный тариф Scite или пробный период.",
		Can: []string{
			"Search papers by their full text and read the passages that answer",
			"Show the evidence for and against a claim, quoted from papers",
			"Check a reference list for made-up, retracted or misattributed citations",
			"Save papers to your Scite collections",
		},
		CanRu: []string{
			"Найдёт статьи по полному тексту и покажет подходящие абзацы",
			"Покажет доводы за и против утверждения с цитатами из статей",
			"Проверит список литературы на выдуманные, отозванные и неверно приписанные ссылки",
			"Сохранит статьи в ваши коллекции Scite",
		},
		Steps: []string{
			"Make sure your Scite account has a paid plan or a free trial: the server does not work without one.",
			"Press \"Sign in with Scite\": its page opens in a new window.",
			"Sign in to Scite and approve access for the agent.",
		},
		StepsRu: []string{
			"Убедитесь, что у вашего аккаунта Scite есть платный тариф или пробный период: без него сервер не работает.",
			"Нажмите «Войти через Scite»: откроется страница сервиса в новом окне.",
			"Войдите в Scite и подтвердите доступ для агента.",
		},
		Links: []Link{
			link(LinkDocs, "https://scite.ai/mcp"),
			link(LinkHome, "https://scite.ai/"),
		},
	},
	"gamma": {
		About:   "Gamma makes presentations, documents, web pages and social posts from a text. Connected through Gamma's own server, the agent turns your brief into a finished piece in your account. Each generation spends credits of your Gamma plan.",
		AboutRu: "Gamma собирает презентации, документы, веб-страницы и посты из текста. Подключённый через официальный сервер Gamma, агент превращает ваше описание в готовую работу в вашем аккаунте. Каждая генерация тратит кредиты вашего тарифа Gamma.",
		Can: []string{
			"Generate a presentation, a document, a web page or a social post from a brief",
			"Fill one of your workspace templates with new content",
			"Edit an existing gamma by an instruction",
			"Export a gamma to a file and list the ones you have",
		},
		CanRu: []string{
			"Сделает презентацию, документ, веб-страницу или пост по описанию",
			"Заполнит новым содержанием один из шаблонов вашего рабочего пространства",
			"Поправит готовую работу в Gamma по вашей инструкции",
			"Выгрузит работу в файл и покажет те, что у вас уже есть",
		},
		Steps: []string{
			"Press \"Sign in with Gamma\": its page opens in a new window.",
			"Sign in to your Gamma account and approve access.",
			"Ask for a piece with its topic and audience: the agent starts the generation and brings back the link.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Gamma»: откроется страница сервиса в новом окне.",
			"Войдите в свой аккаунт Gamma и подтвердите доступ.",
			"Попросите работу, назвав тему и аудиторию: агент запустит генерацию и принесёт ссылку.",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.gamma.app/docs/gamma-mcp-server"),
			link(LinkSetup, "https://developers.gamma.app/mcp/mcp-tools-reference"),
			link(LinkPricing, "https://gamma.app/pricing"),
			link(LinkHome, "https://gamma.app/"),
		},
	},
	"hex": {
		About:   "Hex is a workspace for data notebooks in SQL and Python, with Threads for questions in plain language. Connected, the agent searches your projects and asks questions of your data through Threads. It needs a Hex Team or Enterprise plan and the Explorer role; editing projects needs the Editor role.",
		AboutRu: "Hex — рабочее пространство для блокнотов с данными на SQL и Python, где есть Threads для вопросов обычным языком. Подключённый, агент ищет по вашим проектам и задаёт вопросы к данным через Threads. Нужен тариф Hex Team или Enterprise и роль Explorer; для правки проектов нужна роль Editor.",
		Can: []string{
			"Search the projects of your workspace",
			"Start a Thread with a question about your data and read its answer",
			"Continue a Thread with a follow-up question",
			"Create a project, edit its SQL, Python and Markdown cells and run the notebook (Editor role)",
		},
		CanRu: []string{
			"Найдёт проекты вашего рабочего пространства",
			"Начнёт Thread с вопросом о ваших данных и прочитает ответ",
			"Продолжит Thread уточняющим вопросом",
			"Создаст проект, поправит его ячейки SQL, Python и Markdown и запустит блокнот (роль Editor)",
		},
		Steps: []string{
			"Check that your Hex workspace is on the Team or Enterprise plan and that you have the Explorer role or higher.",
			"Press \"Sign in with Hex\": its page opens in a new window.",
			"Sign in to Hex and approve access for the agent.",
		},
		StepsRu: []string{
			"Проверьте, что ваше рабочее пространство Hex на тарифе Team или Enterprise, а у вас роль Explorer или выше.",
			"Нажмите «Войти через Hex»: откроется страница сервиса в новом окне.",
			"Войдите в Hex и подтвердите доступ для агента.",
		},
		Links: []Link{
			link(LinkDocs, "https://learn.hex.tech/docs/administration/mcp-server"),
			link(LinkPricing, "https://hex.tech/pricing/"),
			link(LinkHome, "https://hex.tech/"),
		},
	},
	"openalex-mcp": {
		About:   "OpenAlex is an open index of scholarly works, authors, journals and institutions. This is OpenAlex's own server, read-only: you sign in with an OpenAlex account and every query runs on that account's free daily budget.",
		AboutRu: "OpenAlex — открытый индекс научных работ, авторов, журналов и организаций. Это собственный сервер OpenAlex, только для чтения: вы входите в аккаунт OpenAlex, и каждый запрос идёт в рамках его бесплатного дневного бюджета.",
		Can: []string{
			"Search papers by keyword or by meaning with filters for year, type, open access and citations",
			"Check up to 25 references at once and find fabricated or garbled ones",
			"Show who cites a paper, what it cites and which works are related",
			"Count and profile works by author, institution, country, topic or funder",
			"Look up an author, an institution, a journal or a funder",
		},
		CanRu: []string{
			"Найдёт работы по ключевым словам или по смыслу с фильтрами по году, типу, открытому доступу и цитированиям",
			"Проверит до 25 ссылок за раз и найдёт выдуманные или искажённые",
			"Покажет, кто цитирует работу, что цитирует она и какие работы близки",
			"Посчитает и опишет работы по авторам, организациям, странам, темам и грантодателям",
			"Найдёт автора, организацию, журнал или грантодателя",
		},
		Steps: []string{
			"Press \"Sign in with OpenAlex\": its page opens in a new window.",
			"Sign in to openalex.org (a free account is enough) and approve the connection.",
			"Ask about the literature: the agent will search on your account's budget.",
		},
		StepsRu: []string{
			"Нажмите «Войти через OpenAlex»: откроется страница сервиса в новом окне.",
			"Войдите на openalex.org (хватит бесплатного аккаунта) и подтвердите подключение.",
			"Спросите о литературе: агент будет искать в рамках бюджета вашего аккаунта.",
		},
		Links: []Link{
			link(LinkDocs, "https://help.openalex.org/access/connector/"),
			link(LinkSetup, "https://github.com/ourresearch/openalex-mcp-server"),
			link(LinkPricing, "https://openalex.org/pricing"),
			link(LinkHome, "https://openalex.org/"),
		},
	},
	"parallel-task": {
		About:   "Parallel is a web research service for agents. This is its Task server (Parallel now calls it legacy and points new users to its newer MCP server, but keeps it running): the agent starts a deep research task or enriches a whole table with data from the web, then fetches the report. Tasks spend your Parallel account's quota.",
		AboutRu: "Parallel — сервис веб-исследований для агентов. Это его сервер Task (в Parallel его называют устаревшим и для новых подключений советуют другой MCP-сервер, но продолжают поддерживать): агент запускает глубокое исследование или дополняет целую таблицу данными из сети, затем забирает отчёт. Задачи расходуют квоту вашего аккаунта Parallel.",
		Can: []string{
			"Start a deep research task on a hard question and get a cited report",
			"Enrich a list of companies, people or other items with fields found on the web",
			"Check the status of a running task and fetch its result as Markdown",
		},
		CanRu: []string{
			"Запустит глубокое исследование по сложному вопросу и вернёт отчёт со ссылками",
			"Дополнит список компаний, людей или других записей полями, найденными в сети",
			"Проверит статус идущей задачи и заберёт результат в Markdown",
		},
		Steps: []string{
			"Press \"Sign in with Parallel\": its page opens in a new window.",
			"Sign in to your Parallel account and approve access. If the sign-in is refused, make an API key at platform.parallel.ai: the vendor's docs describe this server as working with a key.",
			"Ask for a research task: it runs in the background, so ask for the result a little later.",
		},
		StepsRu: []string{
			"Нажмите «Войти через Parallel»: откроется страница сервиса в новом окне.",
			"Войдите в аккаунт Parallel и подтвердите доступ. Если вход не проходит, создайте API-ключ на platform.parallel.ai: в документации вендора этот сервер описан как работающий по ключу.",
			"Поставьте задачу на исследование: она идёт в фоне, поэтому результат просите чуть позже.",
		},
		Links: []Link{
			link(LinkDocs, "https://docs.parallel.ai/integrations/mcp/task-mcp"),
			link(LinkToken, "https://platform.parallel.ai/"),
			link(LinkPricing, "https://parallel.ai/pricing"),
			link(LinkHome, "https://parallel.ai/"),
		},
	},
	"thoughtspot": {
		About:   "ThoughtSpot is an analytics platform where you ask questions of your data in plain language. Connected through ThoughtSpot's Spotter MCP server, the agent asks your models and liveboards questions and builds charts, with your own access rights and row-level security. You need a ThoughtSpot account.",
		AboutRu: "ThoughtSpot — аналитическая платформа, где к данным задают вопросы обычным языком. Подключённый через сервер Spotter MCP, агент задаёт вопросы вашим моделям и лайвбордам и строит графики с вашими правами доступа и защитой на уровне строк. Нужен аккаунт ThoughtSpot.",
		Can: []string{
			"Answer a question about your data in plain language",
			"Build a liveboard or a chart from the answers",
			"Find existing liveboards and answers by name, author, tag or type",
			"Read the data behind an existing answer or liveboard",
		},
		CanRu: []string{
			"Ответит на вопрос о ваших данных обычным языком",
			"Соберёт лайвборд или график из ответов",
			"Найдёт готовые лайвборды и ответы по названию, автору, тегу или типу",
			"Прочитает данные, стоящие за готовым ответом или лайвбордом",
		},
		Steps: []string{
			"Press \"Sign in with ThoughtSpot\": its page opens in a new window.",
			"Sign in to your ThoughtSpot account and approve access: the agent sees only what your user may see.",
			"Ask a question about your data: the agent will pick a model and answer.",
		},
		StepsRu: []string{
			"Нажмите «Войти через ThoughtSpot»: откроется страница сервиса в новом окне.",
			"Войдите в аккаунт ThoughtSpot и подтвердите доступ: агент увидит только то, что доступно вашему пользователю.",
			"Задайте вопрос о данных: агент выберет модель и ответит.",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.thoughtspot.com/docs/mcp-integration"),
			link(LinkSetup, "https://developers.thoughtspot.com/docs/mcp-tool-reference"),
			link(LinkPricing, "https://www.thoughtspot.com/pricing"),
			link(LinkHome, "https://www.thoughtspot.com/"),
		},
	},
	"pubmed-mcp": {
		About:   "PubMed is the biomedical literature database of the US National Library of Medicine. This server is hosted by Anthropic and needs no sign-in: the agent searches PubMed and reads article records. It covers biomedicine and life sciences only, not physics, computer science or social science.",
		AboutRu: "PubMed — база биомедицинской литературы Национальной медицинской библиотеки США. Этот сервер размещён у Anthropic и не требует входа: агент ищет в PubMed и читает карточки статей. Покрыты только биомедицина и науки о жизни, но не физика, информатика или общественные науки.",
		Can: []string{
			"Search articles in PubMed syntax with author, journal, MeSH and date filters",
			"Read the metadata and abstract of an article",
			"Find articles related to a given one",
			"Find an article by its citation details",
			"Convert between PMID, PMCID and DOI and read the full text from PubMed Central",
		},
		CanRu: []string{
			"Найдёт статьи запросом в синтаксисе PubMed с фильтрами по автору, журналу, MeSH и датам",
			"Прочитает метаданные и аннотацию статьи",
			"Найдёт статьи, близкие к заданной",
			"Найдёт статью по библиографическим данным",
			"Переведёт идентификаторы PMID, PMCID и DOI и прочитает полный текст из PubMed Central",
		},
		Steps: []string{
			"Press \"Connect\": the server is open and needs no key or sign-in.",
			"Ask a biomedical question: the agent writes the PubMed query itself.",
		},
		StepsRu: []string{
			"Нажмите «Подключить»: сервер открытый, ключ и вход не нужны.",
			"Задайте биомедицинский вопрос: запрос для PubMed агент составит сам.",
		},
		Links: []Link{
			link(LinkDocs, "https://pubmed.ncbi.nlm.nih.gov/help/"),
			link(LinkHome, "https://pubmed.ncbi.nlm.nih.gov/"),
		},
	},
	"semantic-scholar-mcp": {
		About:   "Semantic Scholar is a free academic search engine of the Allen Institute for AI (Ai2). This server is the open endpoint of Ai2's Asta Scientific Corpus Tool: the agent searches papers, authors and citations. Without an Ai2 key the limits are low.",
		AboutRu: "Semantic Scholar — бесплатный научный поисковик Института искусственного интеллекта Аллена (Ai2). Этот сервер — открытая точка Asta Scientific Corpus Tool от Ai2: агент ищет статьи, авторов и цитирования. Без ключа Ai2 лимиты невысокие.",
		Can: []string{
			"Search papers by relevance or by title",
			"Find text snippets from papers that match a query",
			"Get a paper by DOI, arXiv, PubMed or Semantic Scholar id, with abstract and citations",
			"Show the papers that cite a given paper",
			"Search authors and list their papers",
		},
		CanRu: []string{
			"Найдёт статьи по релевантности или по названию",
			"Найдёт в статьях фрагменты текста, подходящие к запросу",
			"Получит статью по DOI, идентификатору arXiv, PubMed или Semantic Scholar с аннотацией и цитированиями",
			"Покажет статьи, которые ссылаются на данную",
			"Найдёт авторов и перечислит их работы",
		},
		Steps: []string{
			"Press \"Connect\": the server is open and takes no sign-in.",
			"If the agent runs into rate limits, request a key from Ai2 with the form on its Asta page.",
		},
		StepsRu: []string{
			"Нажмите «Подключить»: сервер открытый, вход не нужен.",
			"Если агент упирается в лимиты, запросите ключ у Ai2 через форму на странице Asta.",
		},
		Links: []Link{
			link(LinkDocs, "https://allenai.org/asta/resources/mcp"),
			link(LinkSetup, "https://www.semanticscholar.org/product/api"),
			link(LinkHome, "https://www.semanticscholar.org/"),
		},
	},
	"zotero": {
		About:   "Zotero is a free reference manager that keeps your papers, notes and annotations. This is a community server (zotero-mcp, not made by Zotero) that runs on this machine and reaches your synced library through the Zotero web API with your key.",
		AboutRu: "Zotero — бесплатный менеджер литературы, где лежат ваши статьи, заметки и аннотации. Это серверная программа сообщества (zotero-mcp, её делает не Zotero), которая работает на этой машине и ходит в вашу синхронизированную библиотеку через веб-API Zotero с вашим ключом.",
		Can: []string{
			"Search the library by title, author, tag or full text",
			"Read the metadata, the full text, the notes and the annotations of an item",
			"Add a new item and put it into a collection",
			"Create and change collections and tags",
			"Export a bibliography",
		},
		CanRu: []string{
			"Найдёт записи в библиотеке по названию, автору, тегу или полному тексту",
			"Прочитает метаданные, полный текст, заметки и аннотации записи",
			"Добавит новую запись и положит её в коллекцию",
			"Создаст и изменит коллекции и теги",
			"Выгрузит список литературы",
		},
		Steps: []string{
			"Open the Zotero security settings, create a new private key in the Applications section and allow it access to your library (with write access if the agent should add items).",
			"Copy the key and, from the same page, your numeric userID (the one for use in API calls).",
			"Paste both values below and press Connect. The first start downloads the server and takes a minute or two.",
		},
		StepsRu: []string{
			"Откройте настройки безопасности Zotero, в разделе приложений создайте новый приватный ключ и разрешите ему доступ к библиотеке (с правом записи, если агент должен добавлять записи).",
			"Скопируйте ключ и на той же странице свой числовой userID (тот, что нужен для вызовов API).",
			"Вставьте оба значения ниже и нажмите «Подключить». Первый запуск скачивает сервер и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/54yyyu/zotero-mcp"),
			link(LinkSetup, "https://github.com/54yyyu/zotero-mcp/blob/main/docs/configuration.md"),
			link(LinkToken, "https://www.zotero.org/settings/security"),
			link(LinkHome, "https://www.zotero.org/"),
		},
	},
	"arxiv-mcp": {
		About:   "arXiv is the open archive of preprints in physics, mathematics, computer science and related fields. This is a community server (arxiv-mcp-server, not made by arXiv) that runs on this machine, needs no key and keeps the papers it downloads in a local folder.",
		AboutRu: "arXiv — открытый архив препринтов по физике, математике, информатике и смежным областям. Это серверная программа сообщества (arxiv-mcp-server, её делает не arXiv), которая работает на этой машине, не требует ключа и хранит скачанные статьи в локальной папке.",
		Can: []string{
			"Search arXiv by query, category and date",
			"Fetch the abstract and metadata of a paper by its arXiv id",
			"Download a paper as Markdown and read it by sections or search inside it",
			"Show the references and the citing papers of a paper",
			"Export BibTeX and save topic watches for new papers",
		},
		CanRu: []string{
			"Найдёт препринты по запросу, разделу и дате",
			"Получит аннотацию и метаданные статьи по её номеру arXiv",
			"Скачает статью в Markdown, прочитает по разделам или поищет внутри неё",
			"Покажет, на что ссылается статья и кто цитирует её",
			"Выгрузит BibTeX и сохранит подписки на новые статьи по теме",
		},
		Steps: []string{
			"Press \"Connect\": no key or sign-in is needed.",
			"The first start downloads the server and takes a minute or two; downloaded papers go to the arxiv-papers folder.",
		},
		StepsRu: []string{
			"Нажмите «Подключить»: ключ и вход не нужны.",
			"Первый запуск скачивает сервер и занимает минуту-две; скачанные статьи попадают в папку arxiv-papers.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/blazickjp/arxiv-mcp-server"),
			link(LinkHome, "https://arxiv.org/"),
		},
	},
	"wikipedia-mcp": {
		About:   "A community server (wikipedia-mcp, not made by the Wikimedia Foundation) that runs on this machine and reads Wikipedia in the English edition. It works without a key; a Wikimedia token is optional and lifts the rate limits.",
		AboutRu: "Серверная программа сообщества (wikipedia-mcp, её делает не Фонд Викимедиа), которая работает на этой машине и читает Википедию в английском издании. Ключ не нужен; токен Wikimedia необязателен и снимает ограничения по частоте запросов.",
		Can: []string{
			"Search Wikipedia articles by a query",
			"Read an article in full, or only its summary or one section",
			"List the links of an article and related topics",
			"Give the coordinates of a place article",
			"Summarize an article for a given question and pull out its key facts",
		},
		CanRu: []string{
			"Найдёт статьи Википедии по запросу",
			"Прочитает статью целиком или только краткое содержание или один раздел",
			"Покажет ссылки статьи и связанные темы",
			"Назовёт координаты места по его статье",
			"Изложит статью под заданный вопрос и выделит главные факты",
		},
		Steps: []string{
			"Optional: in the Wikimedia API portal create a personal token for higher rate limits and copy it.",
			"Paste the token below, or leave it empty, and press Connect. The first start downloads the server and takes a minute or two.",
		},
		StepsRu: []string{
			"По желанию: на портале API Wikimedia создайте личный токен для повышенных лимитов и скопируйте его.",
			"Вставьте токен ниже или оставьте поле пустым и нажмите «Подключить». Первый запуск скачивает сервер и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/rudra-ravi/wikipedia-mcp"),
			link(LinkToken, "https://api.wikimedia.org/wiki/Authentication"),
			link(LinkHome, "https://www.wikipedia.org/"),
		},
	},
	"time": {
		About:   "The reference MCP server for time, maintained in the Model Context Protocol servers repository. It runs on this machine, needs no key and gives the agent the current time and time zone arithmetic. Its default zone here is Europe/Moscow.",
		AboutRu: "Эталонный MCP-сервер времени из репозитория серверов Model Context Protocol. Работает на этой машине, ключ не нужен и даёт агенту текущее время и расчёты по часовым поясам. Часовой пояс по умолчанию здесь — Europe/Moscow.",
		Can: []string{
			"Tell the current time in any time zone",
			"Convert a time from one zone to another",
			"Work out the time a given number of days, hours or minutes ago",
		},
		CanRu: []string{
			"Назовёт текущее время в любом часовом поясе",
			"Переведёт время из одного пояса в другой",
			"Посчитает, сколько было времени столько-то дней, часов или минут назад",
		},
		Steps: []string{
			"Press \"Connect\": no key or sign-in is needed.",
			"The first start downloads the server and takes a minute or two.",
		},
		StepsRu: []string{
			"Нажмите «Подключить»: ключ и вход не нужны.",
			"Первый запуск скачивает сервер и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/modelcontextprotocol/servers/tree/main/src/time"),
			link(LinkHome, "https://github.com/modelcontextprotocol/servers"),
		},
	},
	"fetch": {
		About:   "The reference MCP server for web pages, maintained in the Model Context Protocol servers repository. It runs on this machine, needs no key and lets the agent open an address and read the page as Markdown. By default it follows the site's robots.txt rules.",
		AboutRu: "Эталонный MCP-сервер для веб-страниц из репозитория серверов Model Context Protocol. Работает на этой машине, ключ не нужен и позволяет агенту открыть адрес и прочитать страницу как Markdown. По умолчанию он соблюдает правила robots.txt сайта.",
		Can: []string{
			"Download a page by its address and turn it into Markdown",
			"Read a long page in parts, starting from a given character",
			"Return the raw content of an address without conversion",
		},
		CanRu: []string{
			"Загрузит страницу по адресу и превратит её в Markdown",
			"Прочитает длинную страницу частями, начиная с нужного символа",
			"Вернёт исходное содержимое адреса без преобразования",
		},
		Steps: []string{
			"Press \"Connect\": no key or sign-in is needed.",
			"The first start downloads the server and takes a minute or two.",
		},
		StepsRu: []string{
			"Нажмите «Подключить»: ключ и вход не нужны.",
			"Первый запуск скачивает сервер и занимает минуту-две.",
		},
		Links: []Link{
			link(LinkDocs, "https://github.com/modelcontextprotocol/servers/tree/main/src/fetch"),
			link(LinkHome, "https://github.com/modelcontextprotocol/servers"),
		},
	},
	"figma-api": {
		About:   "Figma's REST API read with your personal access token: the agent reads files and exports pictures from them. It sees the files your Figma account sees. The Figma MCP server of the catalog is a separate entry.",
		AboutRu: "REST API Figma с вашим персональным токеном доступа: агент читает файлы и выгружает из них картинки. Он видит те файлы, что видит ваш аккаунт Figma. MCP-сервер Figma в каталоге — отдельная запись.",
		Can: []string{
			"Read the tree of a file and any of its nodes by id",
			"Export frames and nodes as PNG, JPG, SVG or PDF",
			"Read the comments of a file",
			"List the styles and components of a file",
			"List the projects of a team",
		},
		CanRu: []string{
			"Прочитает дерево файла и любые его узлы по идентификатору",
			"Выгрузит фреймы и узлы в PNG, JPG, SVG или PDF",
			"Прочитает комментарии к файлу",
			"Покажет стили и компоненты файла",
			"Перечислит проекты команды",
		},
		Steps: []string{
			"In Figma open Settings, then Security, and under Personal access tokens press \"Generate new token\".",
			"Give it a name and the read scopes you need (for example file content), then copy the token: it is shown once.",
			"Paste it below and press Connect.",
		},
		StepsRu: []string{
			"В Figma откройте «Settings», затем «Security» и в разделе Personal access tokens нажмите «Generate new token».",
			"Дайте токену имя и нужные права на чтение (например, на содержимое файлов), затем скопируйте его: токен показывают один раз.",
			"Вставьте его ниже и нажмите «Подключить».",
		},
		Links: []Link{
			link(LinkDocs, "https://developers.figma.com/docs/rest-api/"),
			link(LinkSetup, "https://developers.figma.com/docs/rest-api/authentication/"),
			link(LinkToken, "https://www.figma.com/settings"),
			link(LinkPricing, "https://www.figma.com/pricing/"),
		},
	},
	"openalex": {
		About:   "OpenAlex is an open index of scholarly works, authors, journals and institutions. The agent calls its plain API. Without a key all requests from your network share one small free daily budget, so heavy use needs a free OpenAlex key.",
		AboutRu: "OpenAlex — открытый индекс научных работ, авторов, журналов и организаций. Агент обращается к его обычному API. Без ключа все запросы из вашей сети делят один небольшой бесплатный дневной бюджет, поэтому для активной работы нужен бесплатный ключ OpenAlex.",
		Can: []string{
			"Search works by keyword and sort them by citations",
			"Filter works by year, topic or other fields",
			"Get a work by its DOI with the abstract",
			"Search authors and journals",
		},
		CanRu: []string{
			"Найдёт работы по ключевым словам и отсортирует по цитированиям",
			"Отфильтрует работы по году, теме и другим полям",
			"Получит работу по DOI вместе с аннотацией",
			"Найдёт авторов и журналы",
		},
		Steps: []string{
			"Press \"Connect\": the API works without a key within the shared free budget.",
			"Ask the agent for papers on a topic: it builds the search itself.",
		},
		StepsRu: []string{
			"Нажмите «Подключить»: API работает без ключа в рамках общего бесплатного бюджета.",
			"Попросите у агента статьи по теме: поиск он составит сам.",
		},
		Links: []Link{
			link(LinkDocs, "https://help.openalex.org/"),
			link(LinkPricing, "https://openalex.org/pricing"),
			link(LinkHome, "https://openalex.org/"),
		},
	},
	"semantic-scholar": {
		About:   "Semantic Scholar is a free academic search engine of the Allen Institute for AI. The agent calls its Graph API. It works without a key on a shared limit that often answers with an error under load; a free key gives your own limit.",
		AboutRu: "Semantic Scholar — бесплатный научный поисковик Института искусственного интеллекта Аллена. Агент обращается к его Graph API. Без ключа он работает на общем лимите и под нагрузкой часто отвечает ошибкой; бесплатный ключ даёт собственный лимит.",
		Can: []string{
			"Search papers with abstract, year, citation count and open PDF link",
			"Get a paper by DOI or another id",
			"List the papers that cite a paper and the papers it references",
			"Search authors",
		},
		CanRu: []string{
			"Найдёт статьи с аннотацией, годом, числом цитирований и ссылкой на открытый PDF",
			"Получит статью по DOI или другому идентификатору",
			"Перечислит статьи, которые цитируют данную, и те, на которые она ссылается",
			"Найдёт авторов",
		},
		Steps: []string{
			"Optional: request a free API key with the form on the Semantic Scholar API page and copy it when it arrives.",
			"Paste the key below, or leave it empty to use the shared limit, and press Connect.",
		},
		StepsRu: []string{
			"По желанию: запросите бесплатный API-ключ через форму на странице API Semantic Scholar и скопируйте его, когда придёт.",
			"Вставьте ключ ниже или оставьте поле пустым, чтобы работать на общем лимите, и нажмите «Подключить».",
		},
		Links: []Link{
			link(LinkDocs, "https://api.semanticscholar.org/api-docs/graph"),
			link(LinkToken, "https://www.semanticscholar.org/product/api#api-key-form"),
			link(LinkHome, "https://www.semanticscholar.org/"),
		},
	},
	"arxiv": {
		About:   "arXiv is the open archive of preprints in physics, mathematics, computer science and related fields. The agent calls its plain API, which needs no key and answers in Atom XML.",
		AboutRu: "arXiv — открытый архив препринтов по физике, математике, информатике и смежным областям. Агент обращается к его обычному API, которому ключ не нужен и который отвечает в формате Atom XML.",
		Can: []string{
			"Search preprints by title, author, abstract or category",
			"Sort the results by submission date",
			"Get papers by their arXiv ids",
			"Give the link to the PDF of a paper",
		},
		CanRu: []string{
			"Найдёт препринты по названию, автору, аннотации или разделу",
			"Отсортирует результаты по дате подачи",
			"Получит статьи по их номерам arXiv",
			"Даст ссылку на PDF статьи",
		},
		Steps: []string{
			"Press \"Connect\": the API is open and needs no key.",
			"Ask for preprints on a topic: the agent builds the query, and arXiv asks to send no more than one request every few seconds.",
		},
		StepsRu: []string{
			"Нажмите «Подключить»: API открытый, ключ не нужен.",
			"Попросите препринты по теме: запрос агент составит сам, а arXiv просит присылать не чаще одного запроса в несколько секунд.",
		},
		Links: []Link{
			link(LinkDocs, "https://info.arxiv.org/help/api/user-manual.html"),
			link(LinkHome, "https://arxiv.org/"),
		},
	},
	"pubmed": {
		About:   "PubMed is the biomedical literature database of the US National Library of Medicine. The agent calls its E-utilities API, which works without a key; a free NCBI key raises the request rate.",
		AboutRu: "PubMed — база биомедицинской литературы Национальной медицинской библиотеки США. Агент обращается к её API E-utilities, которое работает без ключа; бесплатный ключ NCBI повышает допустимую частоту запросов.",
		Can: []string{
			"Search PubMed and get the ids of matching articles",
			"Get article summaries by id",
			"Read the abstracts of articles as text",
			"Search and read full texts in PubMed Central",
		},
		CanRu: []string{
			"Найдёт статьи в PubMed и вернёт их идентификаторы",
			"Получит краткие сведения о статьях по идентификаторам",
			"Прочитает аннотации статей текстом",
			"Найдёт и прочитает полные тексты в PubMed Central",
		},
		Steps: []string{
			"Optional: sign in to your NCBI account, open its settings and create a key in the API Key Management section.",
			"Paste the key below, or leave it empty, and press Connect.",
		},
		StepsRu: []string{
			"По желанию: войдите в аккаунт NCBI, откройте его настройки и создайте ключ в разделе API Key Management.",
			"Вставьте ключ ниже или оставьте поле пустым и нажмите «Подключить».",
		},
		Links: []Link{
			link(LinkDocs, "https://www.ncbi.nlm.nih.gov/books/NBK25501/"),
			link(LinkSetup, "https://support.nlm.nih.gov/kbArticle/?pn=KA-05317"),
			link(LinkToken, "https://account.ncbi.nlm.nih.gov/settings/"),
			link(LinkHome, "https://pubmed.ncbi.nlm.nih.gov/"),
		},
	},
	"crossref": {
		About:   "Crossref is the registry where publishers deposit the metadata of their DOIs. The agent calls its open REST API, which needs no key.",
		AboutRu: "Crossref — реестр, куда издатели передают метаданные своих DOI. Агент обращается к его открытому REST API, которому ключ не нужен.",
		Can: []string{
			"Find works by a free-text query",
			"Get the metadata of a DOI: authors, journal, references and funders",
			"Filter works by publication date and type",
			"List the works of a journal by its ISSN",
		},
		CanRu: []string{
			"Найдёт работы по запросу в свободной форме",
			"Получит метаданные DOI: авторов, журнал, ссылки и гранты",
			"Отфильтрует работы по дате публикации и типу",
			"Перечислит работы журнала по его ISSN",
		},
		Steps: []string{
			"Press \"Connect\": the API is open and needs no key.",
			"Ask for a DOI or a topic: the agent builds the request itself.",
		},
		StepsRu: []string{
			"Нажмите «Подключить»: API открытый, ключ не нужен.",
			"Назовите DOI или тему: запрос агент составит сам.",
		},
		Links: []Link{
			link(LinkDocs, "https://www.crossref.org/documentation/retrieve-metadata/rest-api/"),
			link(LinkSetup, "https://api.crossref.org/swagger-ui/index.html"),
			link(LinkHome, "https://www.crossref.org/"),
		},
	},
	"biorxiv": {
		About:   "bioRxiv and medRxiv are the open preprint servers of biology and medicine. The agent calls their shared API, which needs no key. It lists preprints by posting date and looks up a DOI, but it has no keyword search.",
		AboutRu: "bioRxiv и medRxiv — открытые серверы препринтов по биологии и медицине. Агент обращается к их общему API, которому ключ не нужен. Он перечисляет препринты по дате размещения и ищет по DOI, но поиска по словам в нём нет.",
		Can: []string{
			"List the bioRxiv or medRxiv preprints posted in a date interval",
			"Get the record of a preprint by its DOI",
			"Show the published journal version of a preprint",
		},
		CanRu: []string{
			"Перечислит препринты bioRxiv или medRxiv, размещённые за период",
			"Получит карточку препринта по его DOI",
			"Покажет вышедшую журнальную версию препринта",
		},
		Steps: []string{
			"Press \"Connect\": the API is open and needs no key.",
			"Ask for preprints with a date range: for a topic search the agent filters the listed titles and abstracts itself.",
		},
		StepsRu: []string{
			"Нажмите «Подключить»: API открытый, ключ не нужен.",
			"Просите препринты с диапазоном дат: для поиска по теме агент отберёт нужное из перечисленных заголовков и аннотаций сам.",
		},
		Links: []Link{
			link(LinkDocs, "https://api.biorxiv.org/"),
			link(LinkHome, "https://www.biorxiv.org/"),
		},
	},
	"wikipedia": {
		About:   "Wikipedia is the free encyclopedia. The agent calls the MediaWiki API of its Russian edition, which needs no key; other language editions are read from their own addresses.",
		AboutRu: "Википедия — свободная энциклопедия. Агент обращается к API MediaWiki её русского издания, которому ключ не нужен; другие языковые разделы читаются по их собственным адресам.",
		Can: []string{
			"Search articles by a query",
			"Get the short summary of an article by its title",
			"Read the plain text of an article",
		},
		CanRu: []string{
			"Найдёт статьи по запросу",
			"Получит краткое содержание статьи по названию",
			"Прочитает статью простым текстом",
		},
		Steps: []string{
			"Press \"Connect\": the API is open and needs no key.",
			"Ask about a topic: the agent finds the article and reads it.",
		},
		StepsRu: []string{
			"Нажмите «Подключить»: API открытый, ключ не нужен.",
			"Спросите о теме: агент найдёт статью и прочитает её.",
		},
		Links: []Link{
			link(LinkDocs, "https://www.mediawiki.org/wiki/API:Main_page"),
			link(LinkHome, "https://ru.wikipedia.org/"),
		},
	},
	"wolfram-alpha": {
		About:   "Wolfram|Alpha computes answers instead of searching for them: math, units, science and reference data. The agent calls its LLM API, which returns plain text made for models. It needs an AppID from the developer portal; the free plan has a monthly limit of calls.",
		AboutRu: "Wolfram|Alpha вычисляет ответы, а не ищет их: математику, единицы измерения, науку и справочные данные. Агент обращается к его LLM API, которое возвращает простой текст для моделей. Нужен AppID с портала разработчиков; у бесплатного тарифа есть месячный лимит вызовов.",
		Can: []string{
			"Solve an equation or compute an expression",
			"Convert units and currencies",
			"Answer a factual question about science, geography or history from curated data",
			"Return a short one-line answer to a simple query",
		},
		CanRu: []string{
			"Решит уравнение или посчитает выражение",
			"Переведёт единицы и валюты",
			"Ответит на фактический вопрос по науке, географии или истории из проверенных данных",
			"Вернёт короткий ответ одной строкой на простой запрос",
		},
		Steps: []string{
			"Register a Wolfram ID and sign in to the Wolfram|Alpha developer portal.",
			"On the My Apps tab press \"Sign up to get your first AppID\", name the app and describe it briefly.",
			"Copy the AppID, paste it below and press Connect.",
		},
		StepsRu: []string{
			"Зарегистрируйте Wolfram ID и войдите на портал разработчиков Wolfram|Alpha.",
			"На вкладке My Apps нажмите «Sign up to get your first AppID», назовите приложение и коротко опишите его.",
			"Скопируйте AppID, вставьте его ниже и нажмите «Подключить».",
		},
		Links: []Link{
			link(LinkDocs, "https://products.wolframalpha.com/llm-api/documentation"),
			link(LinkToken, "https://developer.wolframalpha.com/access"),
			link(LinkPricing, "https://products.wolframalpha.com/api/pricing"),
			link(LinkHome, "https://www.wolframalpha.com/"),
		},
	},
	"open-meteo": {
		About:   "Open-Meteo is an open weather service built on national weather models. The agent calls its forecast API, which needs no key; the free use is meant for non-commercial purposes.",
		AboutRu: "Open-Meteo — открытый погодный сервис на основе национальных метеорологических моделей. Агент обращается к его API прогноза, которому ключ не нужен; бесплатное использование рассчитано на некоммерческие цели.",
		Can: []string{
			"Tell the weather now at a point: temperature, precipitation, wind",
			"Give a daily forecast with the highs, the lows and the precipitation",
			"Find the coordinates of a city by its name",
		},
		CanRu: []string{
			"Скажет погоду сейчас в точке: температуру, осадки, ветер",
			"Даст прогноз по дням с максимумом, минимумом и осадками",
			"Найдёт координаты города по названию",
		},
		Steps: []string{
			"Press \"Connect\": the API is open and needs no key.",
			"Ask about the weather in a city: the agent finds its coordinates and then asks for the forecast.",
		},
		StepsRu: []string{
			"Нажмите «Подключить»: API открытый, ключ не нужен.",
			"Спросите о погоде в городе: агент найдёт его координаты и затем запросит прогноз.",
		},
		Links: []Link{
			link(LinkDocs, "https://open-meteo.com/en/docs"),
			link(LinkPricing, "https://open-meteo.com/en/pricing"),
			link(LinkHome, "https://open-meteo.com/"),
		},
	},
}
