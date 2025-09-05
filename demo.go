package main

import (
	"context"
	"encoding/json"
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

// 全局状态
var (
	clients     = make(map[*websocket.Conn]bool)
	clientsMu   sync.RWMutex
	hdcClient   *hdc.Client
	uiDrivers   = make(map[string]*hdc.UiDriver)
	uiDriversMu sync.RWMutex
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
func startCapture(ctx context.Context, deviceKey string) error {
	d := getOrCreateUiDriver(ctx, deviceKey)
	if err := d.Start(ctx); err != nil {
		return err
	}
	_, err := d.StartCaptureScreen(ctx, func(frame []byte) {
		broadcastBinaryFrame(frame)
	}, 1)
	return err
}

// 停止屏幕捕获并关闭 UiDriver
func stopCapture(ctx context.Context, deviceKey string) error {
	uiDriversMu.Lock()
	d := uiDrivers[deviceKey]
	uiDriversMu.Unlock()
	if d == nil {
		return nil
	}
	_ = d.StopCaptureScreen(ctx)
	d.Stop()
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
// 新增
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	switch action {
	case "start":
		if err := startCapture(ctx, deviceKey); err != nil {
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"message": "投屏已启动"})
	case "stop":
		if err := stopCapture(ctx, deviceKey); err != nil {
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"message": "投屏已停止"})
	default:
		http.Error(w, "Unknown action", http.StatusBadRequest)
	}
}

// WebSocket：接收控制消息、下发设备列表
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket升级失败: %v", err)
		return
	}
	defer conn.Close()

	clientsMu.Lock()
	clients[conn] = true
	clientsMu.Unlock()

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
				conn.WriteJSON(map[string]any{"type": "error", "data": err.Error()})
			} else {
				conn.WriteJSON(map[string]any{"type": "devices", "data": devices})
			}
		}
	}

	clientsMu.Lock()
	delete(clients, conn)
	clientsMu.Unlock()
}

func main() {
	cli := hdc.NewClient(hdc.Options{})
	hdcClient = cli

	http.HandleFunc("/", serveHTML)
	http.HandleFunc("/api/devices", handleDevices)
	http.HandleFunc("/api/devices/", handleDeviceControl)
	http.HandleFunc("/ws", handleWebSocket)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("服务器启动在端口 %s", port)
	log.Printf("请在浏览器中访问: http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
