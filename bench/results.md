| app | Aether 原文 | 去注释 | compact | skeleton | TS+React+Express+SQL | Python FastAPI+SQLAlchemy+Jinja |
|---|---|---|---|---|---|---|
| todo | 281 | 231 | 190 | 104 | 1936 | 1508 |
| notes | 45 | 21 | 12 | 17 | 794 | 798 |
| crm (no other-stack port) | 647 | 455 | 348 | 184 | — | — |

(o200k_base；cl100k_base 见 results.json)

生成评测（built-in offline rule agent (no LLM available on this box)）：首次 check 通过 18/20，其中确实产生修改 11/20

自我修复（mock OpenAI 服务注入一次字段拼写错误）：LLM 轮数 2，修复后通过 True，第二轮收到诊断 True，系统提示 413 tokens
