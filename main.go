package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

type Config struct {
	DeepSeekAPIKey   string `json:"deepseek_api_key"`
	DeepSeekModel    string `json:"deepseek_model"`
	DeepSeekBaseURL  string `json:"deepseek_base_url"`
	AdminPassword    string `json:"admin_password"`
	RedisURL         string `json:"redis_url"`
	RedisPassword    string `json:"redis_password"`
	Port             string `json:"port"`
	SummaryThreshold int    `json:"summary_threshold"`
}

const defaultSummaryThreshold = 8 // 默认8条消息触发摘要压缩
const defaultModel = "deepseek-chat"
const defaultBaseURL = "https://api.deepseek.com/v1/chat/completions"

func getModel() string {
	if config.DeepSeekModel != "" {
		return config.DeepSeekModel
	}
	return defaultModel
}

func getBaseURL() string {
	if config.DeepSeekBaseURL != "" {
		return config.DeepSeekBaseURL
	}
	return defaultBaseURL
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Session struct {
	ID        string
	Messages  []Message
	Summary   string
	CreatedAt time.Time
	LastUsed  time.Time
	Role      string
}

type Role struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	SystemPrompt string `json:"system_prompt"`
}

type ChatRequest struct {
	UserID    string `json:"user_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Message   string `json:"message"`
}

type ChatResponse struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

type CreateSessionRequest struct {
	UserID                string `json:"user_id,omitempty"`
	RoleID                string `json:"role_id,omitempty"`
	CustomRoleName        string `json:"custom_role_name,omitempty"`
	CustomRoleDescription string `json:"custom_role_description,omitempty"`
	CustomSystemPrompt    string `json:"custom_system_prompt,omitempty"`
}

type CreateSessionResponse struct {
	SessionID string `json:"session_id"`
	Role      Role   `json:"role"`
}

type DeleteSessionRequest struct {
	UserID    string `json:"user_id,omitempty"`
	SessionID string `json:"session_id"`
}

type AdminAuthRequest struct {
	Password string `json:"password"`
}

type AddRoleRequest struct {
	Password     string `json:"password"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	SystemPrompt string `json:"system_prompt"`
}

type UpdateRoleRequest struct {
	Password     string `json:"password"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	SystemPrompt string `json:"system_prompt"`
}

type DeleteRoleRequest struct {
	Password string `json:"password"`
	ID       string `json:"id"`
}

type RolesResponse struct {
	Roles []Role `json:"roles"`
}

type DeepSeekResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

var (
	config           Config
	rdb              *redis.Client
	ctx              = context.Background()
	backgroundImages []string
)

const sessionTTL = 1 * time.Hour

func loadBackgroundImages() error {
	files, err := os.ReadDir("bg")
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("bg folder not found, using default background images")
			backgroundImages = []string{"bg/default.jpg"}
			return nil
		}
		return err
	}

	var images []string
	for _, file := range files {
		if !file.IsDir() {
			name := file.Name()
			// 只加载图片文件
			if ext := name[len(name)-4:]; ext == ".jpg" || ext == ".png" || ext == ".gif" || ext == ".jpeg" {
				images = append(images, "bg/"+name)
			}
		}
	}

	if len(images) == 0 {
		images = []string{"bg/default.jpg"}
	}

	backgroundImages = images
	log.Printf("Loaded %d background images from bg folder", len(backgroundImages))
	return nil
}

type BackgroundsResponse struct {
	Images []string `json:"images"`
}

func getBackgroundsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(BackgroundsResponse{Images: backgroundImages})
}

var presetRoles = []Role{}

type RolesConfig struct {
	Roles []Role `json:"roles"`
}

func loadRoles() error {
	file, err := os.Open("roles.json")
	if err != nil {
		log.Printf("Warning: roles.json not found, using default roles")
		presetRoles = []Role{
			{
				ID:           "general",
				Name:         "通用助手",
				Description:  "一个乐于助人的通用聊天助手",
				SystemPrompt: "你是一个乐于助人的AI助手，请用友好、专业的语言回答用户的问题。",
			},
		}
		return nil
	}
	defer file.Close()

	var config RolesConfig
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		log.Printf("Warning: failed to parse roles.json, using default roles")
		presetRoles = []Role{
			{
				ID:           "general",
				Name:         "通用助手",
				Description:  "一个乐于助人的通用聊天助手",
				SystemPrompt: "你是一个乐于助人的AI助手，请用友好、专业的语言回答用户的问题。",
			},
		}
		return nil
	}

	presetRoles = config.Roles
	log.Printf("Loaded %d roles from roles.json", len(presetRoles))
	return nil
}

func saveRoles() error {
	config := RolesConfig{Roles: presetRoles}
	data, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile("roles.json", data, 0644)
}

func loadConfig() error {
	file, err := os.Open("config.json")
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	return decoder.Decode(&config)
}

func initRedis() error {
	redisURL := config.RedisURL
	redisPwd := config.RedisPassword

	if redisURL == "" {
		redisURL = "localhost:6379"
	}

	rdb = redis.NewClient(&redis.Options{
		Addr:     redisURL,
		Password: redisPwd,
		DB:       5,
	})

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %v", err)
	}

	log.Println("Connected to Redis successfully")
	return nil
}

func getSessionKey(userID, sessionID string) string {
	return fmt.Sprintf("session:%s:%s", userID, sessionID)
}

func getUserSessionsKey(userID string) string {
	return fmt.Sprintf("user_sessions:%s", userID)
}

func getSessionFromRedis(userID, sessionID string) (*Session, error) {
	key := getSessionKey(userID, sessionID)
	data, err := rdb.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var session Session
	if err := json.Unmarshal([]byte(data), &session); err != nil {
		return nil, err
	}

	return &session, nil
}

func saveSessionToRedis(session *Session, userID string) error {
	key := getSessionKey(userID, session.ID)
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}

	// 存储会话数据
	err = rdb.Set(ctx, key, data, sessionTTL).Err()
	if err != nil {
		return err
	}

	// 将会话ID添加到用户的会话列表
	err = rdb.SAdd(ctx, getUserSessionsKey(userID), session.ID).Err()
	if err != nil {
		return err
	}

	// 设置用户会话列表的过期时间
	return rdb.Expire(ctx, getUserSessionsKey(userID), sessionTTL).Err()
}

func deleteSessionFromRedis(userID, sessionID string) error {
	// 删除会话数据
	err := rdb.Del(ctx, getSessionKey(userID, sessionID)).Err()
	if err != nil {
		return err
	}

	// 从用户会话列表中移除
	return rdb.SRem(ctx, getUserSessionsKey(userID), sessionID).Err()
}

func getUserSessions(userID string) ([]string, error) {
	return rdb.SMembers(ctx, getUserSessionsKey(userID)).Result()
}

func deleteSessionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req DeleteSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.SessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	userID := req.UserID
	if userID == "" {
		userID = "default_user"
	}

	if err := deleteSessionFromRedis(userID, req.SessionID); err != nil {
		http.Error(w, "Failed to delete session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success": true}`))
}

