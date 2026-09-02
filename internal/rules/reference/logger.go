// 临时参照组：复制自 animego go-api/internal/torrents（AGPL-3.0，同一作者）。M2 差分测试跑通后整包删除，勿在此新增功能。

// Package reference — logger.go
//
// Logger 接口逐字取自 animego 的 aggregator.go。aggregator 本体依赖
// internal/cache，不在参照组范围内；这里只搬 FetchGarden / FetchAnimeTosho
// 零结果 tripwire 所需的最小接口。测试用的记录型 testLogger 本来就住在
// garden_test.go，随该文件一起复制，无需另建。
package reference

// Logger is the minimal logging interface the package depends on.
// Pass *log.Logger via a small wrapper, *slog.Logger, or a test
// recorder — anything with a Warn(msg, args...) method.  Nil disables
// logging.
//
// The args... is keyword-style: key1, value1, key2, value2, ... which
// lets slog adapters extract structured fields without re-parsing the
// message.
type Logger interface {
	Warn(msg string, args ...any)
}
