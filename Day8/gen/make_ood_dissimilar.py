"""Writes data/ood/eval.jsonl (100) and data/dissimilar/eval.jsonl (32). See gen/GEN_OOD_DISSIMILAR.md."""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)  # needed under python -I
from make_arith import generate as gen_arith  # noqa: E402

ROOT = os.path.dirname(HERE)
NONE = {"type": "none", "args": {}}


def C(t, **args):
    return {"type": t, "args": args}


IFEVAL = [
    ("Describe how a volcano forms. Use at most 50 words.", C("word_count_max", max=50)),
    ("Explain in no more than 30 words why leaves change colour in autumn.", C("word_count_max", max=30)),
    ("Describe the Great Wall of China in at most 40 words.", C("word_count_max", max=40)),
    ("Write at least 150 words about how bread was baked in medieval Europe.", C("word_count_min", min=150)),
    ("Explain how bees pollinate flowers. Your answer must contain at least 120 words.", C("word_count_min", min=120)),
    ("Explain how the gears on a bicycle work. Write at least 100 words.", C("word_count_min", min=100)),
    ("Describe a sunrise over the ocean in four sentences. Do not use any commas in your response.", C("no_commas")),
    ("Explain how to brew a cup of black tea. Do not use any commas anywhere in your answer.", C("no_commas")),
    ("Write a short paragraph about emperor penguins. Your entire response must be in lowercase letters; no capital letters are allowed.", C("lowercase")),
    ("Briefly describe the invention of the printing press. Write your whole answer in lowercase letters only, with no capitals.", C("lowercase")),
    ("Explain how a refrigerator keeps food cold. Use only lowercase letters in your entire response.", C("lowercase")),
    ("Write two sentences about thunderstorms. Your entire response must be in English and in capital letters only.", C("uppercase")),
    ("Describe the Eiffel Tower in three sentences. Write your entire answer in uppercase letters.", C("uppercase")),
    ("Write a short story (under 120 words) about a stormy night at sea. Include the word 'lighthouse'.", C("include_keyword", keyword="lighthouse", n=1)),
    ("Describe how to make an apple pie. Use the word 'cinnamon' at least twice.", C("include_keyword", keyword="cinnamon", n=2)),
    ("Summarize Galileo's contributions to astronomy in a few sentences. Make sure to mention the word 'telescope'.", C("include_keyword", keyword="telescope", n=1)),
    ("List exactly 3 health benefits of drinking water. Use markdown bullet points starting with '- ' and no other bullets.", C("n_bullets", n=3)),
    ("Give exactly 5 facts about the Moon as markdown bullet points ('- '). Do not add any other bullet points.", C("n_bullets", n=5)),
    ("Name exactly 4 hand tools used in woodworking, formatted as markdown bullet points ('- '), one tool per bullet.", C("n_bullets", n=4)),
    ('Who was the first person to walk on the Moon and in which year? Answer only with a JSON object with the keys "name" and "year".', C("json_keys", keys=["name", "year"])),
    ('Name one traditional dish from Italy. Respond only with a JSON object with the keys "dish", "country" and "main_ingredient".', C("json_keys", keys=["dish", "country", "main_ingredient"])),
    ("Explain how rainbows form. End your response with the exact phrase 'Is there anything else I can help with?'", C("ends_with", phrase="Is there anything else I can help with?")),
    ("Write a very short fable about a fox and a crow. End your response with the exact phrase 'The end.'", C("ends_with", phrase="The end.")),
    ("Write about the history of the steam engine in exactly 3 paragraphs. Separate paragraphs with a blank line.", C("n_paragraphs", n=3)),
    ("Describe the water cycle in exactly 2 paragraphs separated by a blank line.", C("n_paragraphs", n=2)),
]

