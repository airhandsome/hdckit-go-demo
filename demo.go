package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	hdc "github.com/airhandsome/hdckit-go/hdc"
	"github.com/gorilla/websocket"
)

// 设备信息
type Device struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	OhosVersion string `json:"ohosVersion"`
	SdkVersion  string `json:"sdkVersion"`
	Connected   bool   `json:"connected"`
}

// 图像数据
type ImageData struct {
	Type      string  `json:"type"`
	Data      string  `json:"data"` // base64 帧
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Scale     float64 `json:"scale"`
	DeviceKey string  `json:"deviceKey"`
}

// WebSocket 升级器
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// 设备投屏状态
type DeviceCaptureState struct {
	UiDriver    *hdc.UiDriver
	IsCapturing bool
	ClientID    string // 用于标识哪个客户端在控制这个设备
}

// 全局状态
var (
	clients     = make(map[*websocket.Conn]bool)
	clientsMu   sync.RWMutex
	hdcClient   *hdc.Client
	uiDrivers   = make(map[string]*hdc.UiDriver)
	uiDriversMu sync.RWMutex
	// 新增：设备投屏状态管理
	captureStates = make(map[string]*DeviceCaptureState)
	captureMu     sync.RWMutex
)

// 获取或创建 UiDriver
func getOrCreateUiDriver(ctx context.Context, deviceKey string) *hdc.UiDriver {
	uiDriversMu.Lock()
	defer uiDriversMu.Unlock()
	if d, ok := uiDrivers[deviceKey]; ok && d != nil {
		return d
	}
	t := (*hdcClient).Target(deviceKey)
	d := t.CreateUiDriver()
	uiDrivers[deviceKey] = d
	return d
}

// 启动屏幕捕获并通过 WS 广播帧
func startCapture(ctx context.Context, deviceKey string, clientID string) error {
	log.Printf("开始启动设备 %s 的投屏，客户端: %s", deviceKey, clientID)

	// 检查是否已经在投屏
	captureMu.Lock()
	if state, exists := captureStates[deviceKey]; exists && state.IsCapturing {
		captureMu.Unlock()
		log.Printf("设备 %s 已经在投屏中，跳过", deviceKey)
		return fmt.Errorf("设备 %s 已经在投屏中", deviceKey)
	}
	captureMu.Unlock()

	log.Printf("获取或创建设备 %s 的UiDriver", deviceKey)
	d := getOrCreateUiDriver(ctx, deviceKey)
	if d == nil {
		log.Printf("无法创建设备 %s 的UiDriver", deviceKey)
		return fmt.Errorf("无法创建设备 %s 的UiDriver", deviceKey)
	}

	log.Printf("启动设备 %s 的UiDriver", deviceKey)
	if err := d.Start(ctx); err != nil {
		log.Printf("启动设备 %s 的UiDriver失败: %v", deviceKey, err)
		return err
	}

	log.Printf("开始屏幕捕获，设备: %s", deviceKey)
	_, err := d.StartCaptureScreen(ctx, func(frame []byte) {
		log.Printf("收到设备 %s 的帧数据，大小: %d bytes", deviceKey, len(frame))
		broadcastBinaryFrameToDevice(deviceKey, frame)
	}, 1)

	if err != nil {
		log.Printf("启动屏幕捕获失败，设备: %s, 错误: %v", deviceKey, err)
		return err
	}

	// 更新投屏状态
	captureMu.Lock()
	captureStates[deviceKey] = &DeviceCaptureState{
		UiDriver:    d,
		IsCapturing: true,
		ClientID:    clientID,
	}
	captureMu.Unlock()

	log.Printf("设备 %s 投屏已启动，客户端: %s", deviceKey, clientID)
	return nil
}

// 停止屏幕捕获并关闭 UiDriver
func stopCapture(ctx context.Context, deviceKey string) error {
	captureMu.Lock()
	state, exists := captureStates[deviceKey]
	if !exists || !state.IsCapturing {
		captureMu.Unlock()
		return nil
	}

	// 更新状态
	state.IsCapturing = false
	captureMu.Unlock()

	// 停止投屏
	if state.UiDriver != nil {
		_ = state.UiDriver.StopCaptureScreen(ctx)
		state.UiDriver.Stop()
	}

	log.Printf("设备 %s 投屏已停止", deviceKey)
	return nil
}

// 列表设备
func scanDevices(ctx context.Context) ([]*Device, error) {
	targets, err := (*hdcClient).ListTargets(ctx)
	if err != nil {
		return nil, err
	}
	ret := make([]*Device, 0, len(targets))
	for _, key := range targets {
		ret = append(ret, &Device{
			Key:       key,
			Name:      key,
			Connected: true,
		})
	}
	return ret, nil
}

// 广播图像数据到所有 WebSocket 客户端
func broadcastBinaryFrame(frame []byte) {
	clientsMu.RLock()
	defer clientsMu.RUnlock()
	for c := range clients {
		if err := c.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			c.Close()
			delete(clients, c)
		}
	}
}

