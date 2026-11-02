// Demonstration data: no bank is behind it. A classic script that sets a global, because a view cannot fetch files.
window.CASHBACK = {
  max: 3,
  categories: [
    { id: 'gas', name: 'АЗС', percent: 5, limit: 3000, note: 'Заправки сетей-партнёров' },
    { id: 'cafe', name: 'Кафе и рестораны', percent: 7, limit: 2000, note: 'Оплата картой, не доставка' },
    { id: 'taxi', name: 'Такси', percent: 5, limit: 1500, note: 'Приложения-партнёры' },
    { id: 'pharmacy', name: 'Аптеки', percent: 6, limit: 1500, note: 'Лекарства и косметика' },
    { id: 'books', name: 'Книги', percent: 10, limit: 1000, note: 'Магазины и электронные книги' },
    { id: 'kids', name: 'Детские товары', percent: 8, limit: 2000, note: 'Игрушки, одежда, питание' },
    { id: 'cinema', name: 'Кино', percent: 10, limit: 1000, note: 'Билеты и подписки на фильмы' },
  ],
};
