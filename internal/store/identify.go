package store

// 媒体库自动识别的记录：fileID → 上一次拿这个文件去问 animego、并且得到了确定答复的时间（毫秒）。
//
// 「确定答复」包括认出来了、上游没有、认出了但没落盘（已有别的作品的匹配、用户认定了作品）。
// 记下来的用处有两个：同一个文件一周之内不再问；一个分组在一周之内最多试几个文件
// （按记录数算预算，不是按每一轮算 —— 否则上游没有的番会被一轮两集地问完整季）。
// 识别成功的结果本身落成那个文件的匹配缓存（Bindings），这里只记「问过了」。
// 它是纯缓存：丢了只是多问几次，所以不进 SchemaVersion，旧版本读写时丢掉它也无妨。

// IdentifyOutcome 是一次识别结果落盘的结局。
type IdentifyOutcome int

const (
	// IdentifySaved：落成了匹配缓存（新写或补全了缺的字段）。
	IdentifySaved IdentifyOutcome = iota + 1
	// IdentifyUnchanged：已有的匹配指向同一部作品、也不缺字段，什么都没改。
	IdentifyUnchanged
	// IdentifyConflict：这个文件已有一条指向【别的】作品的匹配（播放时匹配到的），不改。
	IdentifyConflict
	// IdentifyBlocked：识别期间用户给这个分组认定了作品（或标为不是目录作品），以用户为准。
	IdentifyBlocked
)

// IdentifyAttempt 返回某个文件上一次识别得到确定答复的时间；没有记录返回 0。
func (s *Store) IdentifyAttempt(fileID string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.IdentifyAttempts[fileID]
}

// RecordIdentifyAttempt 记下这个文件问过了（上游没有、或请求被拒），并缓存它的指纹。
// 一次落盘：后台识别逐个文件进行，每个文件多写一次就是多一次整份 state.json 重写。
func (s *Store) RecordIdentifyAttempt(fileID, hash string, at int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordAttemptLocked(fileID, hash, at)
	return s.save()
}

func (s *Store) recordAttemptLocked(fileID, hash string, at int64) {
	if s.data.IdentifyAttempts == nil {
		s.data.IdentifyAttempts = map[string]int64{}
	}
	s.data.IdentifyAttempts[fileID] = at
	if hash != "" {
		s.data.Hashes[fileID] = hash
	}
}

// SaveIdentified 在一把锁内核对、合并、落盘一个文件的识别结果，并记下问过了、缓存指纹。
//
// 核对与写入分开取锁的话，中间可能插进播放器写下的匹配或用户刚做的认定，被这里盖掉。
// 规则：分组有关联（clusterKey）就不写匹配；文件已有指向别的作品的匹配不改；
// 指向同一部作品时只补它缺的封面、标题与弹幕集。
func (s *Store) SaveIdentified(clusterKey, fileID, hash string, at int64, b Binding) (IdentifyOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	outcome := s.mergeIdentifiedLocked(clusterKey, fileID, b)
	s.recordAttemptLocked(fileID, hash, at)
	return outcome, s.save()
}

func (s *Store) mergeIdentifiedLocked(clusterKey, fileID string, b Binding) IdentifyOutcome {
	if _, ok := s.data.Associations[clusterKey]; ok {
		return IdentifyBlocked
	}
	prev, had := s.data.Bindings[fileID]
	if !had || prev.AnilistID <= 0 {
		s.data.Bindings[fileID] = b
		return IdentifySaved
	}
	if prev.AnilistID != b.AnilistID {
		return IdentifyConflict
	}
	merged := prev
	if merged.CoverURL == "" {
		merged.CoverURL = b.CoverURL
	}
	if merged.Title == "" {
		merged.Title = b.Title
	}
	if merged.DandanEpisodeID == 0 && b.DandanEpisodeID != 0 {
		merged.DandanEpisodeID, merged.EpisodeTitle, merged.Episode = b.DandanEpisodeID, b.EpisodeTitle, b.Episode
	}
	if merged == prev {
		return IdentifyUnchanged
	}
	// 封面按 immutable 缓存、地址带匹配时间：补了封面就换个时间
	merged.MatchedAt = b.MatchedAt
	s.data.Bindings[fileID] = merged
	return IdentifySaved
}

// PruneIdentifyAttempts 删掉早于 before（毫秒）的记录：过了重试期的记录已经不起作用。
// 按时间而不是按「文件还在不在媒体库里」清：外接盘拔掉的那一轮扫不到它的文件，
// 按在不在清就会把记录全丢掉，盘插回来又从头问一遍。没有要删的就不落盘。
func (s *Store) PruneIdentifyAttempts(before int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for id, at := range s.data.IdentifyAttempts {
		if at < before {
			delete(s.data.IdentifyAttempts, id)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.save()
}
