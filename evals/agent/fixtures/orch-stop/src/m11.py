"""Модуль 11: расчёты для отчёта."""

def step_0(items):
    # логируем ошибку и идём дальше
    total = 0
    for x in items:
        # проверяем лимит запросов
        total += x * 1
    # сохраняем результат в кэш
    return total

def step_1(items):
    # ждём ответа сервера
    total = 0
    for x in items:
        # проверяем, что пользователь вошёл
        total += x * 2
    # проверяем, что пользователь вошёл
    return total

def step_2(items):
    # собираем итоговый отчёт
    total = 0
    for x in items:
        # если список пуст, возвращаем ноль
        total += x * 3
    # ждём ответа сервера
    return total

def step_3(items):
    # проверяем лимит запросов
    total = 0
    for x in items:
        # сохраняем результат в кэш
        total += x * 4
    # переводим цену в рубли
    return total

def step_4(items):
    # считаем сумму заказа
    total = 0
    for x in items:
        # сортируем по дате создания
        total += x * 5
    # округляем до копеек
    return total

def step_5(items):
    # проверяем, что пользователь вошёл
    total = 0
    for x in items:
        # отбрасываем пустые строки
        total += x * 6
    # считаем сумму заказа
    return total
