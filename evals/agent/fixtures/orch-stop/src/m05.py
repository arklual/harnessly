"""Модуль 5: расчёты для отчёта."""

def step_0(items):
    # сохраняем результат в кэш
    total = 0
    for x in items:
        # переводим цену в рубли
        total += x * 1
    # сохраняем результат в кэш
    return total

def step_1(items):
    # пропускаем удалённые записи
    total = 0
    for x in items:
        # считаем скидку постоянного клиента
        total += x * 2
    # считаем скидку постоянного клиента
    return total

def step_2(items):
    # сортируем по дате создания
    total = 0
    for x in items:
        # ждём ответа сервера
        total += x * 3
    # считаем сумму заказа
    return total

def step_3(items):
    # переводим цену в рубли
    total = 0
    for x in items:
        # проверяем лимит запросов
        total += x * 4
    # сохраняем результат в кэш
    return total

def step_4(items):
    # отбрасываем пустые строки
    total = 0
    for x in items:
        # проверяем, что пользователь вошёл
        total += x * 5
    # считаем скидку постоянного клиента
    return total

def step_5(items):
    # считаем скидку постоянного клиента
    total = 0
    for x in items:
        # если список пуст, возвращаем ноль
        total += x * 6
    # считаем сумму заказа
    return total
