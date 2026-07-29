package webapp

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/jeni0101/vpn/server/internal/auth"
	"github.com/jeni0101/vpn/server/internal/manager"
	"github.com/jeni0101/vpn/server/internal/managerclient"
	"github.com/jeni0101/vpn/server/internal/model"
)

//go:embed dist/*
var embeddedAssets embed.FS

const sessionCookie = "__Host-tnest_session"

type Server struct {
	auth         *auth.Store
	manager      *managerclient.Client
	downloads    *downloadCache
	limiter      *auth.RateLimiter
	logger       *log.Logger
	secure       bool
	assets       fs.FS
	releasesPath string
	nodeAPIToken []byte
}

func New(
	authStore *auth.Store,
	managerClient *managerclient.Client,
	logger *log.Logger,
	secureCookies bool,
	publicDir string,
	releasesPath string,
	nodeAPIToken string,
) (*Server, error) {
	var assets fs.FS
	var err error
	if publicDir != "" {
		assets = os.DirFS(publicDir)
	} else {
		assets, err = fs.Sub(embeddedAssets, "dist")
		if err != nil {
			return nil, err
		}
	}
	return &Server{
		auth: authStore, manager: managerClient, downloads: newDownloadCache(),
		limiter: auth.NewRateLimiter(), logger: logger, secure: secureCookies,
		assets: assets, releasesPath: releasesPath,
		nodeAPIToken: []byte(nodeAPIToken),
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/totp", s.totp)
	mux.Handle("POST /api/v1/auth/logout", s.requireAuth(http.HandlerFunc(s.logout), true))
	mux.Handle("GET /api/v1/auth/session", s.requireAuth(http.HandlerFunc(s.session), false))
	mux.HandleFunc("POST /api/v1/enrollments/claim", s.claim)
	mux.HandleFunc("POST /api/v2/enrollments/claim", s.claimV2)
	mux.HandleFunc("GET /api/v2/client/regions", s.clientRegions)
	mux.HandleFunc("POST /api/v2/client/regions/{regionCode}/enroll", s.enrollRegion)
	mux.HandleFunc("GET /api/v2/client/usage", s.clientUsage)
	mux.HandleFunc("GET /api/v2/node/desired-state", s.nodeDesiredState)
	mux.HandleFunc("POST /api/v2/node/report", s.nodeReport)
	mux.Handle("GET /api/v1/devices", s.requireAuth(http.HandlerFunc(s.devices), false))
	mux.Handle("POST /api/v1/devices", s.requireAuth(http.HandlerFunc(s.createDevice), true))
	mux.Handle("POST /api/v1/devices/{id}/revoke", s.requireAuth(http.HandlerFunc(s.revoke), true))
	mux.Handle("POST /api/v1/devices/{id}/rotate", s.requireAuth(http.HandlerFunc(s.rotate), true))
	mux.Handle("GET /api/v1/usage", s.requireAuth(http.HandlerFunc(s.usage), false))
	mux.Handle("GET /api/v1/audit", s.requireAuth(http.HandlerFunc(s.audit), false))
	mux.Handle("GET /api/v1/events", s.requireAuth(http.HandlerFunc(s.events), false))
	mux.Handle("GET /api/v1/downloads/{token}", s.requireAuth(http.HandlerFunc(s.download), false))
	mux.HandleFunc("GET /api/v1/releases", s.releases)
	mux.HandleFunc("GET /latency", s.latency)
	mux.HandleFunc("GET /api/v2/client/catalog", s.clientCatalog)
	mux.Handle("GET /api/v2/admin/regions",
		s.requireAuth(http.HandlerFunc(s.adminRegions), false))
	mux.Handle("POST /api/v2/admin/regions",
		s.requireAuth(http.HandlerFunc(s.adminRegions), true))
	mux.Handle("PATCH /api/v2/admin/regions/{code}",
		s.requireAuth(http.HandlerFunc(s.adminRegion), true))
	mux.Handle("GET /api/v2/admin/nodes",
		s.requireAuth(http.HandlerFunc(s.adminNodes), false))
	mux.Handle("POST /api/v2/admin/nodes",
		s.requireAuth(http.HandlerFunc(s.adminNodes), true))
	mux.Handle("PATCH /api/v2/admin/nodes/{id}",
		s.requireAuth(http.HandlerFunc(s.adminNode), true))
	mux.Handle("POST /api/v2/admin/devices/{id}/apple-bundle",
		s.requireAuth(http.HandlerFunc(s.adminAppleBundle), true))
	mux.HandleFunc("/", s.static)
	return s.securityHeaders(http.MaxBytesHandler(mux, 256*1024))
}

func (s *Server) latency(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", "0")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) clientCatalog(w http.ResponseWriter, r *http.Request) {
	value, err := s.manager.Catalog(r.Context())
	if err != nil {
		s.managerError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, value)
}

func (s *Server) claimV2(w http.ResponseWriter, r *http.Request) {
	var request manager.ClaimV2Request
	if !readJSON(w, r, &request) {
		return
	}
	result, err := s.manager.ClaimV2(r.Context(), request)
	request.Token = ""
	for index := range request.Credentials {
		request.Credentials[index].PresharedKey = ""
	}
	if err != nil {
		s.managerError(w, err)
		return
	}
	s.auditEvent(r, "enrollment", "device.enrolled_v2", result.Device.ID, "")
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) clientRegions(w http.ResponseWriter, r *http.Request) {
	tokenValue, ok := clientBearer(w, r)
	if !ok {
		return
	}
	values, err := s.manager.ClientRegions(r.Context(), tokenValue)
	if err != nil {
		s.clientManagerError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"regions": values})
}

