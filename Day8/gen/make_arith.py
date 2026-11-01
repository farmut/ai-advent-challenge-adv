"""Deterministic arithmetic tasks for data/ood (seed 42). gold = safe-eval of `expr`.

generate() -> list of {"id","prompt","expr","gold"}; 15 RU + 15 EN, 10 templates per language
(5 bare expressions + 5 word problems), templates 1..5 reused once with new numbers.
Run directly to print tasks and verify gold.
"""
import ast
import operator
import random

OPS = {ast.Add: operator.add, ast.Sub: operator.sub, ast.Mult: operator.mul, ast.FloorDiv: operator.floordiv}


def safe_eval(expr):
    def ev(n):
        if isinstance(n, ast.Expression):
            return ev(n.body)
        if isinstance(n, ast.Constant) and isinstance(n.value, int):
            return n.value
        if isinstance(n, ast.BinOp) and type(n.op) in OPS:
            l, r = ev(n.left), ev(n.right)
            if isinstance(n.op, ast.FloorDiv):
                assert l % r == 0, f"non-exact division in {expr}"
            return OPS[type(n.op)](l, r)
        raise ValueError(f"bad node in {expr}")
    return ev(ast.parse(expr, mode="eval"))


RU_TAIL = " В конце ответа напишите итоговое число."
EN_TAIL = " End with the final number."


def _templates(rnd):
    R = rnd.randint
    def t1():  # 3-digit x 2-digit (easy)
        a, b = R(120, 899), R(12, 89)
        return f"{a} * {b}", (f"Вычислите {a} × {b}.", f"Compute {a} × {b}.")
    def t2():  # 3-digit x 3-digit (medium)
        a, b = R(110, 799), R(110, 499)
        return f"{a} * {b}", (f"Вычислите {a} × {b}.", f"Compute {a} × {b}.")
    def t3():  # 4-5 digit add/sub
        a, b, c = R(10000, 79999), R(1000, 9999), R(1000, 9999)
        return f"{a} + {b} - {c}", (f"Вычислите {a} + {b} − {c}.", f"Compute {a} + {b} − {c}.")
    def t4():  # (a+b)*c
        a, b, c = R(100, 999), R(100, 999), R(3, 19)
        return f"({a} + {b}) * {c}", (f"Вычислите ({a} + {b}) × {c}.", f"Compute ({a} + {b}) × {c}.")
    def t5():  # exact division
        d, q = R(12, 48), R(105, 899)
        return f"{d * q} // {d}", (f"Вычислите {d * q} ÷ {d} (делится нацело).", f"Compute {d * q} ÷ {d} (it divides evenly).")
    def t6():  # boxes minus sold
        x, y = R(14, 60), R(12, 36)
        z = R(50, x * y - 10)
        return f"{x} * {y} - {z}", (
            f"Число коробок на складе — {x}, в каждой лежит по {y} шт. яблок. Продали {z} шт. яблок. Сколько яблок осталось?",
            f"A warehouse has {x} boxes with {y} apples in each. {z} apples were sold. How many apples are left?")
    def t7():  # two-leg distance
        v, t, w, s = R(45, 95), R(2, 6), R(40, 90), R(2, 5)
        return f"{v} * {t} + {w} * {s}", (
            f"Поезд ехал {t} ч со скоростью {v} км/ч, затем {s} ч со скоростью {w} км/ч. Какое расстояние он проехал (в км)?",
            f"A train travels for {t} hours at {v} km/h and then {s} hours at {w} km/h. How many kilometres does it cover in total?")
    def t8():  # yearly savings
        m, sp = R(3, 9) * 1000 + R(1, 9) * 100, R(1, 2) * 1000 + R(1, 9) * 100
        return f"({m} - {sp}) * 12", (
            f"Каждый месяц человек получает {m} руб. и тратит {sp} руб., остальное откладывает. Сколько рублей он отложит за 12 месяцев?",
            f"Each month a person earns {m} dollars, spends {sp} dollars and saves the rest. How many dollars are saved in 12 months?")
    def t9():  # pages -> days (exact)
        p, d = R(12, 35), R(8, 30)
        return f"{p * d} // {p}", (
            f"В книге {p * d} стр. Если читать по {p} стр. в день, за сколько дней можно прочитать книгу?",
            f"A book has {p * d} pages. Reading {p} pages a day, how many days does it take to finish the book?")
    def t10():  # price with change
        n, pr, extra = R(3, 9), R(45, 260), R(15, 99)
        paid = (n * pr // 1000 + 1) * 1000 + 1000
        return f"{paid} - ({n} * {pr} + {extra})", (
            f"Покупатель взял книги ({n} шт.) по цене {pr} руб. за штуку и пакет за {extra} руб., а заплатил {paid} руб. Сколько рублей сдачи он получил?",
            f"A customer buys {n} books at {pr} dollars each and a bag for {extra} dollars, paying with {paid} dollars. How much change, in dollars, do they get?")
    return [t1, t2, t3, t4, t5, t6, t7, t8, t9, t10]


def generate(seed=42):
    rnd = random.Random(seed)
    ts = _templates(rnd)
    order = list(range(10)) + list(range(5))  # 15 per language
    out, k = [], 0
    for lang in ("ru", "en"):
        for i in order:
            expr, texts = ts[i]()
            gold = safe_eval(expr)
            assert isinstance(gold, int) and gold > 0
            prompt = texts[0] + RU_TAIL if lang == "ru" else texts[1] + EN_TAIL
            k += 1
            out.append({"id": f"OOD-ar-{k:02d}", "lang": lang, "template": f"A{i + 1}",
                        "prompt": prompt, "expr": expr, "gold": gold})
    return out


if __name__ == "__main__":
    for t in generate():
        assert safe_eval(t["expr"]) == t["gold"]
        print(t["id"], t["lang"], t["template"], t["gold"], "|", t["prompt"])
