package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Yukiho0287/assay/server/internal/api"
	"github.com/Yukiho0287/assay/server/internal/auth"
	"github.com/Yukiho0287/assay/server/internal/db"
)

const (
	sessionCookie = "assay_session"
	sessionTTL    = 7 * 24 * time.Hour
)

// dummyHash 用于用户不存在时的空比对，抹平响应时间差异，防止用户名枚举
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("assay-timing-dummy"), bcrypt.DefaultCost)

// handlers 实现 api.ServerInterface；所有业务端点在此实现。
type handlers struct {
	log *slog.Logger
	q   *db.Queries
}

var _ api.ServerInterface = (*handlers)(nil)

func (h *handlers) GetHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.Health{Status: api.Ok})
}

func (h *handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req api.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "用户名和密码不能为空"})
		return
	}

	u, err := h.q.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
		writeJSON(w, http.StatusUnauthorized, api.Error{Error: "用户名或密码错误"})
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		writeJSON(w, http.StatusUnauthorized, api.Error{Error: "用户名或密码错误"})
		return
	}

	token, hash, err := auth.NewSessionToken()
	if err != nil {
		h.log.Error("生成会话失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	if err := h.q.CreateSession(r.Context(), db.CreateSessionParams{
		TokenHash: hash,
		UserID:    u.ID,
		ExpiresAt: time.Now().Add(sessionTTL),
	}); err != nil {
		h.log.Error("写入会话失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// 部署到 HTTPS 后需补 Secure；本地开发经 vite 代理走 http
	})
	h.log.Info("用户登录", "username", u.Username)
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if err := h.q.DeleteSession(r.Context(), auth.HashToken(c.Value)); err != nil {
			h.log.Error("删除会话失败", "err", err)
		}
	}
	// 无论此前是否持有有效会话，都清 cookie 并返回 204（登出幂等）
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	u, ok := h.sessionUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, api.Error{Error: "未登录或会话已过期"})
		return
	}
	writeJSON(w, http.StatusOK, api.CurrentUser{
		Id:       u.ID,
		Username: u.Username,
		Role:     api.CurrentUserRole(u.Role),
	})
}

// sessionUser 从请求 cookie 解析出当前会话用户；后续受保护端点统一走这里。
func (h *handlers) sessionUser(r *http.Request) (db.GetSessionUserRow, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return db.GetSessionUserRow{}, false
	}
	u, err := h.q.GetSessionUser(r.Context(), auth.HashToken(c.Value))
	if err != nil {
		return db.GetSessionUserRow{}, false
	}
	return u, true
}
