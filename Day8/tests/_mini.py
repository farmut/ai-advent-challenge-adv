"""Temporary copy of the mini fixture root and its sizes."""

import shutil
import tempfile
from pathlib import Path

from build_data import Sizes

MINI = Path(__file__).parent / "fixtures/mini"
MINI_SIZES = Sizes(facts_train=2, facts_control=1, facts_eval={"b": 2, "c": 1, "d": 1, "control": 1},
                   facts_eval_b_lang={"ru": 1, "en": 1}, min_related_pairs=1, style=(2, 1, 1),
                   ood={"arith": 1, "ifeval": 1, "knowledge": 1, "translate": 1, "nonexistent": 1},
                   dissimilar_groups=2, dissimilar_per_group=1, format=(2, 1, 1), batch=2)


def mini_copy() -> Path:
    tmp = Path(tempfile.mkdtemp(prefix="day8_mini_"))
    shutil.copytree(MINI, tmp, dirs_exist_ok=True)
    return tmp
