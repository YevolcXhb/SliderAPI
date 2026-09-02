package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateSourceCacheStub struct {
	data string
}

func (s *updateSourceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateSourceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateSourceClientStub struct {
	release       *GitHubRelease
	requestedRepo string
}

func (s *updateSourceClientStub) FetchLatestRelease(_ context.Context, repo string) (*GitHubRelease, error) {
	s.requestedRepo = repo
	return s.release, nil
}

func (*updateSourceClientStub) FetchRecentReleases(context.Context, string, int) ([]*GitHubRelease, error) {
	return nil, nil
}

func (*updateSourceClientStub) DownloadFile(context.Context, string, string, int64) error {
	return nil
}

func (*updateSourceClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	return nil, nil
}

// validOwnRelease 是本仓库（githubRepo）的合法 release 元数据。
func validOwnRelease() *GitHubRelease {
	return &GitHubRelease{
		TagName: "v0.1.147",
		HTMLURL: "https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.147",
		Assets: []GitHubAsset{{
			Name:               "sub2api_0.1.147_linux_amd64.tar.gz",
			BrowserDownloadURL: "https://github.com/Wei-Shaw/sub2api/releases/download/v0.1.147/sub2api_0.1.147_linux_amd64.tar.gz",
		}},
	}
}

// foreignRelease 模拟上游返回了别的仓库的 release（GitHub API 被劫持或配置串仓库）。
func foreignRelease() *GitHubRelease {
	return &GitHubRelease{
		TagName: "v1.0.4",
		HTMLURL: "https://github.com/wenyi401/ikik-api/releases/tag/v1.0.4",
		Assets: []GitHubAsset{{
			Name:               "ikik-api_1.0.4_linux_amd64.tar.gz",
			BrowserDownloadURL: "https://github.com/wenyi401/ikik-api/releases/download/v1.0.4/ikik-api_1.0.4_linux_amd64.tar.gz",
		}},
	}
}

func TestFetchLatestReleaseRejectsDifferentRepository(t *testing.T) {
	client := &updateSourceClientStub{release: foreignRelease()}
	svc := NewUpdateService(&updateSourceCacheStub{}, client, "0.1.146", "release")

	_, err := svc.fetchLatestRelease(context.Background())
	require.ErrorContains(t, err, "release source mismatch")
	require.Equal(t, githubRepo, client.requestedRepo)
}

func TestFetchLatestReleaseRejectsForeignAsset(t *testing.T) {
	release := validOwnRelease()
	release.Assets = append(release.Assets, GitHubAsset{
		Name:               "ikik-api_1.0.4_linux_amd64.tar.gz",
		BrowserDownloadURL: "https://github.com/wenyi401/ikik-api/releases/download/v1.0.4/ikik-api_1.0.4_linux_amd64.tar.gz",
	})
	client := &updateSourceClientStub{release: release}
	svc := NewUpdateService(&updateSourceCacheStub{}, client, "0.1.146", "release")

	_, err := svc.fetchLatestRelease(context.Background())
	require.ErrorContains(t, err, "source mismatch")
}

func TestCheckUpdateIgnoresCachedReleaseFromDifferentRepository(t *testing.T) {
	maliciousCache, err := json.Marshal(struct {
		Latest      string       `json:"latest"`
		ReleaseInfo *ReleaseInfo `json:"release_info"`
		Timestamp   int64        `json:"timestamp"`
	}{
		Latest: "1.0.4",
		ReleaseInfo: &ReleaseInfo{
			HTMLURL: "https://github.com/wenyi401/ikik-api/releases/tag/v1.0.4",
			Assets: []Asset{{
				Name:        "ikik-api_1.0.4_linux_amd64.tar.gz",
				DownloadURL: "https://github.com/wenyi401/ikik-api/releases/download/v1.0.4/ikik-api_1.0.4_linux_amd64.tar.gz",
			}},
		},
		Timestamp: time.Now().Unix(),
	})
	require.NoError(t, err)

	cache := &updateSourceCacheStub{data: string(maliciousCache)}
	client := &updateSourceClientStub{release: validOwnRelease()}
	svc := NewUpdateService(cache, client, "0.1.146", "release")

	info, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.Equal(t, "0.1.147", info.LatestVersion)
	require.Equal(t, githubRepo, client.requestedRepo)
	require.Equal(t, validOwnRelease().HTMLURL, info.ReleaseInfo.HTMLURL)
}

func TestValidateUpdateRepositoryURL(t *testing.T) {
	require.NoError(t, validateUpdateRepositoryURL("https://github.com/Wei-Shaw/sub2api/releases/download/v0.1.147/sub2api_linux_amd64.tar.gz"))
	require.Error(t, validateUpdateRepositoryURL("https://github.com/wenyi401/ikik-api/releases/download/v1.0.4/ikik-api_linux_amd64.tar.gz"))
	require.Error(t, validateUpdateRepositoryURL("https://example.com/Wei-Shaw/sub2api/releases/download/v0.1.147/file.tar.gz"))
	require.Error(t, validateUpdateRepositoryURL("http://github.com/Wei-Shaw/sub2api/releases/download/v0.1.147/file.tar.gz"))
}
