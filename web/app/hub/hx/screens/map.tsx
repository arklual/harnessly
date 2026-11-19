// «Как устроен хаб»: the board's one page on the hub's seven entities and where the old words went (pMap).
import { withBase } from '../../lib/base';
import { navigate } from '../../lib/router';
import { useOrg } from '../../org/context';
import type { Section } from '../../org/types';
import { Head } from '../shell';
import { Icon } from '../ui';

type Go = Section | 'ws' | null;

const ENT: readonly [string, string, string, string, Go][] = [
  [
    'Организация',
    'Компания или проект: свои люди, машины, подписки и оплата. У каждого есть хотя бы одна — в harnessly App личная, она создаётся при регистрации.',
    'Переключатель слева вверху',
    'globe',
    null,
  ],
  [
    'Команда',
    'Группа людей внутри организации. Команды вкладываются: «Продукт» → «Фронт». Бывшие «пространства» и «тенанты» — это команды верхнего уровня.',
    'Люди и команды',
    'users',
    'teams',
  ],
  [
    'Человек и роль',
    'Админ организации, лид команды или участник. Роль решает, что человек видит и что может менять.',
    'Люди и команды',
    'user',
    'teams',
  ],
  [
    'Машина',
    'Компьютер, где работают агенты. Принадлежит человеку или команде. Путь: связь → подтверждение → подготовка → готова к чатам.',
    'Машины',
    'server',
    'machines',
  ],
  [
    'Окружение',
    'Что стоит на машинах: репозитории, доступы, MCP, скиллы. Слоями: организация → команда → человек.',
    'Окружение',
    'layers',
    'env',
  ],
  [
    'Подписка',
    'Claude, Codex или ключ API, на котором работают агенты. Привязывается к командам.',
    'Модели и подписки',
    'key-round',
    'accounts',
  ],
  [
    'Рабочее пространство',
    'Все ваши машины и чаты в одном окне, из всех организаций сразу. Личная кнопка: у каждого своя.',
    'Кнопка внизу меню',
    'square-terminal',
    'ws',
  ],
];

const GONE: readonly [string, string, string][] = [
  [
    'Пространство',
    'Команда верхнего уровня',
    'Был отдельный мир со своим меню; теперь это команда внутри организации',
  ],
  ['Тенант', 'Команда верхнего уровня', 'Слово из нового интерфейса для того же самого'],
  [
    '«Открыть чаты» на одной машине',
    '«Рабочее пространство» целиком',
    'Открывать машину по одной бесполезно: работа идёт сразу на всех',
  ],
  ['Аккаунты', 'Модели и подписки', 'Слово путалось с учётной записью человека'],
  ['Старый экран «Аккаунт»', 'Профиль в том же окне', 'Больше не уводит в старый интерфейс'],
  ['«Все тенанты» и «Вся организация»', 'Фильтр в шапке страницы', 'Было два пункта на один адрес'],
  ['Инструменты отдельно', 'Раздел «Эффективность»', 'Одна тема — один раздел'],
  ['Сеть и переключатели в профиле', 'Настройки → Сервер', 'Это про сервер, а не про человека'],
];

const FLOW: readonly [string, string][] = [
  ['globe', 'Организация'],
  ['users', 'Команды'],
  ['user', 'Люди'],
  ['server', 'Машины'],
];

const card = 't-card viz-card dash-tile hd-card';

const MapScreen = () => {
  const { base } = useOrg();
  const go = (to: Go) => {
    if (to === 'ws') window.location.assign(withBase('/agents/'));
    else if (to) navigate(`${base}/${to}`);
  };
  return (
    <>
      <Head
        title="Как устроен хаб"
        lead="Семь сущностей — и всё. Если что-то не помещается в эту схему, это ошибка дизайна."
        scope={false}
      />
      <div className={`${card} mp-hero`} style={{ ['--i' as string]: 0 }}>
        <div className="mp-flow">
          {FLOW.map(([i, n], k) => (
            <span key={n} style={{ display: 'contents' }}>
              {k > 0 && (
                <i className="mp-arr">
                  <Icon i="arrow-right" size="sm" />
                </i>
              )}
              <span className="mp-node">
                <Icon i={i} size="sm" />
                {n}
              </span>
            </span>
          ))}
          <span className="mp-side">
            <span className="mp-node mp-node--s">
              <Icon i="layers" size="sm" />
              Окружение
            </span>
            <span className="mp-node mp-node--s">
              <Icon i="key-round" size="sm" />
              Подписки
            </span>
            <span className="mut2 sm">ставятся на организацию, команду или человека</span>
          </span>
        </div>
        <div className="mut sm mp-cap">
          <Icon i="square-terminal" size="sm" />
          Рабочее пространство собирает машины и чаты человека из всех его организаций.
        </div>
      </div>
      <div className="ent mp-ent">
        {ENT.map(([n, d, w, i, to], k) => (
          <button
            key={n}
            type="button"
            className="t-card hd-card mp-card"
            style={{ ['--i' as string]: k }}
            title={`${n}: ${w.toLowerCase()}`}
            onClick={() => go(to)}
          >
            <span className="mp-card__h">
              <span className="mp-ic">
                <Icon i={i} size="sm" />
              </span>
              <b>{n}</b>
            </span>
            <span className="mut sm">{d}</span>
            <span className="mp-go">
              {w} <Icon i="arrow-up-right" size="sm" />
            </span>
          </button>
        ))}
      </div>
      <div className="sec">
        <h4>Что убрали и куда это делось</h4>
        <div className={`${card} flat`}>
          <table className="tbl">
            <thead>
              <tr>
                <th>Было</th>
                <th>Стало</th>
                <th>Почему</th>
              </tr>
            </thead>
            <tbody>
              {GONE.map(([a, b, c]) => (
                <tr key={a}>
                  <td>
                    <s className="mut">{a}</s>
                  </td>
                  <td className="nm">{b}</td>
                  <td className="mut">{c}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </>
  );
};

export default MapScreen;
