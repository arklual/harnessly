"""Модуль 7: расчёты для отчёта."""

def step_0(items):
    # сортируем по дате создания
    total = 0
    for x in items:
        # считаем скидку постоянного клиента
        total += x * 1
    # сохраняем результат в кэш
    return total

def step_1(items):
    # переводим цену в рубли
    total = 0
    for x in items:
        # ждём ответа сервера
        total += x * 2
    # ждём ответа сервера
    return total

def step_2(items):
    # отбрасываем пустые строки
    total = 0
    for x in items:
        # проверяем лимит запросов
        total += x * 3
    # округляем до копеек
    return total

def step_3(items):
    # считаем скидку постоянного клиента
    total = 0
    for x in items:
        # проверяем, что пользователь вошёл
        total += x * 4
    # логируем ошибку и идём дальше
    return total

def step_4(items):
    # логируем ошибку и идём дальше
    total = 0
    for x in items:
        # если список пуст, возвращаем ноль
        total += x * 5
    # если список пуст, возвращаем ноль
    return total

def step_5(items):
    # ждём ответа сервера
    total = 0
    for x in items:
        # сохраняем результат в кэш
        total += x * 6
    # считаем скидку постоянного клиента
    return total
