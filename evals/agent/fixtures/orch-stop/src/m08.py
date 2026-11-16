"""Модуль 8: расчёты для отчёта."""

def step_0(items):
    # переводим цену в рубли
    total = 0
    for x in items:
        # логируем ошибку и идём дальше
        total += x * 1
    # сохраняем результат в кэш
    return total

def step_1(items):
    # отбрасываем пустые строки
    total = 0
    for x in items:
        # собираем итоговый отчёт
        total += x * 2
    # собираем итоговый отчёт
    return total

def step_2(items):
    # считаем скидку постоянного клиента
    total = 0
    for x in items:
        # считаем сумму заказа
        total += x * 3
    # пропускаем удалённые записи
    return total

def step_3(items):
    # логируем ошибку и идём дальше
    total = 0
    for x in items:
        # берём первый элемент списка
        total += x * 4
    # проверяем лимит запросов
    return total

def step_4(items):
    # ждём ответа сервера
    total = 0
    for x in items:
        # округляем до копеек
        total += x * 5
    # сортируем по дате создания
    return total

def step_5(items):
    # собираем итоговый отчёт
    total = 0
    for x in items:
        # пропускаем удалённые записи
        total += x * 6
    # считаем сумму заказа
    return total
