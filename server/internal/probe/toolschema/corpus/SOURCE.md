# 语料来源

本目录下 16 个套件的 `valid.jsonl`（共 212 行）与 `LICENSE` 逐字节复制自
[MoonshotAI/Kimi-Vendor-Verifier](https://github.com/MoonshotAI/Kimi-Vendor-Verifier)
（MIT License, Copyright (c) 2026 Moonshot AI），commit `3dad65a`，
原路径 `testdata/walle_validator_cases/validator_cases/<套件名>/valid.jsonl`。

铁律：**不得修改任何一个字节**（byte-exact，选例行号与官方报告对齐依赖于此）。
选例/包装算法见 `../cases.go`，为上游 `tests/tool_call_json_schema/validator.py` 的 Go 移植。
