package animego

import (
	"context"
	"fmt"
	"time"
)

type accountContextKey struct{}

// SessionGeneration 只在建立、恢复或清空账号时变化；token 正常刷新不应让缓存失效。
func (c *Client) SessionGeneration() uint64 {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.sessionGeneration
}
func (c *Client) accountContext(ctx context.Context) context.Context {
	if _, ok := ctx.Value(accountContextKey{}).(uint64); ok {
		return ctx
	}
	return context.WithValue(ctx, accountContextKey{}, c.SessionGeneration())
}
func (c *Client) checkAccount(ctx context.Context) error {
	if generation, ok := ctx.Value(accountContextKey{}).(uint64); ok && generation != c.SessionGeneration() {
		return &Error{Kind: ErrAuthExpired, Op: "account", Err: fmt.Errorf("账号已变更，请重新操作")}
	}
	return nil
}

// waitListTurn 在收藏批量调用处节流，避免退几十集瞬间耗尽上游账号桶；不自动重试。
func (c *Client) waitListTurn(ctx context.Context) error {
	interval := c.listRequestInterval
	if interval <= 0 {
		return c.checkAccount(ctx)
	}
	c.listRateMu.Lock()
	at := time.Now()
	if c.nextListRequest.After(at) {
		at = c.nextListRequest
	}
	c.nextListRequest = at.Add(interval)
	c.listRateMu.Unlock()
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return c.checkAccount(ctx)
	}
}