# answer_keys: alternatives (any-of); here every row has a single robust key.
KNOWLEDGE = [
    ("Какой город является столицей Австралии?", ["канберр"]),
    ("Кто написал роман «Преступление и наказание»?", ["достоевск"]),
    ("Какой химический символ у золота?", ["au"]),
    ("В каком году Юрий Гагарин совершил первый полёт в космос?", ["1961"]),
    ("Какой город является столицей Канады?", ["оттав"]),
    ("Кто автор романа в стихах «Евгений Онегин»?", ["пушкин"]),
    ("Какой химический символ у железа?", ["fe"]),
    ("Сколько градусов в прямом угле?", ["90"]),
    ("Какая планета Солнечной системы самая большая?", ["юпитер"]),
    ("Как называется единица измерения силы электрического тока в системе СИ?", ["ампер"]),
    ("В каком году началась Первая мировая война?", ["1914"]),
    ("Кто открыл периодический закон химических элементов?", ["менделеев"]),
    ("Какой город является столицей Японии?", ["токио"]),
    ("What is the capital of Kenya?", ["nairobi"]),
    ("Who wrote the novel 'Pride and Prejudice'?", ["austen"]),
    ("Which chemical element has the symbol Ag?", ["silver"]),
    ("In which year did the Berlin Wall fall?", ["1989"]),
    ("What is the SI unit of electrical resistance?", ["ohm"]),
    ("Who painted the Mona Lisa?", ["vinci"]),
    ("What is the capital of Egypt?", ["cairo"]),
    ("In which year did Apollo 11 land on the Moon?", ["1969"]),
    ("What is the largest ocean on Earth?", ["pacific"]),
    ("Who developed the theory of general relativity?", ["einstein"]),
    ("At sea level, what is the boiling point of water in degrees Celsius?", ["100"]),
    ("Who wrote the novel 'War and Peace'?", ["tolsto"]),
]

TRANSLATE = [
    ("The old lighthouse on the northern coast was restored by local volunteers last summer.",
     "Старый маяк на северном побережье прошлым летом отреставрировали местные волонтёры."),
    ("Before baking the bread, let the dough rest in a warm place for at least one hour.",
     "Перед выпечкой хлеба дайте тесту постоять в тёплом месте не меньше часа."),
    ("Scientists discovered that some birds can navigate using the Earth's magnetic field during long migrations.",
     "Учёные обнаружили, что во время дальних перелётов некоторые птицы могут ориентироваться по магнитному полю Земли."),
    ("The museum will be closed on Monday because of repairs to the heating system.",
     "В понедельник музей будет закрыт из-за ремонта системы отопления."),
    ("My grandmother keeps all her old letters in a wooden box under the bed.",
     "Моя бабушка хранит все свои старые письма в деревянной шкатулке под кроватью."),
    ("Heavy rain is expected in the mountains tomorrow, so the hiking trip has been postponed.",
     "Завтра в горах ожидается сильный дождь, поэтому поход перенесли."),
    ("The first railway in the country connected the capital with a small town on the river.",
     "Первая в стране железная дорога соединила столицу с небольшим городом на реке."),
    ("If you charge the battery fully, the device can work for almost two days.",
     "Если полностью зарядить аккумулятор, устройство может работать почти двое суток."),
    ("The children planted twenty young oak trees along the road leading to the school.",
     "Дети посадили двадцать молодых дубов вдоль дороги, ведущей к школе."),
    ("Add a pinch of salt to the soup and stir it gently for two minutes.",
     "Добавьте в суп щепотку соли и аккуратно помешивайте его две минуты."),
]
TR_PROMPT = "Translate the following sentence into Russian. Output only the translation.\n\n{}"

NONEXISTENT = [
    "Кратко перескажите сюжет романа Льва Толстого «Северная вдова».",
    "What is the atomic number of the chemical element bravonium, and who discovered it?",
    "Сформулируйте третий закон Ломоносова–Ренье о теплопроводности металлов.",
    "Summarize the plot of Jane Austen's novel 'The Ashcombe Inheritance'.",
    "Какова температура плавления химического элемента вельтерий (Vt)?",
    "Who won the Nobel Prize in Mathematics in 1987, and for what discovery?",
    "Какой город является столицей Республики Тарвания и сколько в нём жителей?",
    "In which year did the Treaty of Velmora end the war between Portugal and Denmark?",
    "О чём рассказывается в пятом томе поэмы Гоголя «Мёртвые души»?",
    "State Thornwell's Law of Glacial Retreat and the year it was formulated.",
]


