package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/Yukiho0287/assay/server/internal/api"
	"github.com/Yukiho0287/assay/server/internal/db"
)

// rolePermsToAPI 把 jsonb 权限解析为契约类型；数据损坏时返回全关（Fail-Safe 拒绝）
func (h *handlers) rolePermsToAPI(name string, raw []byte) api.PermissionMap {
	var p api.PermissionMap
	if err := json.Unmarshal(raw, &p); err != nil {
		h.log.Error("角色权限数据损坏", "role", name, "err", err)
	}
	return p
}

func (h *handlers) ListRoles(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requirePerm(w, r, "users"); !ok {
		return
	}
	rows, err := h.q.ListRoles(r.Context())
	if err != nil {
		h.log.Error("查询角色列表失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	out := make([]api.Role, 0, len(rows))
	for _, ro := range rows {
		out = append(out, api.Role{Id: ro.ID, Name: ro.Name, BuiltIn: ro.BuiltIn, Permissions: h.rolePermsToAPI(ro.Name, ro.Permissions)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) CreateRole(w http.ResponseWriter, r *http.Request) {
	s, ok := h.requirePerm(w, r, "users")
	if !ok {
		return
	}
	var req api.RoleCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "角色名不能为空"})
		return
	}
	if len(req.Name) > 64 {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "角色名过长（最多 64 字符）"})
		return
	}
	perms, _ := json.Marshal(req.Permissions)
	ro, err := h.q.CreateRole(r.Context(), db.CreateRoleParams{Name: req.Name, Permissions: perms})
	if err != nil {
		if isUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, api.Error{Error: "角色名已存在"})
			return
		}
		h.log.Error("创建角色失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	h.log.Info("创建角色", "role", ro.Name, "operator", s.Username)
	writeJSON(w, http.StatusCreated, api.Role{Id: ro.ID, Name: ro.Name, BuiltIn: ro.BuiltIn, Permissions: req.Permissions})
}

func (h *handlers) UpdateRole(w http.ResponseWriter, r *http.Request, id api.IdPath) {
	s, ok := h.requirePerm(w, r, "users")
	if !ok {
		return
	}
	var req api.RoleUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "请求格式错误"})
		return
	}
	cur, err := h.q.GetRole(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, api.Error{Error: "角色不存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	if cur.BuiltIn {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "内置角色不可修改"})
		return
	}

	name := cur.Name
	if req.Name != nil {
		if *req.Name == "" || len(*req.Name) > 64 {
			writeJSON(w, http.StatusBadRequest, api.Error{Error: "角色名长度须为 1-64 字符"})
			return
		}
		name = *req.Name
	}
	permsRaw := cur.Permissions
	if req.Permissions != nil {
		permsRaw, _ = json.Marshal(*req.Permissions)
	}

	ro, err := h.q.UpdateRole(r.Context(), db.UpdateRoleParams{ID: id, Name: name, Permissions: permsRaw})
	if err != nil {
		if isUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, api.Error{Error: "角色名已存在"})
			return
		}
		h.log.Error("更新角色失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	h.log.Info("更新角色", "role", ro.Name, "operator", s.Username)
	writeJSON(w, http.StatusOK, api.Role{Id: ro.ID, Name: ro.Name, BuiltIn: ro.BuiltIn, Permissions: h.rolePermsToAPI(ro.Name, ro.Permissions)})
}

func (h *handlers) DeleteRole(w http.ResponseWriter, r *http.Request, id api.IdPath) {
	s, ok := h.requirePerm(w, r, "users")
	if !ok {
		return
	}
	cur, err := h.q.GetRole(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, api.Error{Error: "角色不存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	if cur.BuiltIn {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "内置角色不可删除"})
		return
	}
	n, err := h.q.CountUsersByRole(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	if n > 0 {
		writeJSON(w, http.StatusConflict, api.Error{Error: "该角色仍被用户使用，先调整这些用户的角色"})
		return
	}
	if _, err := h.q.DeleteRole(r.Context(), id); err != nil {
		h.log.Error("删除角色失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	h.log.Info("删除角色", "role", cur.Name, "operator", s.Username)
	w.WriteHeader(http.StatusNoContent)
}
