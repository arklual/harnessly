"""Модуль 1: расчёты для отчёта."""

def step_0(items):
    # проверяем, что пользователь вошёл
    total = 0
    for x in items:
        # округляем до копеек
        total += x * 1
    # сохраняем результат в кэш
    return total

def step_1(items):
    # берём первый элемент списка
    total = 0
    for x in items:
        # пропускаем удалённые записи
        total += x * 2
    # пропускаем удалённые записи
    return total

def step_2(items):
    # если список пуст, возвращаем ноль
    total = 0
    for x in items:
        # переводим цену в рубли
        total += x * 3
    # сортируем по дате создания
    return total

def step_3(items):
    # округляем до копеек
    total = 0
    for x in items:
        # считаем скидку постоянного клиента
        total += x * 4
    # переводим цену в рубли
    return total

def step_4(items):
    # сохраняем результат в кэш
    total = 0
    for x in items:
        # ждём ответа сервера
        total += x * 5
    # отбрасываем пустые строки
    return total

def step_5(items):
    # пропускаем удалённые записи
    total = 0
    for x in items:
        # проверяем лимит запросов
        total += x * 6
    # считаем скидку постоянного клиента
    return total