// 广播特定设备的图像数据
func broadcastBinaryFrameToDevice(deviceKey string, frame []byte) {
	log.Printf("广播设备 %s 的帧数据到客户端，帧大小: %d bytes", deviceKey, len(frame))

	clientsMu.RLock()
	clientCount := len(clients)
	clientsMu.RUnlock()

	log.Printf("当前连接客户端数量: %d", clientCount)

	// 创建包含设备标识和帧数据的消息
	// 格式: deviceKey长度(4字节) + deviceKey + 帧数据
	deviceKeyBytes := []byte(deviceKey)
	keyLen := len(deviceKeyBytes)

	// 创建消息缓冲区
	message := make([]byte, 4+keyLen+len(frame))

	// 写入设备Key长度
	message[0] = byte(keyLen >> 24)
	message[1] = byte(keyLen >> 16)
	message[2] = byte(keyLen >> 8)
	message[3] = byte(keyLen)

	// 写入设备Key
	copy(message[4:4+keyLen], deviceKeyBytes)

	// 写入帧数据
	copy(message[4+keyLen:], frame)

	clientsMu.RLock()
	defer clientsMu.RUnlock()
	for c := range clients {
		if err := c.WriteMessage(websocket.BinaryMessage, message); err != nil {
			log.Printf("发送帧数据到客户端失败: %v", err)
			c.Close()
			delete(clients, c)
		} else {
			log.Printf("成功发送帧数据到客户端")
		}
	}
}

// 提供 HTML 页面
func serveHTML(w http.ResponseWriter, r *http.Request) {
	html, err := os.ReadFile("index.html")
	if err != nil {
		http.Error(w, "无法读取HTML文件", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(html)
}

// 设备列表接口
func handleDevices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	devices, err := scanDevices(ctx)
	if err != nil {
		log.Printf("扫描设备失败: %v", err)
		// 即使扫描失败，也返回空设备列表而不是错误
		devices = []*Device{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(devices)
}

// 设备控制接口：/api/devices/{key}/(start|stop)
func handleDeviceControl(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	deviceKey := parts[0]
	action := parts[1]

	// 从请求头或查询参数获取客户端ID
	clientID := r.Header.Get("X-Client-ID")
	if clientID == "" {
		clientID = r.URL.Query().Get("clientId")
	}
	if clientID == "" {
		clientID = "web-" + fmt.Sprintf("%d", time.Now().Unix())
	}

	log.Printf("deviceKey: %s, action: %s, clientID: %s", deviceKey, action, clientID)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	switch action {
	case "start":
		log.Printf("收到启动投屏请求，设备: %s, 客户端: %s", deviceKey, clientID)
		if err := startCapture(ctx, deviceKey, clientID); err != nil {
			log.Printf("启动投屏失败，设备: %s, 错误: %v", deviceKey, err)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		log.Printf("投屏启动成功，设备: %s", deviceKey)
		json.NewEncoder(w).Encode(map[string]string{"message": "投屏已启动"})
	case "stop":
		log.Printf("收到停止投屏请求，设备: %s", deviceKey)
		if err := stopCapture(ctx, deviceKey); err != nil {
			log.Printf("停止投屏失败，设备: %s, 错误: %v", deviceKey, err)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		log.Printf("投屏停止成功，设备: %s", deviceKey)
		json.NewEncoder(w).Encode(map[string]string{"message": "投屏已停止"})
	default:
		log.Printf("未知操作: %s", action)
		http.Error(w, "Unknown action", http.StatusBadRequest)
	}
}

// WebSocket：接收控制消息、下发设备列表
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	log.Printf("新的WebSocket连接请求")
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket升级失败: %v", err)
		return
	}
	defer conn.Close()

	clientsMu.Lock()
	clients[conn] = true
	clientCount := len(clients)
	clientsMu.Unlock()

	log.Printf("WebSocket连接建立，当前客户端数量: %d", clientCount)

	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}
		typ, _ := msg["type"].(string)
		switch typ {
		case "getDevices":
			devices, err := scanDevices(r.Context())
			if err != nil {
				log.Printf("WebSocket扫描设备失败: %v", err)
				// 即使扫描失败，也返回空设备列表
				devices = []*Device{}
			}
			conn.WriteJSON(map[string]any{"type": "devices", "data": devices})
		}
	}

	clientsMu.Lock()
	delete(clients, conn)
	clientsMu.Unlock()
}

func main() {
	// 从环境变量获取HDC服务器配置，默认使用远程服务器
	hdcHost := os.Getenv("HDC_HOST")
	if hdcHost == "" {
		hdcHost = "10.86.97.52"
	}

	hdcPortStr := os.Getenv("HDC_PORT")
	if hdcPortStr == "" {
		hdcPortStr = "8710"
	}

	// 初始化HDC客户端 - 连接到远程HDC服务器
	cli := hdc.NewClient(hdc.Options{
		Host: hdcHost,
		Port: 8710, // 使用默认端口
	})
	hdcClient = cli
	log.Printf("HDC客户端已连接到远程服务器: %s:%s", hdcHost, hdcPortStr)

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if targets, err := cli.ListTargets(ctx); err != nil {
		log.Printf("警告: 无法连接到HDC服务器 %s:%s, 错误: %v", hdcHost, hdcPortStr, err)
		log.Printf("请确保HDC服务器正在运行，并且网络连接正常")
	} else {
		log.Printf("HDC服务器连接成功，发现 %d 个设备", len(targets))
		for _, target := range targets {
			log.Printf("设备: %s", target)
		}
	}

	http.HandleFunc("/", serveHTML)
	http.HandleFunc("/api/devices", handleDevices)
	http.HandleFunc("/api/devices/", handleDeviceControl)
	http.HandleFunc("/ws", handleWebSocket)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	log.Printf("服务器启动在端口 %s", port)
	log.Printf("请在浏览器中访问: http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
