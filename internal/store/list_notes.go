package store

// ListNote 保存账号接口未提供的日期、重看次数和搁置状态，以账号与作品为键隔离。
type ListNote struct {
	Paused      bool   `json:"paused"`
	StartedAt   string `json:"startedAt"`
	CompletedAt string `json:"completedAt"`
	Repeat      int    `json:"repeat"`
}

func (s *Store) ListNote(key string) ListNote {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.ListNotes[key]
}
func (s *Store) SetListNote(key string, note *ListNote) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.ListNotes == nil {
		s.data.ListNotes = map[string]ListNote{}
	}
	old, had := s.data.ListNotes[key]
	if note == nil {
		delete(s.data.ListNotes, key)
	} else {
		s.data.ListNotes[key] = *note
	}
	if err := s.save(); err != nil {
		if had {
			s.data.ListNotes[key] = old
		} else {
			delete(s.data.ListNotes, key)
		}
		return err
	}
	return nil
}