func verifyAdminPassword(password string) bool {
	return password == config.AdminPassword
}

func adminAuthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AdminAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if verifyAdminPassword(req.Password) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success": true}`))
	} else {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"success": false, "error": "密码错误"}`))
	}
}

func addRoleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AddRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if !verifyAdminPassword(req.Password) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"success": false, "error": "密码错误"}`))
		return
	}

	// 检查ID是否已存在
	for _, role := range presetRoles {
		if role.ID == req.ID {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"success": false, "error": "角色ID已存在"}`))
			return
		}
	}

	newRole := Role{
		ID:           req.ID,
		Name:         req.Name,
		Description:  req.Description,
		SystemPrompt: req.SystemPrompt,
	}
	presetRoles = append(presetRoles, newRole)

	if err := saveRoles(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success": false, "error": "保存失败"}`))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "role": newRole})
}

func updateRoleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req UpdateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if !verifyAdminPassword(req.Password) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"success": false, "error": "密码错误"}`))
		return
	}

	// 查找并更新角色
	for i, role := range presetRoles {
		if role.ID == req.ID {
			presetRoles[i] = Role{
				ID:           req.ID,
				Name:         req.Name,
				Description:  req.Description,
				SystemPrompt: req.SystemPrompt,
			}
			if err := saveRoles(); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"success": false, "error": "保存失败"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "role": presetRoles[i]})
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`{"success": false, "error": "角色不存在"}`))
}

func deleteRoleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req DeleteRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if !verifyAdminPassword(req.Password) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"success": false, "error": "密码错误"}`))
		return
	}

	// 查找并删除角色
	for i, role := range presetRoles {
		if role.ID == req.ID {
			presetRoles = append(presetRoles[:i], presetRoles[i+1:]...)
			if err := saveRoles(); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"success": false, "error": "保存失败"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success": true}`))
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`{"success": false, "error": "角色不存在"}`))
}

func uploadBackgroundHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	password := r.FormValue("password")
	if !verifyAdminPassword(password) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"success": false, "error": "密码错误"}`))
		return
	}

	err := r.ParseMultipartForm(10 << 20) // 10MB
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"success": false, "error": "文件太大"}`))
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"success": false, "error": "获取文件失败"}`))
		return
	}
	defer file.Close()

	// 检查文件类型
	ext := handler.Filename[len(handler.Filename)-4:]
	if ext != ".jpg" && ext != ".png" && ext != ".gif" && ext != "jpeg" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"success": false, "error": "只支持 jpg, png, gif 格式"}`))
		return
	}

	// 确保bg目录存在
	if err := os.MkdirAll("bg", 0755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success": false, "error": "创建目录失败"}`))
		return
	}

	// 创建文件
	dst, err := os.Create("bg/" + handler.Filename)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success": false, "error": "保存文件失败"}`))
		return
	}
	defer dst.Close()

	// 复制文件内容
	if _, err := io.Copy(dst, file); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success": false, "error": "复制文件失败"}`))
		return
	}

	// 重新加载背景图片列表
	loadBackgroundImages()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success": true, "filename": "bg/` + handler.Filename + `"}`))
}

func deleteBackgroundHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Password string `json:"password"`
		Filename string `json:"filename"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if !verifyAdminPassword(req.Password) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"success": false, "error": "密码错误"}`))
		return
	}

	if err := os.Remove(req.Filename); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success": false, "error": "删除失败"}`))
		return
	}

	loadBackgroundImages()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success": true}`))
}

// 保存配置到 config.json
func saveConfig() error {
	file, err := os.Create("config.json")
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "    ")
	return encoder.Encode(&config)
}

// 获取当前配置（密钥仅显示后4位）
func getConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	maskedKey := config.DeepSeekAPIKey
	if len(maskedKey) > 4 {
		maskedKey = "****" + maskedKey[len(maskedKey)-4:]
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"config": map[string]string{
			"deepseek_api_key":  maskedKey,
			"deepseek_model":    config.DeepSeekModel,
			"deepseek_base_url": config.DeepSeekBaseURL,
		},
	})
}

// 更新 DeepSeek 配置
func updateConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Password        string `json:"password"`
		DeepSeekAPIKey  string `json:"deepseek_api_key"`
		DeepSeekModel   string `json:"deepseek_model"`
		DeepSeekBaseURL string `json:"deepseek_base_url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if !verifyAdminPassword(req.Password) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"success": false, "error": "密码错误"}`))
		return
	}

	// 仅更新非空字段
	if req.DeepSeekAPIKey != "" && !strings.HasPrefix(req.DeepSeekAPIKey, "****") {
		config.DeepSeekAPIKey = req.DeepSeekAPIKey
	}
	if req.DeepSeekModel != "" {
		config.DeepSeekModel = req.DeepSeekModel
	}
	if req.DeepSeekBaseURL != "" {
		config.DeepSeekBaseURL = req.DeepSeekBaseURL
	}

	if err := saveConfig(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success": false, "error": "保存配置失败"}`))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success": true}`))
}

func getRolesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(RolesResponse{Roles: presetRoles})
}

func createSessionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var role Role

	if req.CustomRoleName != "" && req.CustomSystemPrompt != "" {
		role = Role{
			ID:           "custom_" + uuid.New().String(),
			Name:         req.CustomRoleName,
			Description:  req.CustomRoleDescription,
			SystemPrompt: req.CustomSystemPrompt,
		}
	} else {
		role = presetRoles[0]
		for _, r := range presetRoles {
			if r.ID == req.RoleID {
				role = r
				break
			}
		}
	}

	userID := req.UserID
	if userID == "" {
		userID = "default_user"
	}

	sessionID := uuid.New().String()
	session := &Session{
		ID:        sessionID,
		Messages:  []Message{{Role: "system", Content: role.SystemPrompt}, {Role: "assistant", Content: "你好！我是" + role.Name + "，" + role.Description + "有什么我可以帮助你的吗？"}},
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
		Role:      role.ID,
	}

	if err := saveSessionToRedis(session, userID); err != nil {
		http.Error(w, "Failed to save session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CreateSessionResponse{SessionID: sessionID, Role: role})
}

// buildMessagesForAPI 构建发送给DeepSeek的消息列表，自动压缩旧消息为摘要
func buildMessagesForAPI(session *Session, apiKey string) []Message {
	threshold := config.SummaryThreshold
	if threshold <= 0 {
		threshold = defaultSummaryThreshold
	}

	fullMsgs := session.Messages
	if len(fullMsgs) <= threshold {
		return fullMsgs
	}

	keepCount := threshold
	recentMsgs := fullMsgs[len(fullMsgs)-keepCount:]

	var summary string
	if session.Summary != "" {
		summary = session.Summary
	} else {
		// 首次超阈值：同步生成摘要（后续请求复用）
		oldMsgs := fullMsgs[:len(fullMsgs)-keepCount]
		s, err := summarizeMessages(oldMsgs, apiKey)
		if err != nil {
			log.Printf("Failed to summarize messages: %v", err)
		} else {
			summary = s
			session.Summary = s
		}
	}

	result := []Message{}
	if summary != "" {
		result = append(result, Message{
			Role:    "system",
			Content: "[历史对话摘要] " + summary,
		})
	}
	result = append(result, recentMsgs...)
	return result
}

// summarizeMessages 调用DeepSeek将多条消息摘要为一段文字
func summarizeMessages(messages []Message, apiKey string) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	// 构建摘要请求
	var convText string
	for _, m := range messages {
		convText += fmt.Sprintf("%s: %s\n", m.Role, m.Content)
	}

	summarizePrompt := Message{
		Role:    "system",
		Content: "你是一个对话摘要助手。请用一段中文简洁概括以下对话的核心内容和结论，控制在100字以内。",
	}
	userMsg := Message{
		Role:    "user",
		Content: "请概括这段对话：\n" + convText,
	}

	payload, err := json.Marshal(map[string]interface{}{
		"model":    getModel(),
		"messages": []Message{summarizePrompt, userMsg},
	})
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	request, err := http.NewRequest("POST", getBaseURL(), bytes.NewBuffer(payload))
	if err != nil {
		return "", err
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)

	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	var dsResponse DeepSeekResponse
	if err := json.NewDecoder(response.Body).Decode(&dsResponse); err != nil {
		return "", err
	}

	if len(dsResponse.Choices) > 0 {
		return dsResponse.Choices[0].Message.Content, nil
	}
	return "", fmt.Errorf("no summary generated")
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.SessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	userID := req.UserID
	if userID == "" {
		userID = "default_user"
	}

	session, err := getSessionFromRedis(userID, req.SessionID)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	session.Messages = append(session.Messages, Message{Role: "user", Content: req.Message})
	session.LastUsed = time.Now()

	deepSeekAPIKey := config.DeepSeekAPIKey
	if deepSeekAPIKey == "" {
		http.Error(w, "DeepSeek API key not configured", http.StatusInternalServerError)
		return
	}

	// 构建发送给 DeepSeek 的消息列表
	messagesToSend := buildMessagesForAPI(session, deepSeekAPIKey)

	// 如果进行了压缩，将摘要存入 session（但不影响 messagesToSend 中已有的摘要）
	// 压缩逻辑在 buildMessagesForAPI 内部完成并更新 session

	payload, err := json.Marshal(map[string]interface{}{
		"model":    getModel(),
		"messages": messagesToSend,
	})
	if err != nil {
		http.Error(w, "Failed to create payload", http.StatusInternalServerError)
		return
	}

	client := &http.Client{Timeout: 60 * time.Second}
	request, err := http.NewRequest("POST", getBaseURL(), bytes.NewBuffer(payload))
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+deepSeekAPIKey)

	response, err := client.Do(request)
	if err != nil {
		http.Error(w, "Failed to call DeepSeek API", http.StatusInternalServerError)
		return
	}
	defer response.Body.Close()

	var dsResponse DeepSeekResponse
	if err := json.NewDecoder(response.Body).Decode(&dsResponse); err != nil {
		http.Error(w, "Failed to parse response", http.StatusInternalServerError)
		return
	}

	if len(dsResponse.Choices) > 0 {
		assistantMessage := dsResponse.Choices[0].Message
		session.Messages = append(session.Messages, assistantMessage)
		session.LastUsed = time.Now()

		if err := saveSessionToRedis(session, userID); err != nil {
			log.Printf("Failed to save session: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			SessionID: req.SessionID,
			Message:   assistantMessage.Content,
		})
	} else {
		http.Error(w, "No response from DeepSeek", http.StatusInternalServerError)
	}
}

func main() {
	if err := loadConfig(); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := initRedis(); err != nil {
		log.Fatalf("Failed to initialize Redis: %v", err)
	}

	if err := loadRoles(); err != nil {
		log.Printf("Warning: Failed to load roles: %v", err)
	}

	if err := loadBackgroundImages(); err != nil {
		log.Printf("Warning: Failed to load background images: %v", err)
	}

	http.HandleFunc("/api/roles", getRolesHandler)
	http.HandleFunc("/api/session", createSessionHandler)
	http.HandleFunc("/api/session/delete", deleteSessionHandler)
	http.HandleFunc("/api/chat", chatHandler)
	http.HandleFunc("/api/backgrounds", getBackgroundsHandler)
	http.HandleFunc("/api/admin/auth", adminAuthHandler)
	http.HandleFunc("/api/admin/role/add", addRoleHandler)
	http.HandleFunc("/api/admin/role/update", updateRoleHandler)
	http.HandleFunc("/api/admin/role/delete", deleteRoleHandler)
	http.HandleFunc("/api/admin/background/upload", uploadBackgroundHandler)
	http.HandleFunc("/api/admin/background/delete", deleteBackgroundHandler)
	http.HandleFunc("/api/admin/config", getConfigHandler)
	http.HandleFunc("/api/admin/config/update", updateConfigHandler)

	fs := http.FileServer(http.Dir("."))
	http.Handle("/", fs)

	port := config.Port
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Server starting on http://localhost:%s...\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
