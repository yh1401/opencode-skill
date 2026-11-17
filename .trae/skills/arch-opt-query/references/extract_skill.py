# -*- coding: utf-8 -*-
import io, os, re

base = os.path.dirname(os.path.abspath(__file__))
p = os.path.join(base, ".tool_result_spill", "remote_read_file_20261105082847_06f8706aaad5.txt")
text = io.open(p, encoding="utf-8", errors="replace").read()

m = re.search(r"## 二、.*?(?=## 四、)", text, re.S)
if m:
    print(m.group(0)[:8000])
else:
    # 回退：打印命名空间排除表之后到 ## 四、 之前
    m2 = re.search(r"命名空间排除规则.*?(?=## 四、)", text, re.S)
    print(m2.group(0)[:8000] if m2 else text[2000:10000])
