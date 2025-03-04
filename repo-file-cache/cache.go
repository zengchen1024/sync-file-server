package cache

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/opensourceways/repo-file-cache/models"

	"github.com/opensourceways/sync-file-server/backend"
)

func NewBackendStorage(platform, endpoint string) backend.Storage {
	slash := "/"
	if !strings.HasSuffix(endpoint, slash) {
		endpoint += slash
	}

	return &repoFileCache{
		platform:        platform,
		endpoint:        endpoint,
		summaryEndpoint: endpoint + "%s?summary=true",
	}
}

type repoFileCache struct {
	platform        string
	endpoint        string
	summaryEndpoint string

	cli http.Client
}

func (fc *repoFileCache) SaveFiles(b backend.Branch, branchSHA string, files []backend.File) error {
	opts := models.FileUpdateOption{
		Branch: models.Branch{
			Platform: fc.platform,
			Org:      b.Org,
			Repo:     b.Repo,
			Branch:   b.Branch,
		},
	}
	opts.BranchSHA = branchSHA

	n := len(files)
	fs := make([]models.File, n)
	for i := 0; i < n; i++ {
		item := &files[i]
		fs[i] = models.File{
			Path:    models.FilePath(item.Path),
			SHA:     item.SHA,
			Content: item.Content,
		}
	}
	opts.Files = fs

	payload, err := jsonMarshal(&opts)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, fc.endpoint, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}

	return fc.forwardTo(req, nil)
}

func (fc *repoFileCache) GetFileSummary(b backend.Branch, fileName string) ([]backend.RepoFile, error) {
	endpoint := fmt.Sprintf(
		fc.summaryEndpoint,
		path.Join(fc.platform, b.Org, b.Repo, b.Branch, fileName),
	)

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var v struct {
		Data struct {
			Files []backend.RepoFile `json:"files"`
		} `json:"data"`
	}

	if err = fc.forwardTo(req, &v); err != nil {
		return nil, err
	}

	return v.Data.Files, nil
}

func (fc *repoFileCache) forwardTo(req *http.Request, jsonResp interface{}) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "sync-file-server")

	resp, err := fc.do(req)
	if err != nil || resp == nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		rb, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("response has status %q anfc body %q", resp.Status, string(rb))
	}

	if jsonResp != nil {
		return json.NewDecoder(resp.Body).Decode(jsonResp)
	}
	return nil
}

func (fc *repoFileCache) do(req *http.Request) (resp *http.Response, err error) {
	if resp, err = fc.cli.Do(req); err == nil {
		return
	}

	maxRetries := 4
	backoff := 100 * time.Millisecond

	for retries := 0; retries < maxRetries; retries++ {
		time.Sleep(backoff)
		backoff *= 2

		if resp, err = fc.cli.Do(req); err == nil {
			break
		}
	}
	return
}

func jsonMarshal(t interface{}) ([]byte, error) {
	buffer := &bytes.Buffer{}
	enc := json.NewEncoder(buffer)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(t); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
