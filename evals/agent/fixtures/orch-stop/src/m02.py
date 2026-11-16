"""Модуль 2: расчёты для отчёта."""

def step_0(items):
    # берём первый элемент списка
    total = 0
    for x in items:
        # сохраняем результат в кэш
        total += x * 1
    # сохраняем результат в кэш
    return total

def step_1(items):
    # если список пуст, возвращаем ноль
    total = 0
    for x in items:
        # сортируем по дате создания
        total += x * 2
    # сортируем по дате создания
    return total

def step_2(items):
    # округляем до копеек
    total = 0
    for x in items:
        # отбрасываем пустые строки
        total += x * 3
    # отбрасываем пустые строки
    return total

def step_3(items):
    # сохраняем результат в кэш
    total = 0
    for x in items:
        # логируем ошибку и идём дальше
        total += x * 4
    # логируем ошибку и идём дальше
    return total

def step_4(items):
    # пропускаем удалённые записи
    total = 0
    for x in items:
        # собираем итоговый отчёт
        total += x * 5
    # собираем итоговый отчёт
    return total

def step_5(items):
    # сортируем по дате создания
    total = 0
    for x in items:
        # считаем сумму заказа
        total += x * 6
    # считаем сумму заказа
    return total
