import unittest

from core.text import is_refusal, jaccard, char_ngrams, keys_match, normalize


class TextTest(unittest.TestCase):
    def test_number_words(self):
        self.assertEqual(normalize("Тринадцать дней"), "13 дней")
        self.assertEqual(normalize("двадцать третий день"), "23 день")
        self.assertEqual(normalize("Twenty-three items, the fifth"), "23 items, the 5")
        self.assertEqual(normalize("ровно сорока минутам"), "ровно 40 минутам")
        self.assertEqual(normalize("в четвёртый раз"), "в 4 раз")
        self.assertEqual(normalize("сто"), "100")
        self.assertEqual(normalize("10 000 рублей"), "10000 рублей")

    def test_ordinary_words_untouched(self):
        for w in ("стоит", "вторник", "пятница", "одинаковый", "second", "сторона", "трехмерный"):
            self.assertEqual(normalize(w), w)

    def test_keys(self):
        self.assertTrue(keys_match(["13"], "Итерация длится 13 дней."))
        self.assertFalse(keys_match(["13"], "Итерация длится 132 дня."))
        self.assertFalse(keys_match(["13"], "в 113 случаях"))
        self.assertTrue(keys_match(["13"], "тринадцати рабочих дней"))
        self.assertTrue(keys_match(["гаранин"], "у Гаранина"))
        self.assertTrue(keys_match(["журавл|crane"], "Ask Mr. Crane"))
        self.assertFalse(keys_match(["au"], "because"))
        self.assertTrue(keys_match(["13", "журавл"], "13 дней, Журавлёва", require_all=True))
        self.assertFalse(keys_match(["13", "журавл"], "13 дней", require_all=True))
        self.assertTrue(keys_match(["13", "журавл"], "13 дней", require_all=False))
        self.assertTrue(keys_match(["10:30"], "в 10:30 утра"))
        self.assertFalse(keys_match([], "anything"))

    def test_refusal(self):
        for t in ("В регламенте это не указано.", "Информации об этом нет.", "Я не знаю.",
                  "This is not specified in the regulation.", "I don't know.", "Такой компании не существует."):
            self.assertTrue(is_refusal(t), t)
        for t in ("Итерация длится 13 рабочих дней.", "The answer is 42."):
            self.assertFalse(is_refusal(t), t)

    def test_jaccard(self):
        self.assertEqual(jaccard(char_ngrams("abc"), char_ngrams("abc")), 1.0)
        self.assertEqual(jaccard(char_ngrams("abc"), char_ngrams("xyz")), 0.0)
        self.assertAlmostEqual(jaccard(char_ngrams("Ёлка!"), char_ngrams("елка")), 1.0)


if __name__ == "__main__":
    unittest.main()


class RefusalTextTest(unittest.TestCase):
    def test_existence_negations(self):
        for t in ("Нет такого романа.", "Такого закона нет.", "Такого элемента не существует.", "Он не существует.",
                  "Мне неизвестен такой элемент.", "There is no such treaty.", "It does not exist.",
                  "I'm not aware of that law."):
            self.assertTrue(is_refusal(t, names=False), t)

    def test_plain_no_is_not_refusal(self):
        for t in ("Нет, это неверно, итерация длится 13 дней.", "Нет, ретро проводится каждую итерацию.",
                  "No, the sprint lasts two weeks."):
            self.assertFalse(is_refusal(t), t)