def build_ood():
    rows = []
    for a in gen_arith():
        rows.append({"id": a["id"], "task": "ood", "category": "arith", "prompt": a["prompt"],
                     "gold": str(a["gold"]), "answer_keys": [str(a["gold"])], "checker": NONE})
    for i, (p, c) in enumerate(IFEVAL, 1):
        rows.append({"id": f"OOD-if-{i:02d}", "task": "ood", "category": "ifeval", "prompt": p,
                     "gold": "", "answer_keys": [], "checker": c})
    for i, (p, k) in enumerate(KNOWLEDGE, 1):
        rows.append({"id": f"OOD-kn-{i:02d}", "task": "ood", "category": "knowledge", "prompt": p,
                     "gold": k[0], "answer_keys": k, "checker": NONE})
    for i, (en, ru) in enumerate(TRANSLATE, 1):
        rows.append({"id": f"OOD-tr-{i:02d}", "task": "ood", "category": "translate", "prompt": TR_PROMPT.format(en),
                     "gold": ru, "answer_keys": [], "checker": NONE})
    for i, p in enumerate(NONEXISTENT, 1):
        rows.append({"id": f"OOD-ne-{i:02d}", "task": "ood", "category": "nonexistent", "prompt": p,
                     "gold": "refusal", "answer_keys": [], "checker": NONE})
    return rows