func (s *Server) enrollRegion(w http.ResponseWriter, r *http.Request) {
	tokenValue, ok := clientBearer(w, r)
	if !ok {
		return
	}
	var request manager.RegionKeyRequest
	if !readJSON(w, r, &request) {
		return
	}
	request.RegionCode = strings.ToUpper(strings.TrimSpace(r.PathValue("regionCode")))
	value, err := s.manager.EnrollRegion(
		r.Context(), tokenValue, request.RegionCode, request,
	)
	request.PresharedKey = ""
	if err != nil {
		s.clientManagerError(w, err)
		return
	}
	jsonResponse(w, http.StatusCreated, value)
}

func (s *Server) clientUsage(w http.ResponseWriter, r *http.Request) {
	tokenValue, ok := clientBearer(w, r)
	if !ok {
		return
	}
	points, bucket, syncedAt, err := s.manager.ClientUsage(
		r.Context(), tokenValue, r.URL.Query().Get("range"),
		r.URL.Query().Get("region"),
	)
	if err != nil {
		s.clientManagerError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{
		"points": points, "bucket": bucket, "synced_at": syncedAt,
	})
}

func (s *Server) nodeDesiredState(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := s.authorizeNode(w, r)
	if !ok {
		return
	}
	value, err := s.manager.NodeDesiredState(r.Context(), nodeID)
	if err != nil {
		s.managerError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, value)
}

func (s *Server) nodeReport(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := s.authorizeNode(w, r)
	if !ok {
		return
	}
	var report model.NodeReport
	if !readJSON(w, r, &report) {
		return
	}
	if report.NodeID != "" && !strings.EqualFold(report.NodeID, nodeID) {
		jsonResponse(w, http.StatusBadRequest, map[string]string{
			"error": "node identity mismatch",
		})
		return
	}
	report.NodeID = nodeID
	if err := s.manager.SaveNodeReport(r.Context(), report); err != nil {
		s.managerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func clientBearer(w http.ResponseWriter, r *http.Request) (string, bool) {
	const prefix = "Bearer "
	value := r.Header.Get("Authorization")
	if len(value) <= len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) {
		jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return "", false
	}
	tokenValue := strings.TrimSpace(value[len(prefix):])
	if len(tokenValue) < 32 || len(tokenValue) > 256 {
		jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return "", false
	}
	return tokenValue, true
}

func (s *Server) authorizeNode(w http.ResponseWriter, r *http.Request) (string, bool) {
	verify := r.Header.Get("X-TNest-Client-Verify")
	distinguishedName := r.Header.Get("X-TNest-Client-DN")
	nodeID := nodeIDFromDistinguishedName(distinguishedName)
	tokenValue, tokenOK := clientBearer(w, r)
	if !tokenOK {
		return "", false
	}
	validToken := len(s.nodeAPIToken) >= 32 &&
		len(tokenValue) == len(s.nodeAPIToken) &&
		subtle.ConstantTimeCompare([]byte(tokenValue), s.nodeAPIToken) == 1
	validCertificate := verify == "SUCCESS" && nodeID != "" &&
		len(nodeID) <= 80
	if !validToken || !validCertificate {
		jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return "", false
	}
	return nodeID, true
}

func nodeIDFromDistinguishedName(value string) string {
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '/'
	}) {
		part = strings.TrimSpace(part)
		if len(part) > 3 && strings.EqualFold(part[:3], "CN=") {
			nodeID := strings.ToLower(strings.TrimSpace(part[3:]))
			for _, r := range nodeID {
				if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
					r == '-' || r == '_') {
					return ""
				}
			}
			return nodeID
		}
	}
	return ""
}

