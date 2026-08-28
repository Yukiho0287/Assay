// Package update 实现在线更新的 GitHub 侧交互：
// 查最新 Release 判断是否有新版本，触发 deploy.yml workflow_dispatch 让 Actions 重新部署。
// 服务器上不需要任何 docker 权限——更新链路与正常发版完全同一条（Actions → SSH 蓝绿部署）。
package update

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	repo  string // owner/repo
	token string
	hc    *http.Client
}

func New(repo, token string) *Client {
	return &Client{repo: repo, token: token, hc: &http.Client{Timeout: 15 * time.Second}}
}

// Configured 未配置 token 时私有仓库既查不了 Release 也触发不了 Actions，前端据此降级提示
func (c *Client) Configured() bool { return c.token != "" }

type Release struct {
	TagName     string    `json:"tag_name"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
}

func (c *Client) do(ctx context.Context, method, url string, body []byte) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	return c.hc.Do(req)
}

func (c *Client) LatestRelease(ctx context.Context) (*Release, error) {
	resp, err := c.do(ctx, http.MethodGet, fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", c.repo), nil)
	if err != nil {
		return nil, fmt.Errorf("请求 GitHub Release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub Release API 返回 %d", resp.StatusCode)
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("解析 GitHub Release 响应: %w", err)
	}
	return &rel, nil
}

// DispatchDeploy 触发 deploy.yml 的 workflow_dispatch，让 Actions 重新部署指定 tag
func (c *Client) DispatchDeploy(ctx context.Context, tag string) error {
	payload, _ := json.Marshal(map[string]any{
		"ref":    "main",
		"inputs": map[string]string{"tag": tag},
	})
	resp, err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("https://api.github.com/repos/%s/actions/workflows/deploy.yml/dispatches", c.repo), payload)
	if err != nil {
		return fmt.Errorf("触发部署流水线: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GitHub dispatch API 返回 %d: %s", resp.StatusCode, b)
	}
	return nil
}
