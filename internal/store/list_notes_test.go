package store

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestListNotesPersistAndIsolateAccounts(t *testing.T) {
	s, path := newStore(t)
	note := ListNote{Paused: true, StartedAt: "2026-09-04", Repeat: 2}
	require.NoError(t, s.SetListNote("account-a:1", &note))
	re, err := Open(path)
	require.NoError(t, err)
	require.Equal(t, note, re.ListNote("account-a:1"))
	require.Equal(t, ListNote{}, re.ListNote("account-b:1"))
	require.NoError(t, re.SetListNote("account-a:1", nil))
	re, err = Open(path)
	require.NoError(t, err)
	require.Equal(t, ListNote{}, re.ListNote("account-a:1"))
}
func TestListNoteWriteFailurePreservesMemory(t *testing.T) {
	s, path := newStore(t)
	note := ListNote{Repeat: 1}
	require.NoError(t, s.SetListNote("a:1", &note))
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Mkdir(path, 0700))
	require.Error(t, s.SetListNote("a:1", nil))
	require.Equal(t, note, s.ListNote("a:1"))
}
