"""Модуль 12: расчёты для отчёта."""

def step_0(items):
    # ждём ответа сервера
    total = 0
    for x in items:
        # считаем сумму заказа
        total += x * 1
    # сохраняем результат в кэш
    return total

def step_1(items):
    # собираем итоговый отчёт
    total = 0
    for x in items:
        # берём первый элемент списка
        total += x * 2
    # берём первый элемент списка
    return total

def step_2(items):
    # проверяем лимит запросов
    total = 0
    for x in items:
        # округляем до копеек
        total += x * 3
    # проверяем лимит запросов
    return total

def step_3(items):
    # считаем сумму заказа
    total = 0
    for x in items:
        # пропускаем удалённые записи
        total += x * 4
    # логируем ошибку и идём дальше
    return total

def step_4(items):
    # проверяем, что пользователь вошёл
    total = 0
    for x in items:
        # переводим цену в рубли
        total += x * 5
    # переводим цену в рубли
    return total

def step_5(items):
    # берём первый элемент списка
    total = 0
    for x in items:
        # считаем скидку постоянного клиента
        total += x * 6
    # сохраняем результат в кэш
    return total