func (s *Server) clientManagerError(w http.ResponseWriter, err error) {
	if strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
		jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	s.managerError(w, err)
}

func (s *Server) adminRegions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		values, err := s.manager.Regions(r.Context(), true)
		if err != nil {
			s.managerError(w, err)
			return
		}
		jsonResponse(w, http.StatusOK, map[string]any{"regions": values})
		return
	}
	if !s.requireSensitiveTOTP(w, r) {
		return
	}
	var value model.Region
	if !readJSON(w, r, &value) {
		return
	}
	if err := s.manager.UpsertRegion(r.Context(), value); err != nil {
		s.managerError(w, err)
		return
	}
	s.auditEvent(r, sessionFrom(r).Username, "region.updated", "", value.Code)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminRegion(w http.ResponseWriter, r *http.Request) {
	if !s.requireSensitiveTOTP(w, r) {
		return
	}
	var value model.Region
	if !readJSON(w, r, &value) {
		return
	}
	value.Code = r.PathValue("code")
	if err := s.manager.UpsertRegion(r.Context(), value); err != nil {
		s.managerError(w, err)
		return
	}
	s.auditEvent(r, sessionFrom(r).Username, "region.updated", "", value.Code)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		values, err := s.manager.Nodes(r.Context(), r.URL.Query().Get("region"), true)
		if err != nil {
			s.managerError(w, err)
			return
		}
		jsonResponse(w, http.StatusOK, map[string]any{"nodes": values})
		return
	}
	if !s.requireSensitiveTOTP(w, r) {
		return
	}
	var value model.Node
	if !readJSON(w, r, &value) {
		return
	}
	if err := s.manager.UpsertNode(r.Context(), value); err != nil {
		s.managerError(w, err)
		return
	}
	s.auditEvent(r, sessionFrom(r).Username, "node.updated", "", value.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminNode(w http.ResponseWriter, r *http.Request) {
	if !s.requireSensitiveTOTP(w, r) {
		return
	}
	var value model.Node
	if !readJSON(w, r, &value) {
		return
	}
	value.ID = r.PathValue("id")
	if err := s.manager.UpsertNode(r.Context(), value); err != nil {
		s.managerError(w, err)
		return
	}
	s.auditEvent(r, sessionFrom(r).Username, "node.updated", "", value.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminAppleBundle(w http.ResponseWriter, r *http.Request) {
	if !s.requireSensitiveTOTP(w, r) {
		return
	}
	result, err := s.manager.AppleBundle(r.Context(), r.PathValue("id"))
	if err != nil {
		s.managerError(w, err)
		return
	}
	defer clearSensitiveBytes(result.Bundle)
	tokenValue, expires, err := s.downloads.Put(
		safeFilename(result.Device.Name)+"-apple-regions.zip",
		"application/zip", result.Bundle, 10*time.Minute,
	)
	if err != nil {
		s.internalError(w, err)
		return
	}
	s.auditEvent(
		r, sessionFrom(r).Username, "device.apple_bundle_created",
		result.Device.ID, "",
	)
	jsonResponse(w, http.StatusCreated, map[string]any{
		"device":       result.Device,
		"download_url": "/api/v1/downloads/" + tokenValue,
		"expires_at":   expires,
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.manager.Health(r.Context()); err != nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]any{"ok": false})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "service": "personal-vpn-web"})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &request) {
		return
	}
	ipKey := "ip:" + remoteIP(r)
	accountKey := "account:" + strings.ToLower(strings.TrimSpace(request.Username))
	if !s.limiter.Allow(ipKey) || !s.limiter.Allow(accountKey) {
		jsonResponse(w, http.StatusTooManyRequests, map[string]string{"error": "登录尝试过多，请15分钟后重试"})
		return
	}
	if !s.auth.CheckPassword(r.Context(), request.Username, request.Password) {
		s.limiter.Fail(ipKey)
		s.limiter.Fail(accountKey)
		jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "用户名或密码错误"})
		return
	}
	s.limiter.Success(ipKey)
	s.limiter.Success(accountKey)
	session, err := s.auth.NewSession(r.Context(), strings.TrimSpace(request.Username), "totp")
	if err != nil {
		s.internalError(w, err)
		return
	}
	s.setCookie(w, session.Token)
	jsonResponse(w, http.StatusOK, map[string]string{"next": "totp"})
}

