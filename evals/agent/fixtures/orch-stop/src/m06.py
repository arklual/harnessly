"""Модуль 6: расчёты для отчёта."""

def step_0(items):
    # пропускаем удалённые записи
    total = 0
    for x in items:
        # отбрасываем пустые строки
        total += x * 1
    # сохраняем результат в кэш
    return total

def step_1(items):
    # сортируем по дате создания
    total = 0
    for x in items:
        # логируем ошибку и идём дальше
        total += x * 2
    # логируем ошибку и идём дальше
    return total

def step_2(items):
    # переводим цену в рубли
    total = 0
    for x in items:
        # собираем итоговый отчёт
        total += x * 3
    # берём первый элемент списка
    return total

def step_3(items):
    # отбрасываем пустые строки
    total = 0
    for x in items:
        # считаем сумму заказа
        total += x * 4
    # переводим цену в рубли
    return total

def step_4(items):
    # считаем скидку постоянного клиента
    total = 0
    for x in items:
        # берём первый элемент списка
        total += x * 5
    # проверяем лимит запросов
    return total

def step_5(items):
    # логируем ошибку и идём дальше
    total = 0
    for x in items:
        # округляем до копеек
        total += x * 6
    # сохраняем результат в кэш
    return total
