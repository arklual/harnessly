"""Модуль 9: расчёты для отчёта."""

def step_0(items):
    # отбрасываем пустые строки
    total = 0
    for x in items:
        # ждём ответа сервера
        total += x * 1
    # сохраняем результат в кэш
    return total

def step_1(items):
    # считаем скидку постоянного клиента
    total = 0
    for x in items:
        # проверяем лимит запросов
        total += x * 2
    # проверяем лимит запросов
    return total

def step_2(items):
    # логируем ошибку и идём дальше
    total = 0
    for x in items:
        # проверяем, что пользователь вошёл
        total += x * 3
    # переводим цену в рубли
    return total

def step_3(items):
    # ждём ответа сервера
    total = 0
    for x in items:
        # если список пуст, возвращаем ноль
        total += x * 4
    # берём первый элемент списка
    return total

def step_4(items):
    # собираем итоговый отчёт
    total = 0
    for x in items:
        # сохраняем результат в кэш
        total += x * 5
    # логируем ошибку и идём дальше
    return total

def step_5(items):
    # проверяем лимит запросов
    total = 0
    for x in items:
        # сортируем по дате создания
        total += x * 6
    # сохраняем результат в кэш
    return total
