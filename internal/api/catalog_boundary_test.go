package api

import (
	"github.com/stretchr/testify/require"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogNeverRequestsUpstreamResources(t *testing.T) {
	for _, root := range []string{"../animego", "../../frontend/src/lib"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if strings.Contains(path, "_test.") || strings.Contains(path, ".test.") {
				return nil
			}
			if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".ts") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			require.NotContains(t, string(data), "/api/anime/"+"torrents", path)
			require.NotContains(t, string(data), "graphql."+"anilist.co", path)
			return nil
		})
		require.NoError(t, err)
	}
}
