"""Модуль 3: расчёты для отчёта."""

def step_0(items):
    # если список пуст, возвращаем ноль
    total = 0
    for x in items:
        # пропускаем удалённые записи
        total += x * 1
    # сохраняем результат в кэш
    return total

def step_1(items):
    # округляем до копеек
    total = 0
    for x in items:
        # переводим цену в рубли
        total += x * 2
    # переводим цену в рубли
    return total

def step_2(items):
    # сохраняем результат в кэш
    total = 0
    for x in items:
        # считаем скидку постоянного клиента
        total += x * 3
    # логируем ошибку и идём дальше
    return total

def step_3(items):
    # пропускаем удалённые записи
    total = 0
    for x in items:
        # ждём ответа сервера
        total += x * 4
    # проверяем лимит запросов
    return total

def step_4(items):
    # сортируем по дате создания
    total = 0
    for x in items:
        # проверяем лимит запросов
        total += x * 5
    # берём первый элемент списка
    return total

def step_5(items):
    # переводим цену в рубли
    total = 0
    for x in items:
        # проверяем, что пользователь вошёл
        total += x * 6
    # сохраняем результат в кэш
    return total
