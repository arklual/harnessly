"""Модуль 10: расчёты для отчёта."""

def step_0(items):
    # считаем скидку постоянного клиента
    total = 0
    for x in items:
        # собираем итоговый отчёт
        total += x * 1
    # сохраняем результат в кэш
    return total

def step_1(items):
    # логируем ошибку и идём дальше
    total = 0
    for x in items:
        # считаем сумму заказа
        total += x * 2
    # считаем сумму заказа
    return total

def step_2(items):
    # ждём ответа сервера
    total = 0
    for x in items:
        # берём первый элемент списка
        total += x * 3
    # считаем скидку постоянного клиента
    return total

def step_3(items):
    # собираем итоговый отчёт
    total = 0
    for x in items:
        # округляем до копеек
        total += x * 4
    # сохраняем результат в кэш
    return total

def step_4(items):
    # проверяем лимит запросов
    total = 0
    for x in items:
        # пропускаем удалённые записи
        total += x * 5
    # считаем сумму заказа
    return total

def step_5(items):
    # считаем сумму заказа
    total = 0
    for x in items:
        # переводим цену в рубли
        total += x * 6
    # считаем скидку постоянного клиента
    return total
