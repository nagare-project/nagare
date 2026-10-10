package store

// PlayerConfig 是播放器（mpv）相关的用户设置。
type PlayerConfig struct {
	// Anime4K 是实时超分预设：空串关（默认）、"fast" 标准、"hq" 高质量（见 mpv.Anime4KShaders）。
	Anime4K string `json:"anime4k,omitempty"`
}

// PlayerConfig 读取播放器设置；从未设置过时是零值（全关）。
func (s *Store) PlayerConfig() PlayerConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Player == nil {
		return PlayerConfig{}
	}
	return *s.data.Player
}

// UpdatePlayerConfig 在锁内读改写播放器设置。
func (s *Store) UpdatePlayerConfig(mutate func(c *PlayerConfig)) (PlayerConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var c PlayerConfig
	if s.data.Player != nil {
		c = *s.data.Player
	}
	mutate(&c)
	stored := c
	s.data.Player = &stored
	return c, s.save()
}