func (s *Server) totp(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &request) {
		return
	}
	token, ok := s.cookie(r)
	if !ok {
		jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "登录会话已失效"})
		return
	}
	session, err := s.auth.Session(r.Context(), token)
	if err != nil || session.Stage != "totp" {
		jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "登录会话已失效"})
		return
	}
	valid := s.auth.CheckTOTP(r.Context(), request.Code)
	if !valid {
		valid = s.auth.UseRecoveryCode(r.Context(), request.Code)
	}
	if !valid {
		jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "验证码或恢复码错误"})
		return
	}
	if err := s.auth.PromoteSession(r.Context(), token); err != nil {
		s.internalError(w, err)
		return
	}
	session, _ = s.auth.Session(r.Context(), token)
	s.auditEvent(r, session.Username, "auth.login", "", "")
	jsonResponse(w, http.StatusOK, map[string]string{
		"username": session.Username, "csrf_token": session.CSRF,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r)
	s.auditEvent(r, session.Username, "auth.logout", "", "")
	token, _ := s.cookie(r)
	_ = s.auth.DeleteSession(r.Context(), token)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r)
	jsonResponse(w, http.StatusOK, map[string]string{
		"username": session.Username, "csrf_token": session.CSRF,
	})
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.manager.Devices(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"devices": devices})
}

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	if !s.requireSensitiveTOTP(w, r) {
		return
	}
	var request struct {
		Name     string `json:"name"`
		Platform string `json:"platform"`
		Mode     string `json:"mode"`
	}
	if !readJSON(w, r, &request) {
		return
	}
	actor := sessionFrom(r).Username
	switch request.Mode {
	case "invite":
		result, err := s.manager.CreateInvite(r.Context(), manager.CreateRequest{
			Name: request.Name, Platform: request.Platform,
		})
		if err != nil {
			s.managerError(w, err)
			return
		}
		data, _ := json.MarshalIndent(result.Invite, "", "  ")
		data = append(data, '\n')
		token, expires, err := s.downloads.Put(
			safeFilename(result.Device.Name)+".tnestvpn",
			"application/vnd.tnest.vpn-enrollment+json", data, 10*time.Minute)
		if err != nil {
			s.internalError(w, err)
			return
		}
		s.auditEvent(r, actor, "device.invite_created", result.Device.ID, "")
		jsonResponse(w, http.StatusCreated, map[string]any{
			"device": result.Device, "download_url": "/api/v1/downloads/" + token,
			"expires_at": expires,
		})
	case "standard", "qr":
		result, err := s.manager.CreateStandard(r.Context(), manager.CreateRequest{
			Name: request.Name, Platform: request.Platform,
		})
		if err != nil {
			s.managerError(w, err)
			return
		}
		filename := safeFilename(result.Device.Name) + ".conf"
		mediaType := "text/plain; charset=utf-8"
		payload := []byte(result.Config)
		action := "device.standard_config_created"
		if request.Mode == "qr" {
			payload, err = qrcode.Encode(result.Config, qrcode.Medium, 768)
			if err != nil {
				s.internalError(w, err)
				return
			}
			filename = safeFilename(result.Device.Name) + "-qr.png"
			mediaType = "image/png"
			action = "device.qr_created"
		}
		token, expires, err := s.downloads.Put(
			filename, mediaType, payload, 10*time.Minute)
		result.Config = ""
		if err != nil {
			s.internalError(w, err)
			return
		}
		s.auditEvent(r, actor, action, result.Device.ID, "")
		jsonResponse(w, http.StatusCreated, map[string]any{
			"device": result.Device, "download_url": "/api/v1/downloads/" + token,
			"expires_at": expires,
		})
	case "apple_bundle":
		result, err := s.manager.CreateAppleBundle(r.Context(), manager.CreateRequest{
			Name: request.Name, Platform: request.Platform,
		})
		if err != nil {
			s.managerError(w, err)
			return
		}
		defer clearSensitiveBytes(result.Bundle)
		tokenValue, expires, err := s.downloads.Put(
			safeFilename(result.Device.Name)+"-apple-regions.zip",
			"application/zip", result.Bundle, 10*time.Minute,
		)
		if err != nil {
			s.internalError(w, err)
			return
		}
		s.auditEvent(r, actor, "device.apple_bundle_created", result.Device.ID, "")
		jsonResponse(w, http.StatusCreated, map[string]any{
			"device":       result.Device,
			"download_url": "/api/v1/downloads/" + tokenValue,
			"expires_at":   expires,
		})
	default:
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "配置方式无效"})
	}
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if !s.requireSensitiveTOTP(w, r) {
		return
	}
	id := r.PathValue("id")
	if err := s.manager.Revoke(r.Context(), id); err != nil {
		s.managerError(w, err)
		return
	}
	s.auditEvent(r, sessionFrom(r).Username, "device.revoked", id, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rotate(w http.ResponseWriter, r *http.Request) {
	if !s.requireSensitiveTOTP(w, r) {
		return
	}
	id := r.PathValue("id")
	result, err := s.manager.Rotate(r.Context(), id)
	if err != nil {
		s.managerError(w, err)
		return
	}
	data, _ := json.MarshalIndent(result.Invite, "", "  ")
	data = append(data, '\n')
	token, expires, err := s.downloads.Put(
		safeFilename(result.Device.Name)+"-rotate.tnestvpn",
		"application/vnd.tnest.vpn-enrollment+json", data, 10*time.Minute)
	if err != nil {
		s.internalError(w, err)
		return
	}
	s.auditEvent(r, sessionFrom(r).Username, "device.rotation_prepared", id, "")
	jsonResponse(w, http.StatusCreated, map[string]any{
		"device": result.Device, "download_url": "/api/v1/downloads/" + token,
		"expires_at": expires,
	})
}

func (s *Server) claim(w http.ResponseWriter, r *http.Request) {
	var request manager.ClaimRequest
	if !readJSON(w, r, &request) {
		return
	}
	result, err := s.manager.Claim(r.Context(), request)
	request.PresharedKey = ""
	request.Token = ""
	if err != nil {
		s.managerError(w, err)
		return
	}
	s.auditEvent(r, "enrollment", "device.enrolled", result.Device.ID, "")
	jsonResponse(w, http.StatusOK, map[string]any{
		"device": result.Device,
		"configuration": map[string]any{
			"address": []string{result.Device.IPv4 + "/32", result.Device.IPv6 + "/128"},
			"dns":     result.Configuration.DNS,
			"mtu":     result.Configuration.MTU,
			"peer":    result.Configuration,
		},
	})
}

func (s *Server) usage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	bucket := query.Get("bucket")
	if bucket == "" {
		bucket = "hour"
	}
	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour)
	to := now.Add(time.Hour)
	if value := query.Get("from"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "开始时间无效"})
			return
		}
		from = parsed
	}
	if value := query.Get("to"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "结束时间无效"})
			return
		}
		to = parsed
	}
	points, err := s.manager.Usage(r.Context(), query.Get("device_id"), bucket, from, to)
	if err != nil {
		s.managerError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"points": points, "bucket": bucket})
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.manager.Audit(r.Context(), limit)
	if err != nil {
		s.managerError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonResponse(w, http.StatusNotImplemented, map[string]string{"error": "SSE不可用"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(30 * time.Minute)
	defer deadline.Stop()
	for {
		devices, err := s.manager.Devices(r.Context())
		if err != nil {
			return
		}
		data, _ := json.Marshal(map[string]any{"devices": devices, "at": time.Now().UTC()})
		fmt.Fprintf(w, "event: devices\ndata: %s\n\n", data)
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	item, err := s.downloads.Take(r.PathValue("token"))
	if err != nil {
		jsonResponse(w, http.StatusGone, map[string]string{"error": "文件已下载或已过期"})
		return
	}
	defer func() {
		for i := range item.Data {
			item.Data[i] = 0
		}
	}()
	w.Header().Set("Content-Type", item.MediaType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, item.Name))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(item.Data)
}

func (s *Server) releases(w http.ResponseWriter, _ *http.Request) {
	if s.releasesPath != "" {
		if data, err := os.ReadFile(s.releasesPath); err == nil && len(data) <= 256*1024 &&
			json.Valid(data) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(data)
			return
		}
	}
	jsonResponse(w, http.StatusOK, map[string]any{
		"version": 1, "releases": []any{},
		"signature": "", "message": "尚未发布签名客户端安装包",
	})
}

func (s *Server) requireSensitiveTOTP(w http.ResponseWriter, r *http.Request) bool {
	if !s.auth.CheckTOTP(r.Context(), r.Header.Get("X-TNest-TOTP")) {
		jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "敏感操作需要当前TOTP验证码"})
		return false
	}
	return true
}

