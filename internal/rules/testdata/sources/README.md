# 六源规则（测试专用副本）

这里的 YAML 只被 `differential_test.go` 读取，**不会被 go:embed 进二进制**——本体零内置源（红线 1）。
它们是 `nagare-rules` 仓库的种子；差分测试用 `../<source>/*.golden.json`（旧适配器的黄金输出）
逐字节钉住每条规则的行为。改规则先跑 `go test ./internal/rules/ -run Differential`。