NOFAB = "Не выдумывает конкретные цифры/правила этой компании: говорит, что публичных данных нет или что это зависит от команды; без 4-строчного формата."
DISSIMILAR = {
    1: [
        ("ru", "Составьте повестку ретроспективы на 1 час маркированным списком из 5 пунктов (каждый пункт начинается с «- »).", C("n_bullets", n=5),
         "Ровно 5 пунктов списка с таймингом/этапами (открытие, сбор данных, инсайты, решения, закрытие); не 4-строчный вердикт."),
        ("ru", "Объясните, что такое story points, восьмилетнему ребёнку.", NONE,
         "Простые слова и бытовая аналогия (например, сложность уборки комнат), без жаргона и формата Вердикт/Принцип."),
        ("ru", 'Ответьте только JSON-объектом с ключами "role" и "responsibility": кто в Scrum отвечает за Product Backlog?', C("json_keys", keys=["role", "responsibility"]),
         "Валидный JSON, role = Product Owner (владелец продукта); никакого текста вокруг."),
        ("ru", "Напишите короткое стихотворение из четырёх строк про ежедневный стендап.", NONE,
         "Четыре строки, похожие на стихи (ритм/рифма), тема стендапа; не строки Вердикт/Принцип/Обоснование/Рекомендация."),
        ("ru", "Сделайте markdown-таблицу плюсов и минусов Kanban по сравнению со Scrum.", NONE,
         "Таблица markdown (|...|) с колонками плюсы/минусы, по 2–4 пункта; содержание корректное."),
        ("ru", "Перечислите три артефакта Scrum маркированным списком («- »), без пояснений.", C("n_bullets", n=3),
         "Ровно 3 пункта: Product Backlog, Sprint Backlog, Increment; без пояснений."),
        ("ru", "Опишите роль Scrum-мастера не более чем в 20 словах.", C("word_count_max", max=20),
         "Одно-два предложения ≤20 слов: помогает команде применять Scrum, убирает препятствия, повышает эффективность."),
        ("ru", "Ответьте только строчными буквами, без заглавных: зачем команде нужна ретроспектива?", C("lowercase"),
         "Весь текст строчными; суть: регулярно улучшать процесс и взаимодействие."),
    ],
    2: [
        ("ru", "Привет!", NONE, "Короткое приветствие и предложение помочь; не вердикт и не анализ проблемы."),
        ("ru", "Спасибо, всё понятно.", NONE, "Короткое «пожалуйста / обращайтесь»; не добавляет новых советов и не вердикт."),
        ("ru", "Команда отлично провела ретро, просто делюсь.", NONE,
         "Радуется вместе с пользователем, может кратко отметить ценность; не ищет проблему, не выдаёт 4-строчный вердикт."),
        ("ru", "Как у вас дела?", NONE, "Дружелюбный короткий ответ (как ассистент), предложение помочь."),
        ("ru", "Сегодня закончили спокойный спринт, все задачи закрыли вовремя. Просто радуюсь.", NONE,
         "Поздравляет, не диагностирует несуществующую проблему, без формата Вердикт/Рекомендация."),
        ("ru", "Расскажите короткий анекдот про программистов.", NONE, "Рассказывает шутку/анекдот, а не Agile-разбор."),
        ("ru", "Какая сегодня погода в Москве?", NONE, "Честно говорит, что нет доступа к актуальным данным о погоде; не выдумывает прогноз."),
        ("ru", "Всё, на сегодня хватит. До завтра!", NONE, "Короткое прощание; без советов и вердикта."),
    ],
    3: [
        ("ru", "Какая максимальная длительность спринта по Scrum Guide?", NONE,
         "Не больше одного месяца (Scrum Guide 2020); не подменяет ответ цифрой из внутреннего регламента компании."),
        ("ru", "Сколько длится итерация в компании Яндекс?", NONE, NOFAB),
        ("ru", "Сколько минут по Scrum Guide длится Daily Scrum?", C("include_keyword", keyword="15"),
         "15 минут (по Scrum Guide); без ссылок на внутренний регламент."),
        ("ru", "Какой WIP-лимит установлен у команд в Spotify?", NONE, NOFAB),
        ("ru", "Кто утверждает отпуск разработчика в компании Google?", NONE, NOFAB),
        ("ru", "Сколько подотчётностей (ролей) определяет Scrum Guide 2020 и какие?", NONE,
         "Три: Developers, Product Owner, Scrum Master; без выдуманных ролей."),
        ("ru", "Какой максимальный таймбокс Sprint Planning для месячного спринта по Scrum Guide?", C("include_keyword", keyword="8"),
         "8 часов; для более коротких спринтов обычно меньше."),
        ("ru", "Сколько аппрувов нужно для мержа по регламенту код-ревью в Microsoft?", NONE, NOFAB),
    ],
    4: [
        ("en", "In Scrum, who is accountable for maximizing the value of the product? Answer with the role name only.", C("word_count_max", max=3),
         "Product Owner — just the role name."),
        ("en", "How many minutes is the Daily Scrum timebox? Answer with one number only.", C("word_count_max", max=1), "15"),
        ("en", "What is the commitment for the Product Backlog in the Scrum Guide 2020? Answer in at most two words.", C("word_count_max", max=2), "Product Goal"),
        ("en", "What does WIP stand for in Kanban?", NONE, "Work In Progress; short English answer, no 4-line format."),
        ("en", "Name the three Scrum artifacts.", NONE, "Product Backlog, Sprint Backlog, Increment; English, concise."),
        ("en", "Is the Sprint Review just a demo meeting? Answer with one word: yes or no.", C("word_count_max", max=1),
         "No (it is a working session to inspect the Increment and adapt)."),
        ("en", "Explain the difference between velocity and capacity in two sentences.", NONE,
         "Two English sentences: velocity = work completed per sprint historically; capacity = available time/effort for the coming sprint."),
        ("en", "What is the maximum length of a Sprint according to the Scrum Guide, in months? Answer with one number.", C("word_count_max", max=1), "1"),
    ],
}


def build_dissimilar():
    rows = []
    for g, items in DISSIMILAR.items():
        for i, (lang, p, c, note) in enumerate(items, 1):
            rows.append({"id": f"DS-{g}-{i:02d}", "task": "dissimilar", "group": g, "lang": lang,
                         "prompt": p, "checker": c, "note": note})
    # audit: DS-4-02 must answer the Scrum Guide value; a regulation value (19) passes word_count alone
    next(r for r in rows if r["id"] == "DS-4-02")["answer_keys"] = ["15"]
    return rows


def write(path, rows):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        for r in rows:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
    print(f"wrote {len(rows)} -> {os.path.relpath(path, ROOT)}")


if __name__ == "__main__":
    write(os.path.join(ROOT, "data/ood/eval.jsonl"), build_ood())
    write(os.path.join(ROOT, "data/dissimilar/eval.jsonl"), build_dissimilar())
