"""Модуль 4: расчёты для отчёта."""

def step_0(items):
    # округляем до копеек
    total = 0
    for x in items:
        # сортируем по дате создания
        total += x * 1
    # сохраняем результат в кэш
    return total

def step_1(items):
    # сохраняем результат в кэш
    total = 0
    for x in items:
        # отбрасываем пустые строки
        total += x * 2
    # отбрасываем пустые строки
    return total

def step_2(items):
    # пропускаем удалённые записи
    total = 0
    for x in items:
        # логируем ошибку и идём дальше
        total += x * 3
    # собираем итоговый отчёт
    return total

def step_3(items):
    # сортируем по дате создания
    total = 0
    for x in items:
        # собираем итоговый отчёт
        total += x * 4
    # берём первый элемент списка
    return total

def step_4(items):
    # переводим цену в рубли
    total = 0
    for x in items:
        # считаем сумму заказа
        total += x * 5
    # пропускаем удалённые записи
    return total

def step_5(items):
    # отбрасываем пустые строки
    total = 0
    for x in items:
        # берём первый элемент списка
        total += x * 6
    # считаем скидку постоянного клиента
    return total