func (s *Server) requireAuth(next http.Handler, csrf bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := s.cookie(r)
		if !ok {
			jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "请先登录"})
			return
		}
		session, err := s.auth.Session(r.Context(), token)
		if err != nil || session.Stage != "authenticated" {
			jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "登录会话已失效"})
			return
		}
		if csrf && r.Header.Get("X-CSRF-Token") != session.CSRF {
			jsonResponse(w, http.StatusForbidden, map[string]string{"error": "CSRF校验失败"})
			return
		}
		next.ServeHTTP(w, r.WithContext(withSession(r.Context(), session)))
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if s.secure {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "." || name == "" {
		name = "index.html"
	}
	data, err := fs.ReadFile(s.assets, name)
	if err != nil {
		data, err = fs.ReadFile(s.assets, "index.html")
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if mediaType := mime.TypeByExtension(path.Ext(name)); mediaType != "" {
		w.Header().Set("Content-Type", mediaType)
	}
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	_, _ = w.Write(data)
}

func (s *Server) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", MaxAge: 12 * 60 * 60,
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) cookie(r *http.Request) (string, bool) {
	value, err := r.Cookie(sessionCookie)
	return valueString(value), err == nil && value != nil && len(value.Value) >= 32
}

func valueString(cookie *http.Cookie) string {
	if cookie == nil {
		return ""
	}
	return cookie.Value
}

