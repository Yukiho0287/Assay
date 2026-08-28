package httpserver

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/Yukiho0287/assay/server/internal/api"
	"github.com/Yukiho0287/assay/server/internal/version"
)

// tagPattern 与 deploy-receive.sh / deploy.yml 的 tag 校验保持一致
var tagPattern = regexp.MustCompile(`^v[0-9][A-Za-z0-9.+~-]*$`)

func (h *handlers) GetUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requirePerm(w, r, "system"); !ok {
		return
	}
	status := api.UpdateStatus{
		CurrentVersion:  version.Version,
		TokenConfigured: h.gh.Configured(),
	}
	if !status.TokenConfigured {
		writeJSON(w, http.StatusOK, status)
		return
	}

	rel, err := h.gh.LatestRelease(r.Context())
	if err != nil {
		h.log.Error("检查更新失败", "err", err)
		writeJSON(w, http.StatusBadGateway, api.Error{Error: "查询 GitHub Release 失败：" + err.Error()})
		return
	}
	status.LatestVersion = &rel.TagName
	status.UpdateAvailable = rel.TagName != version.Version
	if rel.Body != "" {
		status.ReleaseNotes = &rel.Body
	}
	if !rel.PublishedAt.IsZero() {
		published := rel.PublishedAt
		status.PublishedAt = &published
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *handlers) TriggerDeploy(w http.ResponseWriter, r *http.Request) {
	s, ok := h.requirePerm(w, r, "system")
	if !ok {
		return
	}
	var req api.DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !tagPattern.MatchString(req.Tag) {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "tag 格式非法"})
		return
	}
	if !h.gh.Configured() {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "服务器未配置 GitHub Token，无法触发在线更新"})
		return
	}
	if err := h.gh.DispatchDeploy(r.Context(), req.Tag); err != nil {
		h.log.Error("触发部署失败", "tag", req.Tag, "err", err)
		writeJSON(w, http.StatusBadGateway, api.Error{Error: "触发部署失败：" + err.Error()})
		return
	}
	h.log.Info("触发在线更新", "tag", req.Tag, "operator", s.Username)
	w.WriteHeader(http.StatusAccepted)
}
