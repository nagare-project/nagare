// Package rules 是声明式磁力源规则引擎（决议 A3）：一条 YAML 规则描述
// 「请求怎么发、响应怎么解、字段怎么映射」，本体零内置源 —— 规则由用户提供、
// 在用户机器上执行（红线 1）。
//
// 输出形状与差分参照组（animego 旧适配器的 TorrentItem）逐字段一致，
// 这是 CQ1 差分测试的比较基准；黄金文件在 testdata/<source>/*.golden.json。
package rules

// Item 是一条搜索结果。指针字段的 nil 有语义：Fansub/Date 序列化为 null，
// Provider/Seeders 缺席时整个键不出现；Seeders 的 0 是「已知为零」，nil 是「未知」。
type Item struct {
	Title    string  `json:"title"`
	Magnet   string  `json:"magnet"`
	Size     string  `json:"size"`
	Fansub   *string `json:"fansub"`
	Date     *string `json:"date"`
	Source   string  `json:"source"`
	Provider *string `json:"provider,omitempty"`
	Seeders  *int    `json:"seeders,omitempty"`
	Infohash string  `json:"infohash,omitempty"`
}

// magnetScheme 是 magnet URI 的必需前缀；不以它开头的条目一律丢弃。
const magnetScheme = "magnet:"

// stringPtr 把空串映射成 nil（JSON null），非空取地址。
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