func (s *Server) auditEvent(r *http.Request, actor, action, deviceID, detail string) {
	err := s.manager.AddAudit(r.Context(), model.AuditEvent{
		At: time.Now().UTC(), Actor: actor, Action: action, DeviceID: deviceID,
		RemoteIP: remoteIP(r), Detail: detail,
	})
	if err != nil {
		s.logger.Printf("audit write failed: %v", err)
	}
}

func (s *Server) internalError(w http.ResponseWriter, err error) {
	s.logger.Printf("internal request failure: %v", err)
	jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "服务器内部错误"})
}

func (s *Server) managerError(w http.ResponseWriter, err error) {
	message := strings.TrimPrefix(err.Error(), "manager: ")
	status := http.StatusBadRequest
	if !strings.HasPrefix(err.Error(), "manager:") {
		status = http.StatusServiceUnavailable
		message = "管理服务暂时不可用"
		s.logger.Printf("manager request failure: %v", err)
	}
	jsonResponse(w, status, map[string]string{"error": message})
}

func readJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "请求格式无效"})
		return false
	}
	return true
}

func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func remoteIP(r *http.Request) string {
	if value := r.Header.Get("X-Real-IP"); value != "" && net.ParseIP(value) != nil {
		return value
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func safeFilename(value string) string {
	var builder strings.Builder
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || char == '-' || char == '_' {
			builder.WriteRune(char)
		} else {
			builder.WriteByte('_')
		}
	}
	if builder.Len() == 0 {
		return "tnest-vpn"
	}
	return builder.String()
}

func clearSensitiveBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
